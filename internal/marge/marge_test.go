package marge

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stein155/retouch/internal/speaker"
)

// fakeTuneIn is an in-process stand-in for the live TuneIn directory so the BMX
// playback tests never touch the network.
type fakeTuneIn struct {
	urls    []string
	err     error
	name    string
	logo    string
	gotID   string
	resolve func(ctx context.Context, id string) ([]string, error)
}

func (f *fakeTuneIn) Resolve(ctx context.Context, id string) ([]string, error) {
	f.gotID = id
	if f.resolve != nil {
		return f.resolve(ctx, id)
	}
	return f.urls, f.err
}

func (f *fakeTuneIn) Describe(_ context.Context, _ string) (string, string) {
	return f.name, f.logo
}

func newTestServer(t *testing.T, info *speaker.Info, tc TuneIn, seeds ...PresetSeed) *Server {
	t.Helper()
	s, err := New("http://stub.local:9080/", info, t.TempDir()+"/presets.json", seeds, tc, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// do issues a request through the full Handler() mux and returns the recorder.
func do(t *testing.T, s *Server, method, path string, body string, hdr map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
	}
	for k, v := range hdr {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	s.Handler().ServeHTTP(w, r)
	return w
}

func TestSpeakerAuthReturnsEmptyOK(t *testing.T) {
	s := newTestServer(t, nil, nil)
	w := do(t, s, http.MethodGet, "/v1/auth", "", map[string]string{"Apikeyheader": "test-key"})
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("body = %q, want empty", w.Body.String())
	}
}

// TestRegistryRewritesBase pins that the embedded BMX registry's base-URL token
// is rewritten to the server's base, so the speaker's TUNEIN worker calls back to us.
func TestRegistryRewritesBase(t *testing.T) {
	s := newTestServer(t, nil, nil)
	w := do(t, s, http.MethodGet, "/bmx/registry/v1/services", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	if strings.Contains(body, tokBase) {
		t.Errorf("registry still contains unsubstituted token %q", tokBase)
	}
	if !strings.Contains(body, "http://stub.local:9080/bmx/tunein") {
		t.Errorf("registry TUNEIN baseUrl not rewritten to stub base; body:\n%s", body)
	}
	if ct := w.Header().Get("Content-Type"); ct != ctJSON {
		t.Errorf("Content-Type = %q, want %q", ct, ctJSON)
	}
	// The TUNEIN service must remain present (value 25) so the worker stays enabled.
	var reg struct {
		Services []struct {
			ID struct {
				Name  string `json:"name"`
				Value int    `json:"value"`
			} `json:"id"`
		} `json:"bmx_services"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &reg); err != nil {
		t.Fatalf("registry not valid JSON: %v", err)
	}
	var foundTuneIn bool
	for _, svc := range reg.Services {
		if svc.ID.Name == "TUNEIN" && svc.ID.Value == 25 {
			foundTuneIn = true
		}
	}
	if !foundTuneIn {
		t.Error("TUNEIN service (value 25) missing from registry")
	}
}

// TestAvailabilityRouting checks the servicesAvailability path is routed to the
// availability doc, not the registry (both live under /bmx/registry/).
func TestAvailabilityRouting(t *testing.T) {
	s := newTestServer(t, nil, nil)
	w := do(t, s, http.MethodGet, "/bmx/registry/v1/servicesAvailability", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Body.Len() != len(s.availability.body) {
		t.Errorf("availability body len = %d, want %d", w.Body.Len(), len(s.availability.body))
	}
}

// TestAccountFullReflectsIdentity asserts the personalised /full account document
// carries the injected speaker identity (account, device, name, IP, serials) and
// that the injected firmware version replaces the captured one.
func TestAccountFullReflectsIdentity(t *testing.T) {
	info := &speaker.Info{
		Account:   "ACC-123",
		DeviceID:  "F4E11E3B013F",
		Name:      "Keuken & Co",
		IP:        "192.168.2.27",
		SerialSCM: "SCM-SERIAL-9",
		SerialPkg: "PKG-SERIAL-7",
		Software:  "99.9.9.test",
	}
	s := newTestServer(t, info, nil)
	w := do(t, s, http.MethodGet, "/streaming/account/ACC-123/full", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`id="ACC-123"`,
		`deviceid="F4E11E3B013F"`,
		"SCM-SERIAL-9",
		"PKG-SERIAL-7",
		"192.168.2.27",
		"Keuken &amp; Co", // XML-escaped name
		"99.9.9.test",     // rewritten firmware version
	} {
		if !strings.Contains(body, want) {
			t.Errorf("account /full missing %q", want)
		}
	}
	if strings.Contains(body, "27.0.6.46330") {
		t.Error("captured firmware version not replaced by injected Software")
	}
	for _, tok := range []string{tokAccount, tokDevice, tokName, tokIP, tokSerialSCM, tokSerialPkg} {
		if strings.Contains(body, tok) {
			t.Errorf("unsubstituted token %q left in /full", tok)
		}
	}
	if ct := w.Header().Get("Content-Type"); ct != ctStreamV12 {
		t.Errorf("Content-Type = %q, want %q", ct, ctStreamV12)
	}
}

// TestAccountFullETag304 covers the If-None-Match conditional on /full.
func TestAccountFullETag304(t *testing.T) {
	s := newTestServer(t, nil, nil)
	w := do(t, s, http.MethodGet, "/streaming/account/x/full", "", nil)
	// marge writes the ETag via the raw header map (uncanonicalised "ETag") on
	// purpose; read it the same way rather than via the canonicalising Get.
	vals := w.Header()["ETag"]
	if len(vals) == 0 || vals[0] == "" {
		t.Fatal("no ETag on /full")
	}
	etag := vals[0]
	w2 := do(t, s, http.MethodGet, "/streaming/account/x/full", "", map[string]string{"If-None-Match": etag})
	if w2.Code != http.StatusNotModified {
		t.Errorf("conditional /full status = %d, want 304", w2.Code)
	}
}

// TestPresetsAllReflectsSeed asserts the served presets/all reflects the embedded
// capture's six native NL stations.
func TestPresetsAllReflectsSeed(t *testing.T) {
	s := newTestServer(t, nil, nil)
	w := do(t, s, http.MethodGet, "/streaming/account/x/presets/all", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	body := w.Body.String()
	for _, name := range []string{"NPO Radio 1", "NPO Radio 2", "Radio 538", "Qmusic", "Sterren.nl", "Radio Starfighter"} {
		if !strings.Contains(body, "<name>"+name+"</name>") {
			t.Errorf("presets/all missing seeded station %q", name)
		}
	}
	if n := strings.Count(body, "<preset buttonNumber="); n != 6 {
		t.Errorf("presets/all has %d preset blocks, want 6", n)
	}
	if ct := w.Header().Get("Content-Type"); ct != ctStreamV11 {
		t.Errorf("Content-Type = %q, want %q", ct, ctStreamV11)
	}
}

// TestPresetSeedOverridesSlot checks PresetSeed entries override the embedded
// capture for the given slot and are reflected in the served presets.
func TestPresetSeedOverridesSlot(t *testing.T) {
	s := newTestServer(t, nil, nil, PresetSeed{
		Slot:     1,
		Name:     "My Custom Station",
		Location: "/v1/playback/station/s12345",
		Logo:     "http://art/logo.png",
	})
	w := do(t, s, http.MethodGet, "/streaming/account/x/presets/all", "", nil)
	body := w.Body.String()
	if !strings.Contains(body, "<name>My Custom Station</name>") {
		t.Errorf("seeded preset name not reflected; body:\n%s", body)
	}
	if strings.Contains(body, "<name>NPO Radio 1</name>") {
		t.Error("slot 1 seed did not override captured NPO Radio 1")
	}
	if !strings.Contains(body, "/v1/playback/station/s12345") {
		t.Error("seeded preset location not reflected")
	}
}

// TestPresetSeedInvalidIgnored verifies out-of-range slots and empty locations
// are ignored by seedNative, leaving the captured defaults intact.
func TestPresetSeedInvalidIgnored(t *testing.T) {
	s := newTestServer(t, nil, nil,
		PresetSeed{Slot: 0, Name: "bad-low", Location: "/x"},
		PresetSeed{Slot: 7, Name: "bad-high", Location: "/x"},
		PresetSeed{Slot: 2, Name: "no-location", Location: ""},
	)
	body := do(t, s, http.MethodGet, "/streaming/account/x/presets/all", "", nil).Body.String()
	for _, bad := range []string{"bad-low", "bad-high", "no-location"} {
		if strings.Contains(body, bad) {
			t.Errorf("invalid seed %q should have been ignored", bad)
		}
	}
	// Slot 2 capture (NPO Radio 2) must survive the rejected empty-location seed.
	if !strings.Contains(body, "<name>NPO Radio 2</name>") {
		t.Error("captured slot 2 lost after rejected seed")
	}
}

// TestUpdatePresetStoreThrough exercises a firmware long-press preset store and
// confirms the new station appears in the subsequent presets/all render.
func TestUpdatePresetStoreThrough(t *testing.T) {
	s := newTestServer(t, nil, nil)
	store := `<?xml version="1.0"?><preset><name>Jazz FM</name>` +
		`<location>/v1/playback/station/s999</location>` +
		`<contentItemType>stationurl</contentItemType></preset>`
	w := do(t, s, http.MethodPost, "/streaming/account/x/device/F4E11E3B013F/presets/3", store, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("updatePreset status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<name>Jazz FM</name>") {
		t.Error("updatePreset response does not contain stored station")
	}
	all := do(t, s, http.MethodGet, "/streaming/account/x/presets/all", "", nil).Body.String()
	if !strings.Contains(all, "<name>Jazz FM</name>") {
		t.Error("stored preset not reflected in presets/all")
	}
	if !strings.Contains(all, "/v1/playback/station/s999") {
		t.Error("stored preset location not reflected in presets/all")
	}
}

// TestUpdatePresetUsernameFallback checks that when <name> is absent the firmware's
// <username> is used as the preset name.
func TestUpdatePresetUsernameFallback(t *testing.T) {
	s := newTestServer(t, nil, nil)
	store := `<preset><username>FromUsername</username>` +
		`<location>/v1/playback/station/s1</location></preset>`
	do(t, s, http.MethodPut, "/streaming/account/x/device/D/preset/4", store, nil)
	all := do(t, s, http.MethodGet, "/streaming/account/x/presets/all", "", nil).Body.String()
	if !strings.Contains(all, "<name>FromUsername</name>") {
		t.Errorf("username fallback not applied; body:\n%s", all)
	}
}

// TestUpdatePresetBadButton verifies a non-store path / bad body is rejected 400.
func TestUpdatePresetBadButton(t *testing.T) {
	s := newTestServer(t, nil, nil)
	// Valid path shape but a body that fails to decode.
	w := do(t, s, http.MethodPost, "/streaming/account/x/device/D/presets/2", "not-xml", nil)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad-body status = %d, want 400", w.Code)
	}
}

func TestIsPresetStore(t *testing.T) {
	cases := []struct {
		path string
		want bool
	}{
		{"/streaming/account/a/device/d/presets/3", true},
		{"/streaming/account/a/device/d/preset/1", true},
		{"/streaming/account/a/presets/all", false}, // no /device segment
		{"/streaming/account/a/device/d/full", false},
		{"/streaming/account/a/device/d/presets", false}, // no trailing /<n>
	}
	for _, c := range cases {
		if got := isPresetStore(c.path); got != c.want {
			t.Errorf("isPresetStore(%q) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestPresetButton(t *testing.T) {
	cases := map[string]int{
		"/x/device/d/presets/1":  1,
		"/x/device/d/presets/6":  6,
		"/x/device/d/presets/0":  0, // below range
		"/x/device/d/presets/7":  0, // above range
		"/x/device/d/presets/ab": 0, // non-numeric
		"/x/device/d/presets/":   0, // empty tail
	}
	for p, want := range cases {
		if got := presetButton(p); got != want {
			t.Errorf("presetButton(%q) = %d, want %d", p, got, want)
		}
	}
}

// TestMirrorAndRemovePreset covers the web-UI mirroring entrypoints.
func TestMirrorAndRemovePreset(t *testing.T) {
	s := newTestServer(t, nil, nil)
	s.MirrorPreset(5, "Mirrored", "/v1/playback/station/s55", "http://art")
	all := do(t, s, http.MethodGet, "/streaming/account/x/presets/all", "", nil).Body.String()
	if !strings.Contains(all, "<name>Mirrored</name>") {
		t.Error("MirrorPreset not reflected")
	}
	s.RemovePreset(5)
	all = do(t, s, http.MethodGet, "/streaming/account/x/presets/all", "", nil).Body.String()
	if strings.Contains(all, "<name>Mirrored</name>") {
		t.Error("RemovePreset did not drop the slot")
	}
}

// TestAddDeviceAssociates pins the AddDevice response that moves the speaker to
// the associated (logged-in) state.
func TestAddDeviceAssociates(t *testing.T) {
	s := newTestServer(t, nil, nil)
	w := do(t, s, http.MethodPost, "/streaming/account/x/device", "", nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("addDevice status = %d, want 201", w.Code)
	}
	body := w.Body.String()
	if !strings.Contains(body, "<margetoken>"+s.token+"</margetoken>") {
		t.Errorf("addDevice missing margetoken; body: %s", body)
	}
	if !strings.Contains(body, `status="OK"`) {
		t.Errorf("addDevice missing OK status; body: %s", body)
	}
}

// TestCatchallAck ensures unknown paths get a generic ack rather than a 404, so
// the speaker never enters a cloud-down retry loop.
func TestCatchallAck(t *testing.T) {
	s := newTestServer(t, nil, nil)
	w := do(t, s, http.MethodGet, "/some/unknown/path", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("catchall status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<ack/>") {
		t.Errorf("catchall body = %q, want <ack/>", w.Body.String())
	}
}

func TestHealthz(t *testing.T) {
	s := newTestServer(t, nil, nil)
	w := do(t, s, http.MethodGet, "/healthz", "", nil)
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Errorf("healthz = %d %q, want 200 \"ok\"", w.Code, w.Body.String())
	}
}

// TestDebugRequestsLog confirms the recent-request ring buffer captures served paths.
func TestDebugRequestsLog(t *testing.T) {
	s := newTestServer(t, nil, nil)
	do(t, s, http.MethodGet, "/streaming/account/x/sources", "", nil)
	w := do(t, s, http.MethodGet, "/debug/requests", "", nil)
	if !strings.Contains(w.Body.String(), "/streaming/account/x/sources") {
		t.Errorf("debug/requests missing logged path; got:\n%s", w.Body.String())
	}
}

// TestBmxTuneinPlayback drives the TUNEIN playback resolve through the fake
// directory and asserts the BMX playback document shape and that the bare station
// id (stripped of trailing path/query) is what gets resolved.
func TestBmxTuneinPlayback(t *testing.T) {
	tc := &fakeTuneIn{
		urls: []string{"http://stream1.example/aac", "http://stream2.example/mp3"},
		name: "Station One",
		logo: "http://logo.example/one.png",
	}
	s := newTestServer(t, nil, tc)
	w := do(t, s, http.MethodGet, "/bmx/tunein/v1/playback/station/s6712/?foo=bar", "", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("playback status = %d, want 200; body: %s", w.Code, w.Body.String())
	}
	if tc.gotID != "s6712" {
		t.Errorf("resolved station id = %q, want %q (should strip trailing path/query)", tc.gotID, "s6712")
	}
	var pb bmxPlayback
	if err := json.Unmarshal(w.Body.Bytes(), &pb); err != nil {
		t.Fatalf("playback not valid JSON: %v", err)
	}
	if pb.Name != "Station One" || pb.ImageUrl != "http://logo.example/one.png" {
		t.Errorf("playback metadata = %+v, want name/logo from Describe", pb)
	}
	if pb.StreamType != "liveRadio" {
		t.Errorf("StreamType = %q, want liveRadio", pb.StreamType)
	}
	if pb.Audio.StreamUrl != "http://stream1.example/aac" {
		t.Errorf("primary StreamUrl = %q, want first resolved url", pb.Audio.StreamUrl)
	}
	if len(pb.Audio.Streams) != 2 {
		t.Errorf("got %d streams, want 2", len(pb.Audio.Streams))
	}
}

// fakeNowPlaying is a stand-in live-track source.
type fakeNowPlaying struct{ track string }

func (f *fakeNowPlaying) Track(string) (string, bool) {
	return f.track, f.track != ""
}

// The display-track injection is gated on the firmware re-fetching the station's
// playback doc: the first fetch keeps the station name (so it can't freeze on a
// select-time track), a repeat swaps in the live track.
func TestBmxTuneinPlaybackNowPlayingGate(t *testing.T) {
	tc := &fakeTuneIn{urls: []string{"http://s/aac"}, name: "Station One", logo: "http://l/one.png"}
	s := newTestServer(t, nil, tc)
	s.SetNowPlaying(&fakeNowPlaying{track: "Artist - Song"})

	nameOf := func() string {
		w := do(t, s, http.MethodGet, "/bmx/tunein/v1/playback/station/s6712", "", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
		}
		var pb bmxPlayback
		if err := json.Unmarshal(w.Body.Bytes(), &pb); err != nil {
			t.Fatalf("bad JSON: %v", err)
		}
		return pb.Name
	}

	if got := nameOf(); got != "Station One" {
		t.Errorf("first fetch Name = %q, want station name (no injection yet)", got)
	}
	if got := nameOf(); got != "Artist - Song" {
		t.Errorf("re-fetch Name = %q, want live track", got)
	}

	// Switching to a different station resets the gate: its first fetch keeps the
	// station name again.
	if w := do(t, s, http.MethodGet, "/bmx/tunein/v1/playback/station/s999", "", nil); w.Code == http.StatusOK {
		var pb bmxPlayback
		_ = json.Unmarshal(w.Body.Bytes(), &pb)
		if pb.Name != "Station One" {
			t.Errorf("new station first fetch Name = %q, want station name", pb.Name)
		}
	}
}

// TestBmxTuneinResolveFailure checks a resolver error yields 502 (so the worker
// retries rather than caches a broken stream).
func TestBmxTuneinResolveFailure(t *testing.T) {
	s := newTestServer(t, nil, &fakeTuneIn{urls: nil}) // empty result == failure
	w := do(t, s, http.MethodGet, "/bmx/tunein/v1/playback/station/s1", "", nil)
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}
}

// TestBmxTuneinNoResolver checks playback with no resolver returns 503.
func TestBmxTuneinNoResolver(t *testing.T) {
	s := newTestServer(t, nil, nil)
	w := do(t, s, http.MethodGet, "/bmx/tunein/v1/playback/station/s1", "", nil)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503", w.Code)
	}
}

// TestBmxTuneinNoopEndpoints covers the init/no-op TUNEIN endpoints the worker
// expects to simply succeed.
func TestBmxTuneinNoopEndpoints(t *testing.T) {
	s := newTestServer(t, nil, nil)
	cases := []struct {
		path string
		want string
	}{
		{"/bmx/tunein/v1/token", `"access_token"`},
		{"/bmx/tunein", `"self"`},
		{"/bmx/tunein/", `"self"`},
		{"/bmx/tunein/v1/navigate", `{}`},
	}
	for _, c := range cases {
		w := do(t, s, http.MethodGet, c.path, "", nil)
		if w.Code != http.StatusOK {
			t.Errorf("%s status = %d, want 200", c.path, w.Code)
		}
		if !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s body = %q, want to contain %q", c.path, w.Body.String(), c.want)
		}
		if ct := w.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("%s Content-Type = %q, want application/json", c.path, ct)
		}
	}
}

func TestXMLText(t *testing.T) {
	cases := map[string]string{
		"plain":   "plain",
		`a&b`:     "a&amp;b",
		`<x>`:     "&lt;x&gt;",
		`"q"`:     "&#34;q&#34;",
		"a\tb\nc": "a&#x9;b&#xA;c",
	}
	for in, want := range cases {
		if got := xmlText(in); got != want {
			t.Errorf("xmlText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSplicePresets(t *testing.T) {
	body := []byte(`<a><presets>OLD</presets><b/>`)
	got := string(splicePresets(body, "NEW"))
	if got != `<a><presets>NEW</presets><b/>` {
		t.Errorf("splicePresets = %q", got)
	}
	// No <presets> block: returned unchanged.
	unchanged := []byte(`<a><b/></a>`)
	if string(splicePresets(unchanged, "X")) != string(unchanged) {
		t.Error("splicePresets should pass through bodies without a presets block")
	}
}

func TestRewriteElementText(t *testing.T) {
	in := `<v>old</v> mid <v>old2</v> <v></v>`
	got := rewriteElementText(in, "v", "new")
	// Non-empty values replaced; empty value left untouched.
	want := `<v>new</v> mid <v>new</v> <v></v>`
	if got != want {
		t.Errorf("rewriteElementText = %q, want %q", got, want)
	}
	// Escaping of the replacement text.
	if got := rewriteElementText(`<v>x</v>`, "v", "a&b"); got != `<v>a&amp;b</v>` {
		t.Errorf("rewriteElementText escaping = %q", got)
	}
}
