// Package web serves the ReTouch control UI and JSON API. Playback is
// NATIVE: presets are real TUNEIN ContentItems the speaker plays itself (resolving
// via the live TuneIn service) — STLocal never proxies audio.
package web

import (
	"context"
	"embed"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"encoding/json"

	"github.com/stein155/retouch/internal/settings"
	"github.com/stein155/retouch/internal/speaker"
	"github.com/stein155/retouch/internal/store"
	"github.com/stein155/retouch/internal/tunein"
)

// dist holds the built React UI (frontend/), embedded at build time. The
// contents are produced by `vite build` (see frontend/). index.html is served
// for / and the hashed assets under their own paths.
//
//go:embed all:dist
var distFS embed.FS

// tuneInBase / logoMaxBytes bound the same-origin TuneIn proxies below. TuneIn
// has no CORS and serves logos over plain http, so the browser can't reach them
// directly — STLocal mirrors the request server-side instead.
const (
	tuneInBase   = "https://opml.radiotime.com"
	logoMaxBytes = 2 << 20 // 2 MiB
)

// Server wires the UI to the speaker and TuneIn search.
type Server struct {
	tunein   *tunein.Client
	speaker  *speaker.Client
	store    *store.Store
	settings *settings.Store
	mirror   PresetMirror
	log      *slog.Logger
	ui       http.Handler // serves the embedded dist bundle
	proxy    *http.Client // for the same-origin TuneIn / logo proxies
}

// PresetMirror receives successful direct speaker preset writes so the local cloud
// emulation cannot later sync stale presets back to the speaker.
type PresetMirror interface {
	MirrorPreset(slot int, name, location, art string)
	RemovePreset(slot int)
}

// New builds a Server.
func New(tc *tunein.Client, b *speaker.Client, s *store.Store, set *settings.Store, log *slog.Logger) *Server {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// dist is embedded at build time; this only fails if the build is broken.
		panic("web: embedded dist missing: " + err.Error())
	}
	return &Server{
		tunein:   tc,
		speaker:  b,
		store:    s,
		settings: set,
		log:      log,
		ui:       http.FileServer(http.FS(sub)),
		proxy:    &http.Client{Timeout: 12 * time.Second},
	}
}

// SetPresetMirror attaches the local cloud preset store after both servers exist.
func (s *Server) SetPresetMirror(m PresetMirror) {
	s.mirror = m
}

// Handler returns the HTTP mux.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/search", s.search)
	mux.HandleFunc("GET /api/tunein/", s.tuneinProxy)
	mux.HandleFunc("GET /api/logo", s.logoProxy)
	mux.HandleFunc("GET /api/presets", s.presets)
	mux.HandleFunc("PUT /api/presets/{slot}", s.setPreset)
	mux.HandleFunc("DELETE /api/presets/{slot}", s.delPreset)
	mux.HandleFunc("POST /api/play/{slot}", s.playPreset)
	mux.HandleFunc("POST /api/play", s.playStation)
	mux.HandleFunc("POST /api/stop", s.stop)
	mux.HandleFunc("GET /api/now", s.now)
	mux.HandleFunc("GET /api/volume", s.getVolume)
	mux.HandleFunc("POST /api/volume", s.setVolume)
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.putSettings)
	// Everything else is the embedded single-page UI. More specific /api/...
	// patterns above win; this serves index.html, assets and icons.
	mux.HandleFunc("GET /", s.ui.ServeHTTP)
	return mux
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	if q == "" {
		writeJSON(w, 200, []tunein.Station{})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	stations, err := s.tunein.Search(ctx, q)
	if err != nil {
		s.fail(w, "search failed", err)
		return
	}
	writeJSON(w, 200, stations)
}

// tuneinProxy mirrors a request to https://opml.radiotime.com so the browser can
// hit TuneIn's OPML API same-origin (TuneIn sends no CORS headers). Only the
// path under /api/tunein/ and the query string are forwarded; the host is fixed.
func (s *Server) tuneinProxy(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/tunein")
	if path == "" || path == "/" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	target := tuneInBase + path
	if r.URL.RawQuery != "" {
		target += "?" + r.URL.RawQuery
	}
	s.relay(w, r, target, "application/json", 256*1024)
}

// logoProxy fetches a TuneIn/CDN logo image given ?u=<absolute url>. Only http/
// https image hosts are followed and the body is size-capped.
func (s *Server) logoProxy(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("u")
	if raw == "" {
		http.Error(w, "missing u", http.StatusBadRequest)
		return
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		http.Error(w, "bad url", http.StatusBadRequest)
		return
	}
	s.relay(w, r, u.String(), "image/png", logoMaxBytes)
}

// relay performs a bounded upstream GET and copies the response back. fallbackCT
// is used only when the upstream omits a Content-Type.
func (s *Server) relay(w http.ResponseWriter, r *http.Request, target, fallbackCT string, max int64) {
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		s.fail(w, "proxy build", err)
		return
	}
	req.Header.Set("User-Agent", "ReTouch/1.0")
	resp, err := s.proxy.Do(req)
	if err != nil {
		s.fail(w, "proxy fetch", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	} else {
		w.Header().Set("Content-Type", fallbackCT)
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, io.LimitReader(resp.Body, max))
}

// presets returns the speaker's NATIVE presets (what the speaker actually has). Falls
// back to the local store if the speaker can't be reached.
func (s *Server) presets(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	ps, err := s.speaker.Presets(ctx)
	if err != nil {
		s.log.Warn("read speaker presets; serving local store", "err", err)
		writeJSON(w, 200, s.store.All())
		return
	}
	writeJSON(w, 200, ps)
}

