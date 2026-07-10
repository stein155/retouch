// Package web serves the ReTouch control UI and JSON API. Playback is
// NATIVE: presets are real TUNEIN ContentItems the speaker plays itself (resolving
// via the live TuneIn service) — STLocal never proxies audio.
package web

import (
	"context"
	"embed"
	"encoding/json"
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

	"github.com/stein155/retouch/internal/artwork"
	"github.com/stein155/retouch/internal/discover"
	"github.com/stein155/retouch/internal/icy"
	"github.com/stein155/retouch/internal/plugins"
	"github.com/stein155/retouch/internal/release"
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

// releasePublicKey is the base64-encoded ed25519 public key that release SHA256SUMS
// files are signed with. When set, a self-update refuses to install unless the
// release ships a valid SHA256SUMS.sig — so a checksum that merely matches the
// (same-origin) SHA256SUMS is no longer enough; the binary must be signed by the
// holder of the private key, closing the "compromised GitHub release → root on
// every speaker" gap that TLS + checksum alone leave open.
//
// Empty (the default) keeps the prior behaviour (TLS + checksum only). To enable
// signing, generate a keypair, put the public half here, and sign releases in CI
// with the private half — see docs/RELEASE_SIGNING.md.
const releasePublicKey = "vmNYVuOZnDN7P7ipK43aJNZ2R6tT1IRX6TXvw7+IvX8="

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
	mdns      Hostnamer
	mqtt      MQTTBridge
	telnetFW  func(closed bool) error // applies/removes the LAN block on :17000
	sessions  *sessionStore           // settings-login sessions (see auth.go)
	loginMu   sync.Mutex              // serializes password verifies (see verifyPassword)
	log       *slog.Logger
	version   string
	homeDir   string
	startedAt time.Time
	updateMu  sync.Mutex
	ui        http.Handler     // serves the embedded dist bundle
	proxy     *http.Client     // for the same-origin TuneIn / logo proxies + artwork
	stream    *http.Client     // reads ICY metadata off the audio stream
	hub       *hub             // pushes live state to browsers over SSE (/api/events)
	plugins   *plugins.Manager // installs/supervises/proxies plugins; nil off-speaker
	sideload  bool             // allow unverified plugin uploads (-allow-sideload)

	npMu       sync.Mutex                // guards npCache and streamURLs
	npCache    map[string]npCacheEntry   // now-playing, keyed by station id
	streamURLs map[string]streamURLEntry // resolved stream URL, keyed by station id
}

// npCacheEntry is a now-playing lookup cached briefly so the UI's poll (every
// few seconds) doesn't re-read the stream for the same station. fetching guards
// against launching a second background refresh while one is already in flight.
type npCacheEntry struct {
	song, artist, art string
	at                time.Time
	fetching          bool
}

// streamURLEntry caches the stream URL resolved from TuneIn so each poll doesn't
// re-hit Tune.ashx; the URL (and any embedded token) is stable for a while.
type streamURLEntry struct {
	url string
	at  time.Time
}

// npTTL is how long a now-playing lookup stays fresh. Songs change every few
// minutes, so a short cache keeps the line current without per-poll stream reads.
const npTTL = 15 * time.Second

const updateDownloadTimeout = 5 * time.Minute

// streamURLTTL is how long a resolved stream URL is reused before re-resolving.
const streamURLTTL = 5 * time.Minute

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

// MQTTBridge is the Home Assistant MQTT bridge (internal/habridge). The web layer
// only needs to nudge it to reconnect after a settings change and read its status;
// it stays an interface so web doesn't depend on the bridge's concrete type.
type MQTTBridge interface {
	Reload()
	Status() (connected bool, lastErr string)
}

// New builds a Server.
func New(tc *tunein.Client, b *speaker.Client, s *store.Store, set *settings.Store, version, homeDir string, log *slog.Logger) *Server {
	sub, err := fs.Sub(distFS, "dist")
	if err != nil {
		// dist is embedded at build time; this only fails if the build is broken.
		panic("web: embedded dist missing: " + err.Error())
	}
	srv := &Server{
		tunein:     tc,
		speaker:    b,
		store:      s,
		settings:   set,
		log:        log,
		version:    version,
		homeDir:    homeDir,
		startedAt:  time.Now(),
		ui:         http.FileServer(http.FS(sub)),
		proxy:      &http.Client{Timeout: 12 * time.Second, Transport: publicOnlyTransport()},
		stream:     &http.Client{Timeout: 12 * time.Second, Transport: publicOnlyTransport()},
		npCache:    map[string]npCacheEntry{},
		streamURLs: map[string]streamURLEntry{},
	}
	srv.hub = newHub(srv.pollState, log.With("comp", "events"))
	srv.telnetFW = telnetFirewall
	srv.sessions = openSessions(filepath.Join(homeDir, ".sessions"))
	return srv
}

