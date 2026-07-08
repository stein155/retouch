// Package plugins turns ReTouch into a runtime plugin host. A plugin is a separate
// binary — its own GitHub release, its own dependencies — that ReTouch downloads,
// verifies (checksum + optional ed25519 signature, exactly like its own OTA),
// stores under <homeDir>/plugins/<name>/, and runs as a supervised child process.
//
// ReTouch launches each plugin on a private loopback port and reverse-proxies
// /api/plugins/<name>/… to it, so the plugin's config lives behind the same guard
// as the rest of the API. The plugin describes its own settings UI at GET /manifest;
// ReTouch renders that generically and ships no plugin-specific frontend code, which
// is what lets a plugin be added to an already-installed speaker without a rebuild.
//
// Because ReTouch owns the child's lifecycle, plugins need no autostart of their own:
// they live and die with ReTouch, come back on reboot because ReTouch does, and are
// removed cleanly by stopping the child and deleting the directory.
package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"log/slog"

	"github.com/stein155/retouch/internal/atomicjson"
	"github.com/stein155/retouch/internal/release"
)

// binName is the fixed on-disk name each plugin binary is stored under, so the
// supervisor doesn't have to remember the release asset name to launch it.
const binName = "bin"

// stateFile is the manifest of installed plugins, kept in the plugins dir.
const stateFile = "plugins.json"

// Installed is one plugin recorded in plugins.json.
type Installed struct {
	Name    string `json:"name"`    // stable id; also the proxy path segment and dir
	Repo    string `json:"repo"`    // owner/name — the GitHub release source
	Asset   string `json:"asset"`   // armv7l binary asset in that release
	PubKey  string `json:"pubkey"`  // base64 ed25519 key; "" means checksum-only
	Version string `json:"version"` // installed release tag ("sideloaded" for uploads)
	Enabled bool   `json:"enabled"`

	// Sideloaded marks a binary uploaded through the UI rather than fetched from a
	// verified release — used while a plugin's repo is still private. Such a plugin
	// has no Repo/Asset, so it can't be updated over the air.
	Sideloaded bool `json:"sideloaded,omitempty"`
}

// Status is the live view of a plugin for the UI: what's installed plus whether its
// child process is currently up and, if not, the last error seen.
type Status struct {
	Installed
	Running bool   `json:"running"`
	LastErr string `json:"lastErr,omitempty"`
}

// proc is the runtime state of one supervised child. done is closed when the
// supervisor goroutine returns (child dead, no restarts pending), so Remove and
// Shutdown can wait for the process to actually be gone.
type proc struct {
	cancel  context.CancelFunc
	done    chan struct{}
	port    int
	running bool
	lastErr string
}

// Manager installs, supervises and proxies plugins. The zero value is not usable;
// call New.
type Manager struct {
	dir     string       // <homeDir>/plugins
	speaker string       // speaker host:port passed to every plugin
	hostURL string       // ReTouch's own base URL, for plugin callbacks
	ua      string       // User-Agent for release downloads
	client  *http.Client // SafeTransport, for installs (public hosts only)
	loop    *http.Client // short-timeout client for loopback /health probes
	log     *slog.Logger

	// Release source, overridable in tests. apiBase serves the GitHub releases API;
	// dlBase serves the release download tree.
	apiBase string
	dlBase  string

	// installMu serialises installs (release + sideload): two overlapping installs
	// of the same plugin would write the same bin.new, letting the loser keep
	// writing into the installed binary AFTER the winner verified its checksum.
	installMu sync.Mutex

	mu     sync.Mutex
	runCtx context.Context // set once Run starts; parent for per-plugin supervisors
	state  []Installed
	procs  map[string]*proc
}

// New opens (or starts) the plugin store rooted at dir. It reads plugins.json but
// starts nothing until Run is called.
func New(dir, speakerHost, hostURL, userAgent string, log *slog.Logger) (*Manager, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create plugins dir: %w", err)
	}
	m := &Manager{
		dir:     dir,
		speaker: speakerHost,
		hostURL: hostURL,
		ua:      userAgent,
		client:  &http.Client{Timeout: 5 * time.Minute, Transport: release.SafeTransport()},
		loop:    &http.Client{Timeout: 2 * time.Second},
		log:     log,
		procs:   map[string]*proc{},
		apiBase: "https://api.github.com",
		dlBase:  "https://github.com",
	}
	if err := m.load(); err != nil {
		return nil, err
	}
	return m, nil
}

