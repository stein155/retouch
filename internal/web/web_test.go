package web_test

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stein155/retouch/internal/nowplaying"
	"github.com/stein155/retouch/internal/settings"
	"github.com/stein155/retouch/internal/sim"
	"github.com/stein155/retouch/internal/speaker"
	"github.com/stein155/retouch/internal/store"
	"github.com/stein155/retouch/internal/tunein"
	"github.com/stein155/retouch/internal/update"
	"github.com/stein155/retouch/internal/web"
)

// newServer wires a web.Server to a fresh in-memory SoundTouch simulator and
// returns the server's HTTP handler plus the simulator (for assertions on its
// state). Everything (sim REST server) is torn down via t.Cleanup.
//
// Only handlers backed by the simulator / local state are exercised here; the
// TuneIn client is constructed but never driven (no /api/search etc.), so the
// tests make no outbound network calls.
func newServer(t *testing.T) (http.Handler, *sim.Speaker) {
	t.Helper()
	srv, sp, _ := newServerSrv(t)
	return srv.Handler(), sp
}

// newServerSrv is newServer but hands back the *web.Server itself (for tests
// that need to drive its background loop, Run) plus its home/state dir.
func newServerSrv(t *testing.T) (*web.Server, *sim.Speaker, string) {
	t.Helper()
	return newServerAt(t, t.TempDir())
}

// newServerAt is newServerSrv over a caller-owned state dir, so a test can
// build a second server over the same dir to simulate a restart.
func newServerAt(t *testing.T, dir string) (*web.Server, *sim.Speaker, string) {
	t.Helper()
	sp := sim.New()

	ts := httptest.NewServer(sp.Handler())
	t.Cleanup(ts.Close)

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse sim url: %v", err)
	}
	host := net.JoinHostPort(u.Hostname(), u.Port())
	sc := speaker.New(host)

	st, err := store.Open(filepath.Join(dir, "presets.json"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	set := settings.Open(filepath.Join(dir, "settings.json"))
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// homeDir is a temp dir with no "retouch" binary, so updatable() is false:
	// /api/version reports updatable:false and /api/update returns 409 without
	// ever reaching GitHub.
	srv := web.New(tunein.New(), sc, st, set, update.New("test", dir, log), nowplaying.New(sc, tunein.New()), dir, log)
	// Toggling closeTelnet applies a firewall rule immediately; stub it out so
	// tests never run iptables.
	srv.SetTelnetFirewall(func(bool) error { return nil })
	return srv, sp, dir
}

// do runs a request against the handler and returns the recorder.
func do(t *testing.T, h http.Handler, method, target string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	// The API guard requires a Host that names the speaker (blocks DNS rebinding);
	// httptest defaults to "example.com", so present a loopback host here.
	req.Host = "127.0.0.1:8000"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// decodeBody decodes the JSON response body into v.
func decodeBody(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode body %q: %v", rec.Body.String(), err)
	}
}

func TestVolume(t *testing.T) {
	h, _ := newServer(t)

	// Fresh sim seeds volume 25.
	rec := do(t, h, "GET", "/api/volume", "")
	if rec.Code != 200 {
		t.Fatalf("GET volume: %d", rec.Code)
	}
	var got struct {
		Volume int `json:"volume"`
	}
	decodeBody(t, rec, &got)
	if got.Volume != 25 {
		t.Errorf("initial volume = %d, want 25", got.Volume)
	}

	rec = do(t, h, "POST", "/api/volume", `{"volume":42}`)
	if rec.Code != 200 {
		t.Fatalf("POST volume: %d (%s)", rec.Code, rec.Body)
	}
	decodeBody(t, rec, &got)
	if got.Volume != 42 {
		t.Errorf("POST volume echoed %d, want 42", got.Volume)
	}

	rec = do(t, h, "GET", "/api/volume", "")
	decodeBody(t, rec, &got)
	if got.Volume != 42 {
		t.Errorf("readback volume = %d, want 42", got.Volume)
	}

	// Sim clamps to 0..100.
	do(t, h, "POST", "/api/volume", `{"volume":250}`)
	rec = do(t, h, "GET", "/api/volume", "")
	decodeBody(t, rec, &got)
	if got.Volume != 100 {
		t.Errorf("clamp high volume = %d, want 100", got.Volume)
	}
}

func TestPresetsLifecycle(t *testing.T) {
	h, _ := newServer(t)

	// Fresh speaker: no presets.
	rec := do(t, h, "GET", "/api/presets", "")
	if rec.Code != 200 {
		t.Fatalf("GET presets: %d", rec.Code)
	}
	var list []store.Preset
	decodeBody(t, rec, &list)
	if len(list) != 0 {
		t.Fatalf("fresh presets = %+v, want none", list)
	}

	// Store a native preset in slot 3.
	rec = do(t, h, "PUT", "/api/presets/3", `{"stationId":"s99","name":"Radio 538","logo":"http://logo"}`)
	if rec.Code != 200 {
		t.Fatalf("PUT preset: %d (%s)", rec.Code, rec.Body)
	}

	rec = do(t, h, "GET", "/api/presets", "")
	var sps []speaker.Preset
	decodeBody(t, rec, &sps)
	if len(sps) != 1 || sps[0].Slot != 3 || sps[0].Name != "Radio 538" || sps[0].StationID != "s99" || sps[0].Logo != "" {
		t.Fatalf("stored preset = %+v", sps)
	}

	// Delete it.
	rec = do(t, h, "DELETE", "/api/presets/3", "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE preset: %d", rec.Code)
	}
	rec = do(t, h, "GET", "/api/presets", "")
	decodeBody(t, rec, &sps)
	if len(sps) != 0 {
		t.Errorf("after delete presets = %+v, want none", sps)
	}
}