// SetTelnetFirewall overrides how the :17000 LAN block is applied/removed.
// Tests inject a fake so toggling the setting never runs iptables.
func (s *Server) SetTelnetFirewall(f func(closed bool) error) {
	s.telnetFW = f
}

// Run drives the background services the Server owns — currently the SSE hub's
// speaker poll loop — until ctx is cancelled. Call it once, in a goroutine.
func (s *Server) Run(ctx context.Context) {
	// Re-apply the telnet block right away on every start: the iptables rule does
	// not survive a reboot, only the marker file does.
	if fileExists(filepath.Join(s.homeDir, ".close-telnet")) {
		if err := s.telnetFW(true); err != nil {
			s.log.Warn("close telnet at startup failed", "err", err)
		} else {
			s.log.Info("closed LAN telnet", "port", 17000)
		}
	}
	s.hub.run(ctx)
}

// SetPresetMirror attaches the local cloud preset store after both servers exist.
func (s *Server) SetPresetMirror(m PresetMirror) {
	s.mirror = m
}

// SetMDNS attaches the mDNS responder so settings can show the .local address and
// renames re-advertise it.
func (s *Server) SetMDNS(h Hostnamer) {
	s.mdns = h
}

// SetMQTTBridge attaches the Home Assistant MQTT bridge so settings changes can
// trigger a reconnect and the UI can show connection status.
func (s *Server) SetMQTTBridge(b MQTTBridge) {
	s.mqtt = b
}

// SetPlugins attaches the plugin host so the UI can list/install/remove plugins and
// their config APIs can be reverse-proxied. Nil (the default, off-speaker) disables
// the plugin endpoints. allowSideload additionally enables installing an uploaded,
// UNVERIFIED binary; it must only be set from an explicit start-up flag — never from
// a runtime setting, which the same LAN attacker it defends against could flip.
// Call before Handler().
func (s *Server) SetPlugins(m *plugins.Manager, allowSideload bool) {
	s.plugins = m
	s.sideload = allowSideload
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
	mux.HandleFunc("GET /api/events", s.events)
	mux.HandleFunc("GET /api/volume", s.getVolume)
	mux.HandleFunc("POST /api/volume", s.setVolume)
	mux.HandleFunc("GET /api/auth", s.getAuth)
	mux.HandleFunc("POST /api/auth/login", s.login)
	mux.HandleFunc("POST /api/auth/logout", s.logout)
	mux.HandleFunc("POST /api/auth/password", s.requireAuth(s.setPassword))
	// The settings side of the API sits behind the admin password (when one is
	// set): everything reachable from the settings sheet that can change the
	// speaker's configuration or leak network details. Playback, presets, volume
	// and multiroom stay open — the radio keeps working without a login.
	mux.HandleFunc("GET /api/settings", s.getSettings)
	mux.HandleFunc("PUT /api/settings", s.requireAuth(s.putSettings))
	mux.HandleFunc("GET /api/mqtt/status", s.requireAuth(s.mqttStatus))
	mux.HandleFunc("GET /api/multiroom", s.multiroom)
	mux.HandleFunc("GET /api/multiroom/speakers", s.multiroomSpeakers)
	mux.HandleFunc("POST /api/multiroom/group", s.multiroomGroup)
	mux.HandleFunc("POST /api/multiroom/ungroup", s.multiroomUngroup)
	mux.HandleFunc("GET /api/version", s.versionInfo)
	mux.HandleFunc("GET /api/releases", s.releases)
	mux.HandleFunc("GET /api/debug", s.requireAuth(s.debugBundle))
	mux.HandleFunc("POST /api/update", s.requireAuth(s.updateApp))

	// Plugins: list/install/remove, plus a reverse proxy of every other subpath to
	// the plugin's own loopback API. The install/remove verbs are more specific than
	// the {path...} catch-all, so the mux routes them first; the plugin's manifest,
	// health and config endpoints all fall through to the proxy. The proxy is
	// registered per method (not as an all-method catch-all) so its pattern stays
	// comparable to "GET /" — an all-method catch-all conflicts with it and panics.
	mux.HandleFunc("GET /api/plugins", s.listPlugins)
	mux.HandleFunc("GET /api/plugins/{name}/latest", s.pluginLatest)
	mux.HandleFunc("POST /api/plugins/{name}/install", s.installPlugin)
	mux.HandleFunc("POST /api/plugins/{name}/upload", s.uploadPlugin)
	mux.HandleFunc("DELETE /api/plugins/{name}", s.removePlugin)
	for _, method := range []string{"GET", "POST", "PUT", "PATCH", "DELETE"} {
		mux.HandleFunc(method+" /api/plugins/{name}/{path...}", s.proxyPlugin)
	}
	// Everything else is the embedded single-page UI. More specific /api/...
	// patterns above win; this serves index.html, assets and icons.
	mux.HandleFunc("GET /", s.serveUI)
	return s.guard(mux)
}

