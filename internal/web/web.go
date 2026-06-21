// Package web serves the ReTouch control UI and JSON API. Playback is
// NATIVE: presets are real TUNEIN ContentItems the speaker plays itself (resolving
// via the live TuneIn service) — STLocal never proxies audio.
package web

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/stein155/retouch/internal/discover"
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
	repo         = "stein155/retouch"
)

type releaseInfo struct {
	TagName    string `json:"tag_name"`
	Name       string `json:"name"`
	Prerelease bool   `json:"prerelease"`
	Draft      bool   `json:"draft"`
}

// betaPRRe matches the per-PR beta tag the Beta Build workflow publishes
// (beta-pr-<number>), so the app can show "PR #<n>" and accept it as a target.
var betaPRRe = regexp.MustCompile(`^beta-pr-(\d+)$`)

// betaPR extracts the PR number from a beta-pr-<n> tag.
func betaPR(tag string) (int, bool) {
	m := betaPRRe.FindStringSubmatch(tag)
	if m == nil {
		return 0, false
	}
	n, _ := strconv.Atoi(m[1])
	return n, true
}

// Server wires the UI to the speaker and TuneIn search.
type Server struct {
	tunein    *tunein.Client
	speaker   *speaker.Client
	store     *store.Store
	settings  *settings.Store
	mirror    PresetMirror
	homekit   *HomeKitInfo // set when the HomeKit bridge is enabled; nil otherwise
	mdns      Hostnamer
	log       *slog.Logger
	version   string
	homeDir   string
	startedAt time.Time
	updateMu  sync.Mutex
	ui        http.Handler // serves the embedded dist bundle
	proxy     *http.Client // for the same-origin TuneIn / logo proxies

	npMu    sync.Mutex              // guards npCache
	npCache map[string]npCacheEntry // TuneIn now-playing, keyed by station id
}

// npCacheEntry is a TuneIn now-playing lookup cached briefly so the UI's poll
// (every few seconds) doesn't hammer Describe.ashx for the same station.
type npCacheEntry struct {
	track tunein.Track
	at    time.Time
}

// npTTL is how long a TuneIn now-playing lookup stays fresh. Songs change every
// few minutes, so a short cache keeps the line current without per-poll fetches.
const npTTL = 15 * time.Second

// PresetMirror receives successful direct speaker preset writes so the local cloud
// emulation cannot later sync stale presets back to the speaker.
type PresetMirror interface {
	MirrorPreset(slot int, name, location, art string)
	RemovePreset(slot int)
}

// Hostnamer is the mDNS responder: it reports the advertised name (e.g.
// "keuken.local") and re-advertises when the speaker is renamed.
type Hostnamer interface {
	Hostname() string
	SetName(name string)
}

// New builds a Server.
func New(tc *tunein.Client, b *speaker.Client, s *store.Store, set *settings.Store, version, homeDir string, log *slog.Logger) *Server {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// dist is embedded at build time; this only fails if the build is broken.
		panic("web: embedded dist missing: " + err.Error())
	}
	return &Server{
		tunein:    tc,
		speaker:   b,
		store:     s,
		settings:  set,
		log:       log,
		version:   version,
		homeDir:   homeDir,
		startedAt: time.Now(),
		ui:        http.FileServer(http.FS(sub)),
		proxy:     &http.Client{Timeout: 12 * time.Second},
		npCache:   map[string]npCacheEntry{},
	}
}

// SetPresetMirror attaches the local cloud preset store after both servers exist.
func (s *Server) SetPresetMirror(m PresetMirror) {
	s.mirror = m
}

// HomeKitInfo is what the UI needs to show the Apple Home pairing code.
type HomeKitInfo struct {
	Enabled bool   `json:"enabled"`
	Name    string `json:"name,omitempty"`
	Code    string `json:"code,omitempty"` // XXX-XX-XXX setup code
}

// SetHomeKit makes the HomeKit pairing details available to GET /api/homekit.
func (s *Server) SetHomeKit(hk *HomeKitInfo) {
	s.homekit = hk
}