func TestPresetBadSlot(t *testing.T) {
	h, _ := newServer(t)
	for _, slot := range []string{"0", "7", "x"} {
		rec := do(t, h, "PUT", "/api/presets/"+slot, `{"stationId":"s1"}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT slot %q: code %d, want 400", slot, rec.Code)
		}
	}
}

func TestPresetMissingStationID(t *testing.T) {
	h, _ := newServer(t)
	rec := do(t, h, "PUT", "/api/presets/1", `{"name":"no id"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("PUT without stationId: code %d, want 400", rec.Code)
	}
}

func TestPlayAndNow(t *testing.T) {
	h, sp := newServer(t)

	// playStation selects a native TUNEIN ContentItem on the sim. Use a station
	// id that does NOT start with "s" so the now handler skips TuneIn enrichment
	// (no outbound network).
	rec := do(t, h, "POST", "/api/play", `{"stationId":"p100","name":"Local FM"}`)
	if rec.Code != 200 {
		t.Fatalf("POST play: %d (%s)", rec.Code, rec.Body)
	}
	var played struct {
		Status  string `json:"status"`
		Station string `json:"station"`
	}
	decodeBody(t, rec, &played)
	if played.Status != "playing" || played.Station != "p100" {
		t.Errorf("play response = %+v", played)
	}

	rec = do(t, h, "GET", "/api/now", "")
	if rec.Code != 200 {
		t.Fatalf("GET now: %d (%s)", rec.Code, rec.Body)
	}
	var np speaker.NowPlaying
	decodeBody(t, rec, &np)
	if np.Source != "TUNEIN" || np.Station != "Local FM" || np.PlayStatus != "PLAY_STATE" {
		t.Errorf("now playing = %+v", np)
	}
	if np.StationID != "p100" {
		t.Errorf("stationId = %q, want p100", np.StationID)
	}

	// Stop pauses playback on the sim.
	rec = do(t, h, "POST", "/api/stop", "")
	if rec.Code != 200 {
		t.Fatalf("POST stop: %d", rec.Code)
	}
	rec = do(t, h, "GET", "/api/now", "")
	decodeBody(t, rec, &np)
	if np.PlayStatus != "PAUSE_STATE" {
		t.Errorf("after stop playStatus = %q, want PAUSE_STATE", np.PlayStatus)
	}
	_ = sp
}

func TestPlayPreset(t *testing.T) {
	h, _ := newServer(t)
	// playPreset presses PRESET_n; it calls Wake (CLI port, not served here) which
	// is best-effort, then Key over the REST API which the sim accepts.
	rec := do(t, h, "POST", "/api/play/2", "")
	if rec.Code != 200 {
		t.Fatalf("POST play/2: %d (%s)", rec.Code, rec.Body)
	}
	var got struct {
		Playing int `json:"playing"`
	}
	decodeBody(t, rec, &got)
	if got.Playing != 2 {
		t.Errorf("playing = %d, want 2", got.Playing)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	h, sp := newServer(t)

	rec := do(t, h, "GET", "/api/settings", "")
	if rec.Code != 200 {
		t.Fatalf("GET settings: %d", rec.Code)
	}
	var got map[string]any
	decodeBody(t, rec, &got)
	if got["name"] != "Keuken" {
		t.Errorf("name = %v, want Keuken", got["name"])
	}
	if got["model"] != "SoundTouch 10" {
		t.Errorf("model = %v, want SoundTouch 10", got["model"])
	}
	if got["language"] != "en" {
		t.Errorf("language = %v, want en", got["language"])
	}
	if got["closeTelnet"] != false {
		t.Errorf("closeTelnet = %v, want false", got["closeTelnet"])
	}
	// bass comes back as an object (target/actual/min/max).
	if _, ok := got["bass"]; !ok {
		t.Errorf("settings missing bass: %+v", got)
	}

	// Update name, bass and language in one call.
	rec = do(t, h, "PUT", "/api/settings", `{"name":"Kantoor","bass":-4,"language":"nl"}`)
	if rec.Code != 200 {
		t.Fatalf("PUT settings: %d (%s)", rec.Code, rec.Body)
	}

	if sp.Name != "Kantoor" {
		t.Errorf("sim name = %q, want Kantoor", sp.Name)
	}

	rec = do(t, h, "GET", "/api/settings", "")
	decodeBody(t, rec, &got)
	if got["name"] != "Kantoor" {
		t.Errorf("readback name = %v, want Kantoor", got["name"])
	}
	if got["language"] != "nl" {
		t.Errorf("readback language = %v, want nl", got["language"])
	}
	bass, ok := got["bass"].(map[string]any)
	if !ok {
		t.Fatalf("bass not an object: %v", got["bass"])
	}
	if bass["target"].(float64) != -4 || bass["actual"].(float64) != -4 {
		t.Errorf("bass after set = %+v, want target/actual -4", bass)
	}

	rec = do(t, h, "PUT", "/api/settings", `{"closeTelnet":true}`)
	if rec.Code != 200 {
		t.Fatalf("PUT closeTelnet on: %d (%s)", rec.Code, rec.Body)
	}
	rec = do(t, h, "GET", "/api/settings", "")
	decodeBody(t, rec, &got)
	if got["closeTelnet"] != true {
		t.Errorf("closeTelnet after on = %v, want true", got["closeTelnet"])
	}
	rec = do(t, h, "PUT", "/api/settings", `{"closeTelnet":false}`)
	if rec.Code != 200 {
		t.Fatalf("PUT closeTelnet off: %d (%s)", rec.Code, rec.Body)
	}
}

func TestDeviceSettingsRoundTrip(t *testing.T) {
	h, _ := newServer(t)

	rec := do(t, h, "GET", "/api/settings", "")
	if rec.Code != 200 {
		t.Fatalf("GET settings: %d", rec.Code)
	}
	var got map[string]any
	decodeBody(t, rec, &got)

	// The sim exposes tone controls, power-saving and a Wi-Fi connection, so all
	// three device-specific settings must be present.
	tr, ok := got["treble"].(map[string]any)
	if !ok {
		t.Fatalf("settings missing treble: %+v", got)
	}
	if tr["min"].(float64) != -100 || tr["max"].(float64) != 100 || tr["step"].(float64) != 10 {
		t.Errorf("treble caps = %+v", tr)
	}
	// Fresh sim: power-saving off => optimized true.
	if got["wifiOptimization"] != true {
		t.Errorf("wifiOptimization = %v, want true", got["wifiOptimization"])
	}
	net, ok := got["network"].(map[string]any)
	if !ok {
		t.Fatalf("settings missing network: %+v", got)
	}
	if net["ssid"] != "HomeWiFi" || net["signal"] != "good" {
		t.Errorf("network = %+v", net)
	}

	// Set treble and flip the Wi-Fi optimization off, then read them back.
	rec = do(t, h, "PUT", "/api/settings", `{"treble":30,"wifiOptimization":false}`)
	if rec.Code != 200 {
		t.Fatalf("PUT settings: %d (%s)", rec.Code, rec.Body)
	}
	rec = do(t, h, "GET", "/api/settings", "")
	decodeBody(t, rec, &got)
	if tr = got["treble"].(map[string]any); tr["value"].(float64) != 30 {
		t.Errorf("treble after set = %+v, want value 30", tr)
	}
	if got["wifiOptimization"] != false {
		t.Errorf("wifiOptimization after disable = %v, want false", got["wifiOptimization"])
	}
}

func TestSettingsBadBody(t *testing.T) {
	h, _ := newServer(t)
	rec := do(t, h, "PUT", "/api/settings", `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("PUT bad body: %d, want 400", rec.Code)
	}
	var got struct {
		Error string `json:"error"`
	}
	decodeBody(t, rec, &got)
	if got.Error == "" {
		t.Errorf("expected error message, got %q", rec.Body.String())
	}
}

func TestMultiroomUngrouped(t *testing.T) {
	h, _ := newServer(t)
	// Fresh sim has no zone, so multiroom reports the speaker's identity and an
	// empty, non-master zone. This is the read-only path (no network sweep).
	rec := do(t, h, "GET", "/api/multiroom", "")
	if rec.Code != 200 {
		t.Fatalf("GET multiroom: %d (%s)", rec.Code, rec.Body)
	}
	var got struct {
		Self struct {
			DeviceID string `json:"deviceId"`
			Name     string `json:"name"`
			IP       string `json:"ip"`
		} `json:"self"`
		IsMaster bool             `json:"isMaster"`
		Members  []speaker.Member `json:"members"`
		Master   string           `json:"master"`
	}
	decodeBody(t, rec, &got)
	if got.Self.DeviceID != "F4E11E3B013F" || got.Self.Name != "Keuken" {
		t.Errorf("self = %+v", got.Self)
	}
	if got.IsMaster {
		t.Errorf("fresh speaker should not be master")
	}
	if len(got.Members) != 0 {
		t.Errorf("fresh speaker members = %+v, want none", got.Members)
	}
}

func TestVersion(t *testing.T) {
	h, _ := newServer(t)
	rec := do(t, h, "GET", "/api/version", "")
	if rec.Code != 200 {
		t.Fatalf("GET version: %d", rec.Code)
	}
	var got struct {
		Version   string `json:"version"`
		Updatable bool   `json:"updatable"`
	}
	decodeBody(t, rec, &got)
	if got.Version != "test" {
		t.Errorf("version = %q, want test", got.Version)
	}
	// homeDir (a t.TempDir) has no "retouch" binary, so updates are unavailable.
	if got.Updatable {
		t.Errorf("updatable = true, want false (no installed binary)")
	}
}

func TestUpdateUnavailable(t *testing.T) {
	h, _ := newServer(t)
	// Not an installed speaker -> 409 Conflict, without ever contacting GitHub.
	rec := do(t, h, "POST", "/api/update", "")
	if rec.Code != http.StatusConflict {
		t.Fatalf("POST update: %d, want 409", rec.Code)
	}
	var got struct {
		Error string `json:"error"`
	}
	decodeBody(t, rec, &got)
	if got.Error == "" {
		t.Errorf("expected error message, got %q", rec.Body.String())
	}
}

// TestEventsStream drives the SSE endpoint end to end: it runs the server's
// background hub, puts the sim into a known playing state, connects to
// /api/events and verifies a live "state" push carrying the now-playing +
// volume the browser would otherwise have to poll for.
func TestEventsStream(t *testing.T) {
	srv, _, _ := newServerSrv(t)
	h := srv.Handler()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.Run(ctx) // the hub's poll loop; without it nothing is pushed

	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)

	// A known playing state so the push has content to assert on.
	if rec := do(t, h, "POST", "/api/play", `{"stationId":"p100","name":"Local FM"}`); rec.Code != 200 {
		t.Fatalf("play: %d (%s)", rec.Code, rec.Body)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+"/api/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("events status %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q, want text/event-stream", ct)
	}

	var snap struct {
		Now *speaker.NowPlaying `json:"now"`
		Vol *int                `json:"volume"`
	}
	if err := readStateEvent(cancel, resp.Body, 5*time.Second, &snap); err != nil {
		t.Fatalf("read state event: %v", err)
	}
	if snap.Now == nil {
		t.Fatal("state push carried no now-playing")
	}
	if snap.Now.Source != "TUNEIN" || snap.Now.Station != "Local FM" {
		t.Errorf("pushed now-playing = %+v", snap.Now)
	}
	if snap.Vol == nil || *snap.Vol != 25 {
		t.Errorf("pushed volume = %v, want 25", snap.Vol)
	}
}

// readStateEvent reads the SSE stream until the first "state" event and decodes
// its data into out. It cancels the request and fails if none arrives in time,
// so a broken stream can't hang the test.
func readStateEvent(cancel context.CancelFunc, body io.Reader, timeout time.Duration, out any) error {
	type result struct {
		data string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		sc := bufio.NewScanner(body)
		sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
		event := ""
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				if event == "state" {
					ch <- result{data: strings.TrimPrefix(line, "data: ")}
					return
				}
			case line == "":
				event = ""
			}
		}
		ch <- result{err: sc.Err()}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if r.data == "" {
			return fmt.Errorf("stream ended before a state event")
		}
		return json.Unmarshal([]byte(r.data), out)
	case <-time.After(timeout):
		cancel()
		return fmt.Errorf("timed out waiting for state event")
	}
}