// maxRequestBody caps the body of a mutating request. The JSON/XML we accept is
// tiny (a few settings fields, a station id); anything larger is a mistake or an
// attempt to drive a large allocation on the memory-constrained speaker.
const maxRequestBody = 64 << 10

// guard wraps the API with the only access controls a LAN-only, login-less service
// still needs: it blocks DNS-rebinding (a Host that isn't this speaker) and CSRF (a
// cross-origin mutating request), and bounds request bodies. Without these, a web
// page a LAN user merely visits could drive POST/PUT endpoints — e.g. repoint the
// speaker at an attacker's MQTT broker via PUT /api/settings, or force a downgrade
// via POST /api/update — from a single drive-by.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !hostAllowed(r.Host) {
			// A rebinding attack reaches us under the attacker's own hostname; a
			// legitimate client uses the speaker's IP or its <name>.local.
			http.Error(w, "forbidden host", http.StatusForbidden)
			return
		}
		switch r.Method {
		case http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodPatch:
			// Browsers send Origin on every cross-origin mutating request (including
			// "simple" text/plain POSTs), so a mismatch means CSRF. Sec-Fetch-Site is
			// the belt to Origin's braces on browsers that send it.
			if origin := r.Header.Get("Origin"); origin != "" {
				u, err := url.Parse(origin)
				if err != nil || u.Host != r.Host {
					http.Error(w, "cross-origin request refused", http.StatusForbidden)
					return
				}
			} else if sfs := r.Header.Get("Sec-Fetch-Site"); sfs != "" && sfs != "same-origin" && sfs != "none" {
				http.Error(w, "cross-site request refused", http.StatusForbidden)
				return
			}
			// A sideloaded plugin binary is multi-MB; every other mutating request is
			// tiny. Cap the upload generously but bound everything else tightly.
			limit := int64(maxRequestBody)
			if strings.HasPrefix(r.URL.Path, "/api/plugins/") && strings.HasSuffix(r.URL.Path, "/upload") {
				limit = pluginUploadMax
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed reports whether the request's Host names this speaker: an IP literal,
// localhost (the on-speaker agent), or a <name>.local mDNS address. A real public
// domain is rejected, which is what defeats DNS rebinding.
func hostAllowed(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return false
	}
	if host == "localhost" || strings.HasSuffix(host, ".local") {
		return true
	}
	return net.ParseIP(host) != nil
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
	if slave.DeviceID == "" {
		// Without a device id the firmware ignores the removal; don't report
		// "ungrouped" for a speaker that was never removed.
		s.fail(w, "could not identify that speaker", nil)
		return
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
		s.badRequest(w, "bad body", err)
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
	fmt.Fprintf(&b, ".close-telnet: %v\n", fileExists(filepath.Join(s.homeDir, ".close-telnet")))
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

	s.startUpdate(s.version, target)
	writeJSON(w, 202, map[string]string{"status": "updating", "from": s.version, "to": target})
}

// startUpdate installs release `to` and restarts, in the background. The caller must
// already hold updateMu (via TryLock); startUpdate hands the lock off to the
// goroutine and releases it when the install finishes or fails.
func (s *Server) startUpdate(from, to string) {
	go func() {
		defer s.updateMu.Unlock()
		if err := s.installRelease(context.Background(), to); err != nil {
			s.log.Warn("self-update failed", "from", from, "to", to, "err", err)
			return
		}
		s.log.Info("self-update installed; restarting", "from", from, "to", to)
		s.restartAfterUpdate()
	}()
}

// UpdateInfo reports the running version, the latest available stable release, its
// release URL, and whether updating is possible here (only on an installed speaker).
// It backs the Home Assistant `update` entity so HA can show an update notification.
// Off-speaker (not updatable) it reports latest == installed and skips the GitHub
// call, so no false update is offered.
func (s *Server) UpdateInfo(ctx context.Context) (installed, latest, releaseURL string, updatable bool, err error) {
	installed = s.version
	updatable = s.updatable()
	if !updatable {
		return installed, installed, "", false, nil
	}
	latest, err = s.latestRelease(ctx)
	if err != nil {
		return installed, "", "", true, err
	}
	if latest == "" {
		latest = installed
	}
	if latest != installed {
		releaseURL = "https://github.com/" + repo + "/releases/tag/" + latest
	}
	return installed, latest, releaseURL, true, nil
}

// UpdateToLatest installs the latest stable release and restarts, reusing the same
// path as POST /api/update. It backs the Home Assistant update entity's Install
// action. It returns quickly: the download + restart run in the background on success.
func (s *Server) UpdateToLatest(ctx context.Context) error {
	if !s.updatable() {
		return fmt.Errorf("updates are only available on an installed speaker")
	}
	if !s.updateMu.TryLock() {
		return fmt.Errorf("an update is already in progress")
	}
	target, err := s.latestRelease(ctx)
	if err != nil {
		s.updateMu.Unlock()
		return fmt.Errorf("latest release check failed: %w", err)
	}
	if target == "" {
		s.updateMu.Unlock()
		return fmt.Errorf("latest release missing tag")
	}
	if target == s.version {
		s.updateMu.Unlock()
		return nil // already current
	}
	s.startUpdate(s.version, target)
	return nil
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
	// When signing is enabled, the checksums file itself must carry a valid
	// signature before we trust any checksum in it. Betas are exempt: they are
	// built from PR code by the Beta Build workflow, which must never be given
	// the release signing key, so a beta ships no SHA256SUMS.sig. Betas stay
	// gated by being maintainer-triggered, opt-in prereleases over TLS + checksum.
	if _, isBeta := betaPR(tag); releasePublicKey != "" && !isBeta {
		sig := filepath.Join(s.homeDir, "SHA256SUMS.sig")
		if err := s.downloadFile(ctx, base+"/SHA256SUMS.sig", sig, 0o644); err != nil {
			_ = os.Remove(newBin)
			return err
		}
		if err := verifyReleaseSignature(releasePublicKey, sums, sig); err != nil {
			_ = os.Remove(newBin)
			return err
		}
	}
	want, err := release.ChecksumFor(sums, "retouch-armv7l")
	if err != nil {
		_ = os.Remove(newBin)
		return err
	}
	if err := release.VerifyChecksum(newBin, want); err != nil {
		_ = os.Remove(newBin)
		return err
	}
	// Keep the outgoing binary as retouch.old so a bad release can be rolled back
	// by hand (best-effort; a first install has none). Restore it if the swap fails,
	// so we never leave the speaker with no binary.
	old := bin + ".old"
	hadOld := fileExists(bin)
	if hadOld {
		_ = os.Rename(bin, old)
	}
	if err := os.Rename(newBin, bin); err != nil {
		_ = os.Remove(newBin)
		if hadOld {
			_ = os.Rename(old, bin)
		}
		return err
	}
	return os.WriteFile(filepath.Join(s.homeDir, ".version"), []byte(tag+"\n"), 0o644)
}

// verifyReleaseSignature is a thin alias over release.VerifySignature (kept so the
// on-speaker OTA reads naturally at the call site and the signature test targets it).
func verifyReleaseSignature(pubKeyB64, sumsPath, sigPath string) error {
	return release.VerifySignature(pubKeyB64, sumsPath, sigPath)
}

func (s *Server) getJSON(ctx context.Context, target string, out any) error {
	return release.GetJSON(ctx, s.proxy, "ReTouch/"+s.version, target, out)
}

func (s *Server) downloadFile(ctx context.Context, target, path string, mode fs.FileMode) error {
	ctx, cancel := context.WithTimeout(ctx, updateDownloadTimeout)
	defer cancel()
	client := &http.Client{Timeout: updateDownloadTimeout}
	return release.Download(ctx, client, "ReTouch/"+s.version, target, path, mode)
}

func (s *Server) restartAfterUpdate() {
	// Stop plugin children first: os.Exit skips context cancellation, so without
	// this they'd be orphaned to init and duplicated by the relaunched ReTouch
	// (two Ring agents then invalidate each other's rotating refresh token).
	if s.plugins != nil {
		s.plugins.Shutdown(3 * time.Second)
	}
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
	// Logos live on public CDNs. Refuse loopback/LAN targets so the proxy can't
	// be used to reach the loopback-only marge stub or other LAN-internal hosts.
	if !publicHost(r.Context(), u.Hostname()) {
		http.Error(w, "bad url", http.StatusBadRequest)
		return
	}
	s.relay(w, r, u.String(), "image/png", logoMaxBytes)
}

// publicHost reports whether host resolves exclusively to globally routable
// addresses — i.e. it is not loopback, RFC1918/ULA, link-local or unspecified.
// It is a fast up-front reject; publicOnlyTransport is the authoritative guard
// (it re-checks the actual address dialed, so a redirect or a DNS rebind between
// this check and the connection can't slip a private target through).
func publicHost(ctx context.Context, host string) bool {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(ips) == 0 {
		return false
	}
	for _, ip := range ips {
		if !release.PublicIP(ip.IP) {
			return false
		}
	}
	return true
}

// publicOnlyTransport is the transport for the outbound proxy client (logo/TuneIn
// mirrors, artwork + GitHub lookups): its dialer rejects any non-public address,
// per hop, closing the SSRF holes a pre-flight hostname check alone leaves open.
func publicOnlyTransport() *http.Transport { return release.SafeTransport() }

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
	// Never reflect the upstream Content-Type verbatim: this proxy fetches
	// attacker-chosen public URLs, and echoing e.g. "text/html" back on the
	// speaker's own origin would let a crafted logo URL execute script here.
	// Keep the upstream type only when it stays within the expected family
	// (e.g. image/jpeg for a logo), otherwise fall back to the safe default,
	// and forbid MIME sniffing so the declared type is authoritative.
	ct := fallbackCT
	if up := resp.Header.Get("Content-Type"); up != "" {
		if fam := fallbackCT[:strings.IndexByte(fallbackCT, '/')+1]; strings.HasPrefix(up, fam) {
			ct = up
		}
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("X-Content-Type-Options", "nosniff")
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
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil || !validStationID(p.StationID) {
		s.badRequest(w, "valid stationId required", err)
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
	// Keep the local store in sync: it is the fallback GET /api/presets serves
	// when the speaker is unreachable, so it must reflect edits made here.
	if err := s.store.Set(p); err != nil {
		s.log.Warn("mirror preset to local store", "err", err)
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
	if err := s.store.Remove(slot); err != nil {
		s.log.Warn("remove preset from local store", "err", err)
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
	s.hub.nudge() // push the new playback state to browsers at once
	writeJSON(w, 200, map[string]int{"playing": slot})
}

func (s *Server) playStation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		StationID string `json:"stationId"`
		Name      string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !validStationID(body.StationID) {
		s.badRequest(w, "valid stationId required", err)
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
	s.hub.nudge() // push the new playback state to browsers at once
	writeJSON(w, 200, map[string]string{"status": "playing", "station": stationID})
}

func (s *Server) stop(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	if err := s.speaker.Key(ctx, "PAUSE"); err != nil {
		s.fail(w, "stop failed", err)
		return
	}
	s.hub.nudge() // push the stopped state to browsers at once
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
	s.enrichNowPlaying(np)
	writeJSON(w, 200, np)
}

// EnrichedNowPlaying returns the current now-playing with the live song/artist/
// cover filled in from the stream metadata — the same view the web UI shows. The
// MQTT bridge uses it so Home Assistant sees the track, not just the station name
// (the speaker itself no longer receives track metadata; see enrichNowPlaying).
func (s *Server) EnrichedNowPlaying(ctx context.Context) (*speaker.NowPlaying, error) {
	np, err := s.speaker.NowPlaying(ctx)
	if err != nil {
		return nil, err
	}
	s.enrichNowPlaying(np)
	return np, nil
}

// enrichNowPlaying fills in the song/artist/cover for a playing station. The
// speaker no longer gets this metadata (it came from the retired Bose cloud), so
// ReTouch reads it from the standard ICY stream metadata (see internal/icy),
// falling back to TuneIn, and looks up cover art generically (see
// internal/artwork). The read happens in the
// background so the poll stays fast; this call only applies whatever is cached
// and kicks off a refresh when the entry is stale. Best-effort: with nothing
// cached yet the UI just shows the station until the next poll fills it in.
func (s *Server) enrichNowPlaying(np *speaker.NowPlaying) {
	if np == nil || !strings.HasPrefix(np.StationID, "s") {
		return
	}
	if np.PlayStatus != "" && np.PlayStatus != "PLAY_STATE" && np.PlayStatus != "BUFFERING_STATE" {
		return // nothing is playing — don't show a stale song
	}
	id := np.StationID
	s.npMu.Lock()
	e, ok := s.npCache[id]
	if (!ok || time.Since(e.at) >= npTTL) && !e.fetching {
		e.fetching = true
		s.npCache[id] = e
		go s.refreshNowPlaying(id)
	}
	s.npMu.Unlock()

	if !ok {
		return
	}
	// The speaker fills Track with the station name as a placeholder and never
	// knows the artist, so the stream's live song wins whenever it has one.
	if e.song != "" {
		np.Track = e.song
		np.Artist = e.artist
		// Some streams (e.g. NPO) put the programme name where the artist goes;
		// when that just repeats the station, drop it rather than show it twice.
		if strings.EqualFold(strings.TrimSpace(np.Artist), strings.TrimSpace(np.Station)) {
			np.Artist = ""
		}
		// The speaker's own art is the generic station logo; our track cover is
		// more specific, so it takes priority when we have one.
		if e.art != "" {
			np.Art = e.art
		}
	} else if np.Art == "" && e.art != "" {
		np.Art = e.art
	}
}

// refreshNowPlaying reads the current track off the station's stream and looks
// up cover art, then stores the result. Runs in its own goroutine off a fresh
// context (the request that triggered it has already returned). Any failure
// caches an empty entry so the poll doesn't retry until the TTL lapses.
func (s *Server) refreshNowPlaying(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	var e npCacheEntry
	// Primary: the standard ICY metadata in the stream itself.
	if url := s.streamURL(ctx, id); url != "" {
		if title, err := icy.StreamTitle(ctx, s.stream, url); err != nil {
			s.invalidateStreamURL(id) // stale URL / expired token — re-resolve next time
		} else {
			e.artist, e.song = icy.SplitArtistTitle(title)
		}
	}
	// Fallback: TuneIn's Describe when the stream carried no track metadata.
	if e.song == "" {
		if t, err := s.tunein.NowPlaying(ctx, id); err == nil && t.Song != "" {
			e.song, e.artist, e.art = t.Song, t.Artist, t.Art
		}
	}
	// Cover art for whatever we found: look it up generically when we don't
	// already have one (ICY carries none; TuneIn sometimes does).
	if e.song != "" && e.art == "" {
		term := strings.TrimSpace(e.artist + " " + e.song)
		if art, err := artwork.Search(ctx, s.proxy, term); err == nil {
			e.art = art
		}
	}
	e.at = time.Now()

	s.npMu.Lock()
	// The cache only needs the stations currently on screen; drop expired
	// entries once it grows past that so it can't accumulate for months.
	if len(s.npCache) > 64 {
		for k, v := range s.npCache {
			if k != id && time.Since(v.at) >= npTTL && !v.fetching {
				delete(s.npCache, k)
			}
		}
	}
	s.npCache[id] = e
	s.npMu.Unlock()
}

// streamURL returns the station's playable stream URL, resolved via TuneIn and
// cached so each poll doesn't re-resolve. Empty on failure.
func (s *Server) streamURL(ctx context.Context, id string) string {
	s.npMu.Lock()
	if e, ok := s.streamURLs[id]; ok && time.Since(e.at) < streamURLTTL {
		s.npMu.Unlock()
		return e.url
	}
	s.npMu.Unlock()

	urls, err := s.tunein.Resolve(ctx, id)
	if err != nil {
		return ""
	}
	u := tunein.PlayableURL(urls)
	if u == "" {
		return ""
	}
	s.npMu.Lock()
	s.streamURLs[id] = streamURLEntry{url: u, at: time.Now()}
	s.npMu.Unlock()
	return u
}

// invalidateStreamURL drops a cached stream URL so it is re-resolved next time.
func (s *Server) invalidateStreamURL(id string) {
	s.npMu.Lock()
	delete(s.streamURLs, id)
	s.npMu.Unlock()
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
		s.badRequest(w, "bad body", err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 4*time.Second)
	defer cancel()
	if err := s.speaker.SetVolume(ctx, body.Volume); err != nil {
		s.fail(w, "set volume failed", err)
		return
	}
	s.hub.nudge() // push the new volume to other browsers at once
	writeJSON(w, 200, map[string]int{"volume": body.Volume})
}

// getSettings returns the speaker name + bass (with range) + UI language, plus any
// device-specific settings this speaker exposes (treble, Wi-Fi/streaming
// optimization) and its current network connection.
func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	authorized := s.authorized(r)
	out := map[string]any{
		"language":      s.settings.Get().Language,
		"hasPassword":   s.settings.Get().Auth.PasswordHash != "",
		"authenticated": authorized,
	}
	info, infoErr := s.speaker.Info(ctx)
	if infoErr == nil {
		out["name"] = info.Name
	}
	if !authorized {
		// Not logged in: only what the app itself needs at startup (language +
		// speaker name). Everything else — network details, MQTT config, telnet
		// state — waits behind the login.
		writeJSON(w, 200, out)
		return
	}
	out["closeTelnet"] = fileExists(filepath.Join(s.homeDir, ".close-telnet"))
	if infoErr == nil {
		out["model"] = info.Type // device model, e.g. "SoundTouch 10"
	}
	if b, err := s.speaker.Bass(ctx); err == nil {
		out["bass"] = b
	}
	// Device-specific settings: included only when this speaker actually exposes
	// them, so the UI can hide controls the hardware doesn't support.
	if tr, err := s.speaker.Treble(ctx); err == nil {
		out["treble"] = tr
	}
	if opt, err := s.speaker.WifiOptimized(ctx); err == nil {
		out["wifiOptimization"] = opt
	}
	if net, err := s.speaker.NetworkInfo(ctx); err == nil {
		out["network"] = net
	}
	if s.mdns != nil {
		out["host"] = s.mdns.Hostname() // friendly .local address, e.g. "keuken.local"
	}
	out["mqtt"] = s.mqttSettingsView()
	writeJSON(w, 200, out)
}

// mqttSettingsView is the MQTT config as returned to the UI: the password is never
// sent back (only whether one is set), and the live connection status is folded in.
func (s *Server) mqttSettingsView() map[string]any {
	m := s.settings.Get().MQTT
	view := map[string]any{
		"enabled":         m.Enabled,
		"host":            m.Host,
		"port":            m.Port,
		"username":        m.Username,
		"hasPassword":     m.Password != "",
		"baseTopic":       m.BaseTopic,
		"discoveryPrefix": m.DiscoveryPrefix,
		"tls":             m.TLS,
	}
	if s.mqtt != nil {
		connected, lastErr := s.mqtt.Status()
		view["connected"] = connected
		view["lastError"] = lastErr
	}
	return view
}

// mqttStatus reports the live broker connection state so the settings UI can show
// whether the Home Assistant link is up.
func (s *Server) mqttStatus(w http.ResponseWriter, r *http.Request) {
	if s.mqtt == nil {
		writeJSON(w, 200, map[string]any{"connected": false, "lastError": ""})
		return
	}
	connected, lastErr := s.mqtt.Status()
	writeJSON(w, 200, map[string]any{"connected": connected, "lastError": lastErr})
}

// putSettings applies any provided fields: name + bass + treble + Wi-Fi
// optimization on the speaker, language in the local store. Fields are optional so
// the UI can live-apply one at a time.
func (s *Server) putSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name             *string `json:"name"`
		Bass             *int    `json:"bass"`
		Treble           *int    `json:"treble"`
		WifiOptimization *bool   `json:"wifiOptimization"`
		Language         *string `json:"language"`
		CloseTelnet      *bool   `json:"closeTelnet"`
		MQTT             *struct {
			Enabled         *bool   `json:"enabled"`
			Host            *string `json:"host"`
			Port            *int    `json:"port"`
			Username        *string `json:"username"`
			Password        *string `json:"password"`
			BaseTopic       *string `json:"baseTopic"`
			DiscoveryPrefix *string `json:"discoveryPrefix"`
			TLS             *bool   `json:"tls"`
		} `json:"mqtt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.badRequest(w, "bad body", err)
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
	if body.Treble != nil {
		if err := s.speaker.SetTreble(ctx, *body.Treble); err != nil {
			s.fail(w, "set treble failed", err)
			return
		}
	}
	if body.WifiOptimization != nil {
		if err := s.speaker.SetWifiOptimized(ctx, *body.WifiOptimization); err != nil {
			s.fail(w, "set wifi optimization failed", err)
			return
		}
	}
	if body.Language != nil {
		if err := s.settings.SetLanguage(*body.Language); err != nil {
			s.fail(w, "set language failed", err)
			return
		}
	}
	if body.CloseTelnet != nil {
		if err := s.setCloseTelnet(*body.CloseTelnet); err != nil {
			s.fail(w, "set telnet close failed", err)
			return
		}
	}
	if body.MQTT != nil {
		// Start from the stored config and override only the fields the UI sent, so
		// an omitted password (never returned to the UI) is preserved.
		cfg := s.settings.Get().MQTT
		p := body.MQTT
		if p.Enabled != nil {
			cfg.Enabled = *p.Enabled
		}
		if p.Host != nil {
			cfg.Host = strings.TrimSpace(*p.Host)
		}
		if p.Port != nil {
			cfg.Port = *p.Port
		}
		if p.Username != nil {
			cfg.Username = *p.Username
		}
		if p.Password != nil {
			cfg.Password = *p.Password
		}
		if p.BaseTopic != nil {
			cfg.BaseTopic = strings.TrimSpace(*p.BaseTopic)
		}
		if p.DiscoveryPrefix != nil {
			cfg.DiscoveryPrefix = strings.TrimSpace(*p.DiscoveryPrefix)
		}
		if p.TLS != nil {
			cfg.TLS = *p.TLS
		}
		if err := s.settings.SetMQTT(cfg); err != nil {
			s.fail(w, "set mqtt failed", err)
			return
		}
		if s.mqtt != nil {
			s.mqtt.Reload() // apply the new config: reconnect (or disconnect)
		}
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

// setCloseTelnet applies the toggle immediately: the marker file only makes the
// choice survive a reboot, the firewall rule is what actually closes the port.
// Enabling is open to anyone; disabling goes through the settings login (the PUT
// handler is auth-gated), which is what makes "close telnet" trustworthy.
func (s *Server) setCloseTelnet(on bool) error {
	path := filepath.Join(s.homeDir, ".close-telnet")
	if on {
		if err := os.WriteFile(path, []byte("1\n"), 0o644); err != nil {
			return err
		}
		if err := s.telnetFW(true); err != nil {
			return err
		}
		s.log.Info("closed LAN telnet", "port", 17000)
		return nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := s.telnetFW(false); err != nil {
		return err
	}
	s.log.Info("reopened LAN telnet", "port", 17000)
	return nil
}

// telnetFirewall installs or removes the raw-table DROP that hides the :17000
// diagnostic CLI from the LAN (loopback stays open), de-duplicating any earlier
// copies first.
func telnetFirewall(closed bool) error {
	script := "while iptables -t raw -D PREROUTING ! -i lo -p tcp --dport 17000 -j DROP 2>/dev/null; do :; done"
	if closed {
		script += "; iptables -t raw -I PREROUTING 1 ! -i lo -p tcp --dport 17000 -j DROP"
	}
	return exec.Command("sh", "-c", script).Run()
}

// stationIDRe matches a TuneIn guide id (e.g. "s6712"). The id is interpolated
// into the ContentItem location and, via marge, into the upstream Tune.ashx
// query, so anything beyond plain alphanumerics is rejected up front.
var stationIDRe = regexp.MustCompile(`^[A-Za-z0-9]{1,64}$`)

func validStationID(id string) bool { return stationIDRe.MatchString(id) }

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

// badRequest reports a client error (malformed body, missing field) as a 400,
// so callers can tell their own mistakes apart from speaker/upstream failures.
func (s *Server) badRequest(w http.ResponseWriter, msg string, err error) {
	if err != nil {
		s.log.Warn(msg, "err", err.Error())
	}
	writeJSON(w, http.StatusBadRequest, map[string]string{"error": msg})
}