// SetMDNS attaches the mDNS responder so settings can show the .local address and
// renames re-advertise it.
func (s *Server) SetMDNS(h Hostnamer) {
	s.mdns = h
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
	mux.HandleFunc("GET /api/multiroom", s.multiroom)
	mux.HandleFunc("GET /api/multiroom/speakers", s.multiroomSpeakers)
	mux.HandleFunc("POST /api/multiroom/group", s.multiroomGroup)
	mux.HandleFunc("POST /api/multiroom/ungroup", s.multiroomUngroup)
	mux.HandleFunc("GET /api/homekit", s.homeKitInfo)
	mux.HandleFunc("GET /api/version", s.versionInfo)
	mux.HandleFunc("GET /api/releases", s.releases)
	mux.HandleFunc("GET /api/debug", s.debugBundle)
	mux.HandleFunc("POST /api/update", s.updateApp)
	// Everything else is the embedded single-page UI. More specific /api/...
	// patterns above win; this serves index.html, assets and icons.
	mux.HandleFunc("GET /", s.serveUI)
	return mux
}

// multiroom returns this speaker's identity and its current multiroom zone, so
// the UI can show whether it is grouped and which other speakers are members.
// This is the fast read (no network sweep — see multiroomSpeakers for that).
func (s *Server) multiroom(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	self, err := s.speaker.Info(ctx)
	if err != nil {
		s.fail(w, "speaker info failed", err)
		return
	}
	out := map[string]any{
		"self":     map[string]string{"deviceId": self.DeviceID, "name": self.Name, "ip": self.IP},
		"isMaster": false,
		"members":  []speaker.Member{},
	}
	if z, err := s.speaker.GetZone(ctx); err == nil {
		out["master"] = z.Master
		out["isMaster"] = z.Master != "" && z.Master == self.DeviceID
		out["members"] = z.Members
	} else {
		s.log.Warn("read zone", "err", err)
	}
	writeJSON(w, 200, out)
}

// multiroomSpeakers sweeps the local network for other SoundTouch speakers and
// returns them, each flagged with whether it is already in this speaker's zone.
// The sweep takes a couple of seconds, so the UI shows a spinner while it runs.
func (s *Server) multiroomSpeakers(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	self, err := s.speaker.Info(ctx)
	if err != nil {
		s.fail(w, "speaker info failed", err)
		return
	}
	grouped := map[string]bool{}
	if z, err := s.speaker.GetZone(ctx); err == nil && z.Master == self.DeviceID {
		for _, m := range z.Members {
			grouped[m.DeviceID] = true
		}
	}
	found, err := discover.Scan(ctx, self.IP, self.DeviceID)
	if err != nil {
		s.fail(w, "scan failed", err)
		return
	}
	type row struct {
		discover.Speaker
		Grouped bool `json:"grouped"`
	}
	rows := make([]row, 0, len(found))
	for _, sp := range found {
		rows = append(rows, row{Speaker: sp, Grouped: grouped[sp.DeviceID]})
	}
	writeJSON(w, 200, rows)
}