// setPreset stores a NATIVE preset on the speaker (slot 1..6) via /storePreset, so
// it becomes a real preset button — not a disconnected local entry.
func (s *Server) setPreset(w http.ResponseWriter, r *http.Request) {
	slot, ok := slotOf(w, r)
	if !ok {
		return
	}
	var p store.Preset
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || p.StationID == "" {
		s.fail(w, "stationId required", err)
		return
	}
	p.Slot = slot
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	loc := "/v1/playback/station/" + p.StationID
	if err := s.speaker.StorePreset(ctx, slot, "TUNEIN", "stationurl", loc, p.Name, p.Logo); err != nil {
		s.fail(w, "save failed", err)
		return
	}
	if s.mirror != nil {
		s.mirror.MirrorPreset(slot, p.Name, loc, p.Logo)
	}
	s.log.Info("store preset", "slot", slot, "station", p.StationID, "name", p.Name)
	writeJSON(w, 200, p)
}

// delPreset clears a native preset slot on the speaker.
func (s *Server) delPreset(w http.ResponseWriter, r *http.Request) {
	slot, ok := slotOf(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	if err := s.speaker.RemovePreset(ctx, slot); err != nil {
		s.fail(w, "delete failed", err)
		return
	}
	if s.mirror != nil {
		s.mirror.RemovePreset(slot)
	}
	w.WriteHeader(http.StatusNoContent)
}

// playPreset plays the speaker's NATIVE preset for the slot by pressing the
// physical preset key (PRESET_1..6). The speaker stores its own presets (shown via
// GET /api/presets), so this plays exactly what the speaker has — no dependency on the
// local store.
func (s *Server) playPreset(w http.ResponseWriter, r *http.Request) {
	slot, ok := slotOf(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	s.speaker.Wake(ctx)
	if err := s.speaker.Key(ctx, "PRESET_"+strconv.Itoa(slot)); err != nil {
		s.fail(w, "play failed", err)
		return
	}
	s.log.Info("play preset", "slot", slot)
	writeJSON(w, 200, map[string]int{"playing": slot})
}

func (s *Server) playStation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		StationID string `json:"stationId"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.StationID == "" {
		s.fail(w, "stationId required", err)
		return
	}
	s.playStationID(w, r, body.StationID, body.Name)
}

// playStationID selects a NATIVE TUNEIN ContentItem; the speaker resolves and
// streams it itself.
func (s *Server) playStationID(w http.ResponseWriter, r *http.Request, stationID, name string) {
	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	s.speaker.Wake(ctx)
	loc := "/v1/playback/station/" + stationID
	if err := s.speaker.Select(ctx, "TUNEIN", "stationurl", loc, name, ""); err != nil {
		s.fail(w, "play failed", err)
		return
	}
	s.log.Info("play", "station", stationID, "name", name)
	writeJSON(w, 200, map[string]string{"status": "playing", "station": stationID})
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	if err := s.speaker.Key(ctx, "PAUSE"); err != nil {
		s.fail(w, "stop failed", err)
		return
	}
	writeJSON(w, 200, map[string]string{"status": "stopped"})
}

func (s *Server) now(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	np, err := s.speaker.NowPlaying(ctx)
	if err != nil {
		s.fail(w, "now failed", err)
		return
	}
	writeJSON(w, 200, np)
}

func (s *Server) getVolume(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	v, err := s.speaker.Volume(ctx)
	if err != nil {
		s.fail(w, "volume failed", err)
		return
	}
	writeJSON(w, 200, map[string]int{"volume": v})
}

func (s *Server) setVolume(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Volume int `json:"volume"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.fail(w, "bad body", err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	if err := s.speaker.SetVolume(ctx, body.Volume); err != nil {
		s.fail(w, "set volume failed", err)
		return
	}
	writeJSON(w, 200, map[string]int{"volume": body.Volume})
}

// getSettings returns the speaker name + bass (with range) + UI language.
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	out := map[string]any{"language": s.settings.Get().Language}
	if info, err := s.speaker.Info(ctx); err == nil {
		out["name"] = info.Name
		out["model"] = info.Type // device model, e.g. "SoundTouch 10"
	}
	if b, err := s.speaker.Bass(ctx); err == nil {
		out["bass"] = b
	}
	writeJSON(w, 200, out)
}

// putSettings applies any provided fields: name + bass on the speaker, language in
// the local store. Fields are optional so the UI can live-apply one at a time.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     *string `json:"name"`
		Bass     *int    `json:"bass"`
		Language *string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.fail(w, "bad body", err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	if body.Name != nil && *body.Name != "" {
		if err := s.speaker.SetName(ctx, *body.Name); err != nil {
			s.fail(w, "set name failed", err)
			return
		}
	}
	if body.Bass != nil {
		if err := s.speaker.SetBass(ctx, *body.Bass); err != nil {
			s.fail(w, "set bass failed", err)
			return
		}
	}
	if body.Language != nil {
		if err := s.settings.SetLanguage(*body.Language); err != nil {
			s.fail(w, "set language failed", err)
			return
		}
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func slotOf(w http.ResponseWriter, r *http.Request) (int, bool) {
	slot, err := strconv.Atoi(r.PathValue("slot"))
	if err != nil || slot < 1 || slot > 6 {
		http.Error(w, "slot must be 1..6", http.StatusBadRequest)
		return 0, false
	}
	return slot, true
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) fail(w http.ResponseWriter, msg string, err error) {
	if err != nil {
		s.log.Warn(msg, "err", err.Error())
	}
	writeJSON(w, http.StatusBadGateway, map[string]string{"error": msg})
}
