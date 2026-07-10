package sim

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The simulator is the backbone of every web/marge/speaker integration test, so
// its wire format and state transitions are a contract worth pinning directly.
// These tests drive its raw HTTP/CLI surface (no speaker.Client) and assert on
// the bytes it produces.

func serve(t *testing.T) (*Speaker, *httptest.Server) {
	t.Helper()
	sp := New()
	ts := httptest.NewServer(sp.Handler())
	t.Cleanup(ts.Close)
	return sp, ts
}

func get(t *testing.T, ts *httptest.Server, path string) (int, string) {
	t.Helper()
	r, err := http.Get(ts.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer func() { _ = r.Body.Close() }()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(b)
}

func post(t *testing.T, ts *httptest.Server, path, body string) (int, string) {
	t.Helper()
	r, err := http.Post(ts.URL+path, "application/xml", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer func() { _ = r.Body.Close() }()
	b, _ := io.ReadAll(r.Body)
	return r.StatusCode, string(b)
}

func mustContain(t *testing.T, hay, needle string) {
	t.Helper()
	if !strings.Contains(hay, needle) {
		t.Errorf("expected response to contain %q; got:\n%s", needle, hay)
	}
}

func TestInfoWireFormat(t *testing.T) {
	_, ts := serve(t)
	code, body := get(t, ts, "/info")
	if code != 200 {
		t.Fatalf("status %d", code)
	}
	for _, want := range []string{
		`deviceID="F4E11E3B013F"`, `<name>Keuken</name>`, `<type>SoundTouch 10</type>`,
		`<componentCategory>SCM</componentCategory>`, `<softwareVersion>27.0.6.46330.5043500</softwareVersion>`,
		`<componentCategory>PackagedProduct</componentCategory>`, `<ipAddress>192.168.1.42</ipAddress>`,
	} {
		mustContain(t, body, want)
	}
}

func TestNowPlayingStartsInStandby(t *testing.T) {
	_, ts := serve(t)
	_, body := get(t, ts, "/now_playing")
	mustContain(t, body, `source="STANDBY"`)
}

func TestSelectThenNowPlaying(t *testing.T) {
	_, ts := serve(t)
	ci := `<ContentItem source="TUNEIN" type="stationurl" location="/v1/playback/station/s123" isPresetable="true"><itemName>Jazz FM</itemName></ContentItem>`
	if code, _ := post(t, ts, "/select", ci); code != 200 {
		t.Fatalf("select status %d", code)
	}
	code, body := get(t, ts, "/now_playing")
	if code != 200 {
		t.Fatalf("now status %d", code)
	}
	mustContain(t, body, `source="TUNEIN"`)
	mustContain(t, body, `location="/v1/playback/station/s123"`)
	mustContain(t, body, `<stationName>Jazz FM</stationName>`)
	mustContain(t, body, `<playStatus>PLAY_STATE</playStatus>`)
}

func TestKeyTransitions(t *testing.T) {
	_, ts := serve(t)
	post(t, ts, "/select", `<ContentItem source="TUNEIN" location="/x"><itemName>X</itemName></ContentItem>`)
	press := func(key string) { post(t, ts, "/key", fmt.Sprintf(`<key state="press" sender="t">%s</key>`, key)) }

	for _, tc := range []struct{ key, want string }{
		{"PAUSE", "PAUSE_STATE"},
		{"PLAY", "PLAY_STATE"},
		{"PLAY_PAUSE", "PAUSE_STATE"}, // toggles from PLAY
		{"PLAY_PAUSE", "PLAY_STATE"},  // toggles back
		{"STOP", "STOP_STATE"},
	} {
		press(tc.key)
		_, body := get(t, ts, "/now_playing")
		mustContain(t, body, "<playStatus>"+tc.want+"</playStatus>")
	}
}

func TestKeyReleaseIsIgnored(t *testing.T) {
	_, ts := serve(t)
	post(t, ts, "/select", `<ContentItem source="TUNEIN" location="/x"><itemName>X</itemName></ContentItem>`)
	post(t, ts, "/key", `<key state="press" sender="t">PLAY</key>`)
	// A stray release for PAUSE must NOT change state (firmware sends press+release;
	// the sim acts only on press).
	post(t, ts, "/key", `<key state="release" sender="t">PAUSE</key>`)
	_, body := get(t, ts, "/now_playing")
	mustContain(t, body, "<playStatus>PLAY_STATE</playStatus>")
}

func TestPowerKeyTogglesStandby(t *testing.T) {
	_, ts := serve(t)
	post(t, ts, "/select", `<ContentItem source="TUNEIN" location="/x"><itemName>X</itemName></ContentItem>`)
	post(t, ts, "/key", `<key state="press" sender="t">POWER</key>`)
	if _, body := get(t, ts, "/now_playing"); !strings.Contains(body, `source="STANDBY"`) {
		t.Fatalf("POWER did not enter standby: %s", body)
	}
	post(t, ts, "/key", `<key state="press" sender="t">POWER</key>`)
	if _, body := get(t, ts, "/now_playing"); !strings.Contains(body, `source="TUNEIN"`) {
		t.Fatalf("POWER did not restore the source: %s", body)
	}
}

func TestPresetStoreRecallRemove(t *testing.T) {
	_, ts := serve(t)
	store := `<preset id="3"><ContentItem source="TUNEIN" type="stationurl" location="/v1/playback/station/s99" isPresetable="true"><itemName>Radio 538</itemName><containerArt>http://logo</containerArt></ContentItem></preset>`
	if code, _ := post(t, ts, "/storePreset", store); code != 200 {
		t.Fatalf("store status %d", code)
	}
	_, list := get(t, ts, "/presets")
	mustContain(t, list, `<preset id="3">`)
	mustContain(t, list, `<itemName>Radio 538</itemName>`)
	mustContain(t, list, `<containerArt>http://logo</containerArt>`)

	// Pressing the preset key plays the stored station.
	post(t, ts, "/key", `<key state="press" sender="t">PRESET_3</key>`)
	_, np := get(t, ts, "/now_playing")
	mustContain(t, np, `<stationName>Radio 538</stationName>`)
	mustContain(t, np, `<playStatus>PLAY_STATE</playStatus>`)

	if code, _ := post(t, ts, "/removePreset", `<preset id="3"></preset>`); code != 200 {
		t.Fatalf("remove status %d", code)
	}
	if _, list := get(t, ts, "/presets"); strings.Contains(list, `id="3"`) {
		t.Errorf("preset 3 still present after remove: %s", list)
	}
}

func TestPresetSlotOutOfRange(t *testing.T) {
	_, ts := serve(t)
	code, _ := post(t, ts, "/storePreset", `<preset id="7"><ContentItem location="/x"><itemName>X</itemName></ContentItem></preset>`)
	if code != http.StatusBadRequest {
		t.Errorf("store slot 7: status %d, want 400", code)
	}
}

func TestPressingUnsetPresetIsNoop(t *testing.T) {
	_, ts := serve(t)
	post(t, ts, "/key", `<key state="press" sender="t">PRESET_5</key>`) // slot 5 empty
	if _, body := get(t, ts, "/now_playing"); !strings.Contains(body, `source="STANDBY"`) {
		t.Errorf("pressing an empty preset changed state: %s", body)
	}
}

func TestVolumeClamp(t *testing.T) {
	_, ts := serve(t)
	post(t, ts, "/volume", "<volume>250</volume>")
	if _, body := get(t, ts, "/volume"); !strings.Contains(body, "<actualvolume>100</actualvolume>") {
		t.Errorf("volume not clamped high: %s", body)
	}
	post(t, ts, "/volume", "<volume>-5</volume>")
	if _, body := get(t, ts, "/volume"); !strings.Contains(body, "<actualvolume>0</actualvolume>") {
		t.Errorf("volume not clamped low: %s", body)
	}
}

func TestBassClampToCaps(t *testing.T) {
	_, ts := serve(t)
	// caps are -9..0; a positive request clamps to 0, below-min to -9.
	post(t, ts, "/bass", "<bass>5</bass>")
	if _, body := get(t, ts, "/bass"); !strings.Contains(body, "<actualbass>0</actualbass>") {
		t.Errorf("bass not clamped to max 0: %s", body)
	}
	post(t, ts, "/bass", "<bass>-20</bass>")
	if _, body := get(t, ts, "/bass"); !strings.Contains(body, "<actualbass>-9</actualbass>") {
		t.Errorf("bass not clamped to min -9: %s", body)
	}
}

func TestTrebleSetAndReport(t *testing.T) {
	_, ts := serve(t)
	post(t, ts, "/audioproducttonecontrols", `<audioproducttonecontrols><treble value="40"/></audioproducttonecontrols>`)
	_, body := get(t, ts, "/audioproducttonecontrols")
	mustContain(t, body, `<treble value="40"`)
	mustContain(t, body, `step="10"`)
}

func TestZoneLifecycle(t *testing.T) {
	_, ts := serve(t)
	if _, body := get(t, ts, "/getZone"); !strings.Contains(body, "<zone />") {
		t.Fatalf("fresh zone not empty: %s", body)
	}
	setZone := `<zone master="MASTER1" senderIPAddress="192.168.1.42"><member ipaddress="192.168.1.50">SLAVE1</member></zone>`
	post(t, ts, "/setZone", setZone)
	_, body := get(t, ts, "/getZone")
	mustContain(t, body, `master="MASTER1"`)
	mustContain(t, body, `<member ipaddress="192.168.1.50">SLAVE1</member>`)

	// Add then remove a second slave.
	post(t, ts, "/addZoneSlave", `<zone master="MASTER1"><member ipaddress="192.168.1.51">SLAVE2</member></zone>`)
	if _, body := get(t, ts, "/getZone"); !strings.Contains(body, "SLAVE2") {
		t.Errorf("addZoneSlave did not add: %s", body)
	}
	post(t, ts, "/removeZoneSlave", `<zone master="MASTER1"><member ipaddress="192.168.1.51">SLAVE2</member></zone>`)
	if _, body := get(t, ts, "/getZone"); strings.Contains(body, "SLAVE2") {
		t.Errorf("removeZoneSlave did not remove: %s", body)
	}
}

func TestStateEndpointsRejectGet(t *testing.T) {
	_, ts := serve(t)
	for _, p := range []string{"/select", "/key", "/storePreset", "/setZone", "/name"} {
		if code, _ := get(t, ts, p); code != http.StatusMethodNotAllowed {
			t.Errorf("GET %s: status %d, want 405", p, code)
		}
	}
}

func TestNameAndMargeAccountUpdate(t *testing.T) {
	sp, ts := serve(t)
	post(t, ts, "/name", "<name>Living Room</name>")
	if _, body := get(t, ts, "/info"); !strings.Contains(body, "<name>Living Room</name>") {
		t.Errorf("name not updated: %s", body)
	}
	post(t, ts, "/setMargeAccount", `<PairDeviceWithAccount><accountId>9999999</accountId></PairDeviceWithAccount>`)
	if sp.Account != "9999999" {
		t.Errorf("account = %q, want 9999999", sp.Account)
	}
}

func TestCLISysPowerWakes(t *testing.T) {
	sp := New()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	go sp.ServeCLI(ln)

	// Put it into a known active source first, then standby via CLI, then wake.
	sp.applyKey("POWER") // STANDBY -> TUNEIN (lastSource defaults)
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()

	if _, err := fmt.Fprintln(conn, "sys power"); err != nil {
		t.Fatal(err)
	}
	// Give the CLI goroutine a moment to process the line.
	waitFor(t, func() bool {
		sp.mu.Lock()
		defer sp.mu.Unlock()
		return sp.source == "STANDBY"
	}, "CLI `sys power` did not toggle to standby")
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}