// multiroomGroup adds the speaker at the posted IP to a zone mastered by THIS
// speaker (creating the zone if there isn't one yet), so it plays in sync with
// whatever this speaker is playing. Uses Bose's own setZone / addZoneSlave.
func (s *Server) multiroomGroup(w http.ResponseWriter, r *http.Request) {
	ip, ok := s.zoneTargetIP(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	self, err := s.speaker.Info(ctx)
	if err != nil {
		s.fail(w, "speaker info failed", err)
		return
	}
	slaveInfo, err := speaker.New(ip).Info(ctx)
	if err != nil || slaveInfo.DeviceID == "" {
		s.fail(w, "could not reach that speaker", err)
		return
	}
	master := speaker.Member{DeviceID: self.DeviceID, IP: self.IP}
	slave := speaker.Member{DeviceID: slaveInfo.DeviceID, IP: ip}

	// Extend an existing zone we already master; otherwise establish a fresh one.
	z, _ := s.speaker.GetZone(ctx)
	if z != nil && z.Master == self.DeviceID && len(z.Members) > 0 {
		err = s.speaker.AddZoneSlave(ctx, master, []speaker.Member{slave})
	} else {
		err = s.speaker.SetZone(ctx, master, []speaker.Member{slave})
	}
	if err != nil {
		s.fail(w, "grouping failed", err)
		return
	}
	s.log.Info("multiroom group", "master", self.DeviceID, "slave", slave.DeviceID, "ip", ip)
	writeJSON(w, 200, map[string]string{"status": "grouped"})
}

// multiroomUngroup removes the speaker at the posted IP from this speaker's zone.
// The slave's deviceID is taken from the current zone (so it works even if the
// slave has since gone offline), falling back to a live probe.
func (s *Server) multiroomUngroup(w http.ResponseWriter, r *http.Request) {
	ip, ok := s.zoneTargetIP(w, r)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	self, err := s.speaker.Info(ctx)
	if err != nil {
		s.fail(w, "speaker info failed", err)
		return
	}
	slave := speaker.Member{IP: ip}
	if z, err := s.speaker.GetZone(ctx); err == nil {
		for _, m := range z.Members {
			if m.IP == ip {
				slave.DeviceID = m.DeviceID
				break
			}
		}
	}
	if slave.DeviceID == "" {
		if info, err := speaker.New(ip).Info(ctx); err == nil {
			slave.DeviceID = info.DeviceID
		}
	}
	master := speaker.Member{DeviceID: self.DeviceID, IP: self.IP}
	if err := s.speaker.RemoveZoneSlave(ctx, master, []speaker.Member{slave}); err != nil {
		s.fail(w, "ungrouping failed", err)
		return
	}
	s.log.Info("multiroom ungroup", "master", self.DeviceID, "slave", slave.DeviceID, "ip", ip)
	writeJSON(w, 200, map[string]string{"status": "ungrouped"})
}

// zoneTargetIP decodes and validates the {"ip": "..."} body shared by the
// group/ungroup endpoints.
func (s *Server) zoneTargetIP(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		IP string `json:"ip"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.fail(w, "bad body", err)
		return "", false
	}
	ip := strings.TrimSpace(body.IP)
	if net.ParseIP(ip) == nil {
		http.Error(w, "valid ip required", http.StatusBadRequest)
		return "", false
	}
	return ip, true
}

func (s *Server) versionInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{"version": s.version, "updatable": s.updatable()})
}

// releases lists what the app can update to: the latest stable release plus every
// open-PR beta the Beta Build workflow has published. The frontend turns this into
// the version picker so a beta can be installed over the air, no computer needed.
func (s *Server) releases(w http.ResponseWriter, r *http.Request) {
	all, err := s.listReleases(r.Context())
	if err != nil {
		s.fail(w, "release list failed", err)
		return
	}
	var stable map[string]any
	betas := []map[string]any{}
	for _, rel := range all {
		tag := strings.TrimSpace(rel.TagName)
		if rel.Draft || tag == "" {
			continue
		}
		if n, ok := betaPR(tag); ok && rel.Prerelease {
			name := strings.TrimSpace(rel.Name)
			if name == "" {
				name = "PR #" + strconv.Itoa(n)
			}
			betas = append(betas, map[string]any{"tag": tag, "pr": n, "name": name})
			continue
		}
		// First non-prerelease wins: the GitHub list is newest-first, so this is
		// the current stable release.
		if !rel.Prerelease && stable == nil {
			stable = map[string]any{"tag": tag, "name": strings.TrimSpace(rel.Name)}
		}
	}
	writeJSON(w, 200, map[string]any{
		"current":   s.version,
		"updatable": s.updatable(),
		"stable":    stable,
		"betas":     betas,
	})
}

// debugBundle returns a plain-text snapshot of the agent version, runtime, the
// PERSISTENT installer state, and the tails of the install/start/agent logs. It is
// meant to be opened in a browser (http://<speaker>:8080/api/debug) and pasted into a
// GitHub issue — no SSH needed. Read-only: it changes nothing on the speaker.
//
// The .gaveup / .attempts fields are the usual culprit when a speaker is stuck on an
// old release: netinstall writes .gaveup after MAX_ATTEMPTS failures and then refuses
// to retry on every later boot, so install.sh waits forever for a version that never
// arrives. Surfacing them here turns that silent dead-end into something a user can see.
func (s *Server) debugBundle(w http.ResponseWriter, r *http.Request) {
	var b strings.Builder
	fmt.Fprintf(&b, "ReTouch debug bundle\n")
	fmt.Fprintf(&b, "version    : %s\n", s.version)
	fmt.Fprintf(&b, "updatable  : %v\n", s.updatable())
	fmt.Fprintf(&b, "home       : %s\n", s.homeDir)
	fmt.Fprintf(&b, "clock      : %s\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "uptime     : %s\n", time.Since(s.startedAt).Round(time.Second))
	fmt.Fprintf(&b, "runtime    : %s %s/%s\n", runtime.Version(), runtime.GOOS, runtime.GOARCH)

	b.WriteString("\n== installer state (persistent; survives reboots) ==\n")
	fmt.Fprintf(&b, ".version   : %s\n", readFileLine(filepath.Join(s.homeDir, ".version")))
	fmt.Fprintf(&b, ".attempts  : %s\n", readFileLine(filepath.Join(s.homeDir, ".attempts")))
	gaveUp := fileExists(filepath.Join(s.homeDir, ".gaveup"))
	fmt.Fprintf(&b, ".gaveup    : %v", gaveUp)
	if gaveUp {
		b.WriteString("   <-- netinstall has given up; it will NOT retry until this is cleared")
	}
	b.WriteByte('\n')

	for _, lg := range []struct{ name, path string }{
		{"install log", "/tmp/retouch-install.log"},
		{"start log", "/tmp/retouch-start.log"},
		{"agent log", "/tmp/retouch.log"},
	} {
		fmt.Fprintf(&b, "\n== %s (%s) ==\n", lg.name, lg.path)
		b.WriteString(tailFile(lg.path, 8<<10))
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusOK)
	io.WriteString(w, b.String())
}

// readFileLine returns the first line of a small state file, or "<none>".
func readFileLine(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return "<none>"
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "<empty>"
	}
	return s
}

// tailFile returns at most max bytes from the end of path, dropping a leading partial
// line when truncated. Missing/empty files yield a short placeholder instead of an error.
func tailFile(path string, max int64) string {
	f, err := os.Open(path)
	if err != nil {
		return "<none>\n"
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "<unreadable>\n"
	}
	truncated := fi.Size() > max
	if truncated {
		if _, err := f.Seek(-max, io.SeekEnd); err != nil {
			return "<unreadable>\n"
		}
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return "<unreadable>\n"
	}
	out := string(data)
	if truncated {
		if i := strings.IndexByte(out, '\n'); i >= 0 {
			out = "…(truncated)\n" + out[i+1:]
		}
	}
	if strings.TrimSpace(out) == "" {
		return "<empty>\n"
	}
	if !strings.HasSuffix(out, "\n") {
		out += "\n"
	}
	return out
}

func (s *Server) updatable() bool {
	return filepath.IsAbs(s.homeDir) && s.homeDir != "/" && fileExists(filepath.Join(s.homeDir, "retouch"))
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func (s *Server) serveUI(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	} else {
		w.Header().Set("Cache-Control", "no-store")
	}
	s.ui.ServeHTTP(w, r)
}

func (s *Server) updateApp(w http.ResponseWriter, r *http.Request) {
	if !s.updatable() {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "updates are only available on an installed speaker"})
		return
	}
	// An optional {"tag": "..."} body targets a specific release (e.g. a beta);
	// an empty body means "latest stable", the default Update button.
	var body struct {
		Tag string `json:"tag"`
	}
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&body)
	}
	target := strings.TrimSpace(body.Tag)

	if !s.updateMu.TryLock() {
		writeJSON(w, http.StatusConflict, map[string]string{"status": "updating"})
		return
	}

	if target == "" {
		latest, err := s.latestRelease(r.Context())
		if err != nil {
			s.updateMu.Unlock()
			s.fail(w, "latest release check failed", err)
			return
		}
		if latest == "" {
			s.updateMu.Unlock()
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "latest release missing tag"})
			return
		}
		target = latest
	} else {
		// Only ever install a tag we actually publish — never an arbitrary ref —
		// so a crafted request can't point the speaker at a foreign download.
		ok, err := s.isOfferedTag(r.Context(), target)
		if err != nil {
			s.updateMu.Unlock()
			s.fail(w, "release check failed", err)
			return
		}
		if !ok {
			s.updateMu.Unlock()
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "unknown release " + target})
			return
		}
	}

	if target == s.version {
		s.updateMu.Unlock()
		writeJSON(w, 200, map[string]string{"status": "current", "version": s.version})
		return
	}

	go func(from, to string) {
		defer s.updateMu.Unlock()
		if err := s.installRelease(context.Background(), to); err != nil {
			s.log.Warn("self-update failed", "from", from, "to", to, "err", err)
			return
		}
		s.log.Info("self-update installed; restarting", "from", from, "to", to)
		s.restartAfterUpdate()
	}(s.version, target)

	writeJSON(w, 202, map[string]string{"status": "updating", "from": s.version, "to": target})
}

func (s *Server) latestRelease(ctx context.Context) (string, error) {
	var rel releaseInfo
	if err := s.getJSON(ctx, "https://api.github.com/repos/"+repo+"/releases/latest", &rel); err != nil {
		return "", err
	}
	return strings.TrimSpace(rel.TagName), nil
}

// listReleases returns the repo's releases, newest first (GitHub's order).
func (s *Server) listReleases(ctx context.Context) ([]releaseInfo, error) {
	var rels []releaseInfo
	if err := s.getJSON(ctx, "https://api.github.com/repos/"+repo+"/releases?per_page=100", &rels); err != nil {
		return nil, err
	}
	return rels, nil
}

// isOfferedTag reports whether tag is a real, non-draft release of this repo.
func (s *Server) isOfferedTag(ctx context.Context, tag string) (bool, error) {
	rels, err := s.listReleases(ctx)
	if err != nil {
		return false, err
	}
	for _, rel := range rels {
		if !rel.Draft && strings.TrimSpace(rel.TagName) == tag {
			return true, nil
		}
	}
	return false, nil
}

func (s *Server) installRelease(ctx context.Context, tag string) error {
	bin := filepath.Join(s.homeDir, "retouch")
	newBin := bin + ".new"
	sums := filepath.Join(s.homeDir, "SHA256SUMS")
	base := "https://github.com/" + repo + "/releases/download/" + tag
	if err := s.downloadFile(ctx, base+"/retouch-armv7l", newBin, 0o755); err != nil {
		return err
	}
	if err := s.downloadFile(ctx, base+"/SHA256SUMS", sums, 0o644); err != nil {
		_ = os.Remove(newBin)
		return err
	}
	want, err := checksumFor(sums, "retouch-armv7l")
	if err != nil {
		_ = os.Remove(newBin)
		return err
	}
	got, err := sha256File(newBin)
	if err != nil {
		_ = os.Remove(newBin)
		return err
	}
	if want != got {
		_ = os.Remove(newBin)
		return errChecksumMismatch{want: want, got: got}
	}
	if err := os.Rename(newBin, bin); err != nil {
		_ = os.Remove(newBin)
		return err
	}
	return os.WriteFile(filepath.Join(s.homeDir, ".version"), []byte(tag+"\n"), 0o644)
}

func (s *Server) getJSON(ctx context.Context, target string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ReTouch/"+s.version)
	resp, err := s.proxy.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errHTTPStatus{url: target, status: resp.StatusCode}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 4<<20)).Decode(out)
}

func (s *Server) downloadFile(ctx context.Context, target, path string, mode fs.FileMode) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "ReTouch/"+s.version)
	resp, err := s.proxy.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return errHTTPStatus{url: target, status: resp.StatusCode}
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func checksumFor(path, name string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(b), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			return strings.ToLower(fields[0]), nil
		}
	}
	return "", errMissingChecksum{name: name}
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Server) restartAfterUpdate() {
	start := filepath.Join(s.homeDir, "start.sh")
	if fileExists(start) {
		cmd := exec.Command("sh", "-c", "sleep 1; "+shellQuote(start)+" >/tmp/retouch-start.log 2>&1 &")
		if err := cmd.Start(); err != nil {
			s.log.Warn("schedule restart", "err", err)
			return
		}
		time.Sleep(200 * time.Millisecond)
		os.Exit(0)
	}
	s.log.Warn("start script missing after update", "path", start)
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}

type errHTTPStatus struct {
	url    string
	status int
}

func (e errHTTPStatus) Error() string { return e.url + " status " + strconv.Itoa(e.status) }

type errMissingChecksum struct{ name string }

func (e errMissingChecksum) Error() string { return "missing checksum for " + e.name }

type errChecksumMismatch struct{ want, got string }

func (e errChecksumMismatch) Error() string { return "checksum mismatch " + e.got + " != " + e.want }

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

// homeKitInfo reports whether the Apple Home bridge is on and, if so, the setup
// code to type into the Home app. Returns {"enabled":false} when HomeKit is off.
func (s *Server) homeKitInfo(w http.ResponseWriter, r *http.Request) {
	if s.homekit == nil {
		writeJSON(w, 200, HomeKitInfo{Enabled: false})
		return
	}
	writeJSON(w, 200, s.homekit)
}

func (s *Server) now(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	np, err := s.speaker.NowPlaying(ctx)
	if err != nil {
		s.fail(w, "now failed", err)
		return
	}
	s.enrichTuneIn(ctx, np)
	writeJSON(w, 200, np)
}

// enrichTuneIn fills in the song/artist/cover for a TuneIn station. The speaker
// itself no longer gets this metadata (it came from the retired Bose cloud), so
// ReTouch asks TuneIn directly and fills only the fields the speaker left blank.
// Best-effort and cached: any failure leaves np untouched.
func (s *Server) enrichTuneIn(ctx context.Context, np *speaker.NowPlaying) {
	if np == nil || !strings.HasPrefix(np.StationID, "s") {
		return
	}
	if np.PlayStatus != "" && np.PlayStatus != "PLAY_STATE" && np.PlayStatus != "BUFFERING_STATE" {
		return // nothing is playing — don't show a stale song
	}
	t, ok := s.cachedNowPlaying(ctx, np.StationID)
	if !ok {
		return
	}
	// For TuneIn radio the speaker fills Track with the station name as a
	// placeholder and never knows the artist (that came from the Bose cloud), so
	// TuneIn's live song wins whenever it has one. When TuneIn has no song we
	// leave the speaker's value alone — the UI then just shows the station.
	if t.Song != "" {
		np.Track = t.Song
		np.Artist = t.Artist
	}
	if np.Art == "" {
		if t.Art != "" {
			np.Art = t.Art
		} else {
			np.Art = t.Logo
		}
	}
}

// cachedNowPlaying returns the TuneIn now-playing for a station, served from a
// short-lived cache so repeated polls don't re-hit TuneIn for every request.
func (s *Server) cachedNowPlaying(ctx context.Context, stationID string) (tunein.Track, bool) {
	s.npMu.Lock()
	if e, ok := s.npCache[stationID]; ok && time.Since(e.at) < npTTL {
		s.npMu.Unlock()
		return e.track, true
	}
	s.npMu.Unlock()

	t, err := s.tunein.NowPlaying(ctx, stationID)
	if err != nil {
		return tunein.Track{}, false
	}
	s.npMu.Lock()
	s.npCache[stationID] = npCacheEntry{track: t, at: time.Now()}
	s.npMu.Unlock()
	return t, true
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
	if s.mdns != nil {
		out["host"] = s.mdns.Hostname() // friendly .local address, e.g. "keuken.local"
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
		if s.mdns != nil {
			s.mdns.SetName(*body.Name) // re-advertise <name>.local
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