// Run starts a supervisor for every enabled plugin and blocks until ctx is done.
// It matches the app.RegisterService shape, so app can launch it as a goroutine
// alongside the other background services.
func (m *Manager) Run(ctx context.Context) {
	m.mu.Lock()
	m.runCtx = ctx
	names := make([]string, 0, len(m.state))
	for _, p := range m.state {
		if p.Enabled {
			names = append(names, p.Name)
		}
	}
	m.mu.Unlock()

	for _, name := range names {
		m.startSupervisor(ctx, name)
	}
	<-ctx.Done()
}

// startSupervisor launches (or relaunches) the supervisor goroutine for name under
// ctx. Caller must not hold m.mu.
func (m *Manager) startSupervisor(parent context.Context, name string) {
	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	m.mu.Lock()
	if old := m.procs[name]; old != nil && old.cancel != nil {
		old.cancel() // replace any prior supervisor for this name
	}
	m.procs[name] = &proc{cancel: cancel, done: done}
	m.mu.Unlock()
	go func() {
		defer close(done)
		m.supervise(ctx, name)
	}()
}

// Shutdown stops every plugin child and waits (up to timeout) for them to be gone.
// Call it before the process exits outside of normal context cancellation — most
// importantly before the OTA self-update's os.Exit — otherwise the children are
// reparented to init and keep running, and the relaunched ReTouch starts duplicates
// (two Ring agents then invalidate each other's rotating refresh token).
func (m *Manager) Shutdown(timeout time.Duration) {
	m.mu.Lock()
	waits := make([]chan struct{}, 0, len(m.procs))
	for _, p := range m.procs {
		if p.cancel != nil {
			p.cancel()
		}
		if p.done != nil {
			waits = append(waits, p.done)
		}
	}
	m.mu.Unlock()
	deadline := time.After(timeout)
	for _, done := range waits {
		select {
		case <-done:
		case <-deadline:
			m.log.Warn("plugin shutdown timed out; child may survive the restart")
			return
		}
	}
}

// supervise runs one plugin's child process, restarting it with capped backoff
// until ctx is cancelled (by shutdown or by Remove).
func (m *Manager) supervise(ctx context.Context, name string) {
	const minBackoff, maxBackoff = time.Second, 30 * time.Second
	backoff := minBackoff
	for ctx.Err() == nil {
		inst, ok := m.get(name)
		if !ok || !inst.Enabled {
			return
		}
		started := time.Now()
		err := m.runOnce(ctx, inst)
		if ctx.Err() != nil {
			return
		}
		m.setProcErr(name, err)
		if err != nil {
			m.log.Warn("plugin exited", "plugin", name, "err", err)
		}
		// A child that stayed up a good while is a normal restart, not a crash loop;
		// reset the backoff so a one-off exit doesn't slow the next start.
		if time.Since(started) > maxBackoff {
			backoff = minBackoff
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

// runOnce starts the plugin binary once and blocks until it exits.
func (m *Manager) runOnce(ctx context.Context, inst Installed) error {
	dir := m.pluginDir(inst.Name)
	bin := filepath.Join(dir, binName)
	if !fileExists(bin) {
		return fmt.Errorf("binary missing at %s", bin)
	}
	port, err := freeLoopbackPort()
	if err != nil {
		return fmt.Errorf("allocate port: %w", err)
	}
	cmd := exec.CommandContext(ctx, bin,
		"--speaker-host", m.speaker,
		"--config-dir", dir,
		"--listen", fmt.Sprintf("127.0.0.1:%d", port),
		"--host-url", m.hostURL,
	)
	// The child logs to a per-plugin file in its own dir; keep it off ReTouch's
	// stderr so a chatty plugin can't drown the agent's own logs. Rotate (one .old
	// generation) instead of truncating: in a crash loop, truncation would wipe
	// the previous attempt's stderr — exactly the output that explains the loop.
	logPath := filepath.Join(dir, "log")
	if fi, err := os.Stat(logPath); err == nil && fi.Size() > 512<<10 {
		_ = os.Rename(logPath, logPath+".old")
	}
	logf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if logf != nil {
		cmd.Stdout = logf
		cmd.Stderr = logf
		defer func() { _ = logf.Close() }()
	}
	// Hand the child a pipe as stdin and keep the write end open for its lifetime.
	// The kernel closes it when THIS process dies — however it dies (os.Exit after
	// a self-update, a crash, kill -9) — so a well-behaved plugin exits on stdin
	// EOF instead of surviving as an orphan next to the relaunched ReTouch.
	pr, pw, pipeErr := os.Pipe()
	if pipeErr == nil {
		cmd.Stdin = pr
		defer func() { _ = pw.Close() }()
	}
	if err := cmd.Start(); err != nil {
		if pipeErr == nil {
			_ = pr.Close()
		}
		return fmt.Errorf("start: %w", err)
	}
	if pipeErr == nil {
		_ = pr.Close() // child holds its own copy
	}
	m.log.Info("plugin started", "plugin", inst.Name, "version", inst.Version, "port", port)
	// Mark the proc up (and start proxying) only once /health answers: proxying
	// before the child listens just turns every early UI request into a 502.
	probeCtx, probeCancel := context.WithCancel(ctx)
	go m.markUpWhenHealthy(probeCtx, inst.Name, port)
	err = cmd.Wait()
	probeCancel()
	m.setProcDown(inst.Name)
	return err
}

// markUpWhenHealthy polls the child's /health until it answers, then marks the proc
// running. Stops when the child exits (ctx cancelled).
func (m *Manager) markUpWhenHealthy(ctx context.Context, name string, port int) {
	url := fmt.Sprintf("http://127.0.0.1:%d/health", port)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return
		}
		if resp, err := m.loop.Do(req); err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				m.setProcUp(name, port)
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// Proxy returns an http.Handler that reverse-proxies to the plugin's current
// loopback port, stripping the /api/plugins/<name> prefix. ok is false only when
// no plugin by that name is installed. If the plugin isn't running, the handler
// answers 503 rather than proxying to a dead port.
func (m *Manager) Proxy(name string) (http.Handler, bool) {
	if _, ok := m.get(name); !ok {
		return nil, false
	}
	prefix := "/api/plugins/" + name
	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = "http"
			req.URL.Host = fmt.Sprintf("127.0.0.1:%d", m.runningPort(name))
			req.URL.Path = "/" + trimPrefixSlash(req.URL.Path, prefix)
		},
		ErrorHandler: func(w http.ResponseWriter, _ *http.Request, err error) {
			m.log.Warn("plugin proxy error", "plugin", name, "err", err)
			http.Error(w, "plugin unavailable", http.StatusBadGateway)
		},
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.runningPort(name) == 0 {
			http.Error(w, "plugin not running", http.StatusServiceUnavailable)
			return
		}
		rp.ServeHTTP(w, r)
	}), true
}

// List returns the installed plugins with live run status, for the UI.
func (m *Manager) List() []Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Status, 0, len(m.state))
	for _, inst := range m.state {
		st := Status{Installed: inst}
		if p := m.procs[inst.Name]; p != nil {
			st.Running = p.running
			st.LastErr = p.lastErr
		}
		out = append(out, st)
	}
	return out
}

// ---- small helpers -------------------------------------------------------

func (m *Manager) pluginDir(name string) string { return filepath.Join(m.dir, name) }

func (m *Manager) get(name string) (Installed, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.state {
		if p.Name == name {
			return p, true
		}
	}
	return Installed{}, false
}

func (m *Manager) runningPort(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.procs[name]; p != nil && p.running {
		return p.port
	}
	return 0
}

func (m *Manager) setProcUp(name string, port int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.procs[name]; p != nil {
		p.port, p.running, p.lastErr = port, true, ""
	}
}

func (m *Manager) setProcDown(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.procs[name]; p != nil {
		p.running, p.port = false, 0
	}
}

func (m *Manager) setProcErr(name string, err error) {
	if err == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if p := m.procs[name]; p != nil {
		p.lastErr = err.Error()
	}
}

// load reads plugins.json; a missing file is an empty store.
func (m *Manager) load() error {
	b, err := os.ReadFile(filepath.Join(m.dir, stateFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, &m.state)
}

// saveLocked persists the state manifest. Caller holds m.mu.
func (m *Manager) saveLocked() error {
	return atomicjson.Write(filepath.Join(m.dir, stateFile), m.state, 0o644)
}

func freeLoopbackPort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer func() { _ = l.Close() }()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// trimPrefixSlash removes prefix from p and any leading slash, so the result can be
// re-rooted with a single "/".
func trimPrefixSlash(p, prefix string) string {
	p = p[min(len(prefix), len(p)):]
	for len(p) > 0 && p[0] == '/' {
		p = p[1:]
	}
	return p
}
