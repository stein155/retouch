package speaker

import (
	"context"
	"net"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stein155/retouch/internal/sim"
)

// newSim starts the SoundTouch simulator (REST API + :17000 CLI) and returns it with
// a Client wired to it. Everything is torn down via t.Cleanup.
func newSim(t *testing.T) (*sim.Speaker, *Client) {
	t.Helper()
	sp := sim.New()

	ts := httptest.NewServer(sp.Handler())
	t.Cleanup(ts.Close)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen CLI: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go sp.ServeCLI(ln)

	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatalf("parse server url: %v", err)
	}
	_, cliPort, _ := net.SplitHostPort(ln.Addr().String())

	c := New(u.Hostname())
	c.apiPort = u.Port()
	c.cliPort = cliPort
	return sp, c
}

func ctx() context.Context { return context.Background() }

func TestInfoRoundTrip(t *testing.T) {
	_, c := newSim(t)
	got, err := c.Info(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if got.DeviceID != "F4E11E3B013F" || got.Name != "Keuken" || got.Type != "SoundTouch 10" {
		t.Errorf("identity wrong: %+v", got)
	}
	if got.Software == "" || got.IP != "192.168.1.42" || got.SerialSCM == "" || got.SerialPkg == "" {
		t.Errorf("components/network not parsed: %+v", got)
	}
}

func TestVolumeRoundTrip(t *testing.T) {
	_, c := newSim(t)
	if err := c.SetVolume(ctx(), 40); err != nil {
		t.Fatal(err)
	}
	if v, err := c.Volume(ctx()); err != nil || v != 40 {
		t.Fatalf("Volume = %d, err %v; want 40", v, err)
	}
	// clamp high and low
	_ = c.SetVolume(ctx(), 250)
	if v, _ := c.Volume(ctx()); v != 100 {
		t.Errorf("clamp high: got %d want 100", v)
	}
	_ = c.SetVolume(ctx(), -10)
	if v, _ := c.Volume(ctx()); v != 0 {
		t.Errorf("clamp low: got %d want 0", v)
	}
}

func TestBassRoundTrip(t *testing.T) {
	_, c := newSim(t)
	b, err := c.Bass(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if b.Min != -9 || b.Max != 0 {
		t.Errorf("bass caps: %+v", b)
	}
	if err := c.SetBass(ctx(), -5); err != nil {
		t.Fatal(err)
	}
	if b2, _ := c.Bass(ctx()); b2.Target != -5 || b2.Actual != -5 {
		t.Errorf("after SetBass: %+v", b2)
	}
}

func TestNowPlayingStandbyThenSelect(t *testing.T) {
	_, c := newSim(t)
	np, err := c.NowPlaying(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if np.Source != "STANDBY" {
		t.Errorf("fresh speaker source = %q, want STANDBY", np.Source)
	}

	const loc = "/v1/playback/station/s47309"
	if err := c.Select(ctx(), "TUNEIN", "stationurl", loc, "NPO Radio 2", ""); err != nil {
		t.Fatal(err)
	}
	np, err = c.NowPlaying(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if np.Source != "TUNEIN" || np.Station != "NPO Radio 2" || np.PlayStatus != "PLAY_STATE" {
		t.Errorf("after select: %+v", np)
	}
	if np.StationID != "s47309" {
		t.Errorf("StationID = %q, want s47309", np.StationID)
	}
}

func TestKeyPauseResumes(t *testing.T) {
	_, c := newSim(t)
	_ = c.Select(ctx(), "TUNEIN", "stationurl", "/v1/playback/station/s1", "X", "")
	if err := c.Key(ctx(), "PAUSE"); err != nil {
		t.Fatal(err)
	}
	if np, _ := c.NowPlaying(ctx()); np.PlayStatus != "PAUSE_STATE" {
		t.Errorf("after PAUSE: playStatus = %q", np.PlayStatus)
	}
	_ = c.Key(ctx(), "PLAY")
	if np, _ := c.NowPlaying(ctx()); np.PlayStatus != "PLAY_STATE" {
		t.Errorf("after PLAY: playStatus = %q", np.PlayStatus)
	}
}

func TestPresetsRoundTrip(t *testing.T) {
	_, c := newSim(t)
	if ps, _ := c.Presets(ctx()); len(ps) != 0 {
		t.Fatalf("fresh speaker has presets: %+v", ps)
	}
	if err := c.StorePreset(ctx(), 2, "TUNEIN", "stationurl", "/v1/playback/station/s99", "Radio 538", "http://logo"); err != nil {
		t.Fatal(err)
	}
	ps, err := c.Presets(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if len(ps) != 1 || ps[0].Slot != 2 || ps[0].Name != "Radio 538" || ps[0].StationID != "s99" || ps[0].Logo != "http://logo" {
		t.Fatalf("stored preset wrong: %+v", ps)
	}
	if err := c.RemovePreset(ctx(), 2); err != nil {
		t.Fatal(err)
	}
	if ps, _ := c.Presets(ctx()); len(ps) != 0 {
		t.Errorf("preset not removed: %+v", ps)
	}
}

func TestSetNameRoundTrip(t *testing.T) {
	_, c := newSim(t)
	if err := c.SetName(ctx(), "Kantoor & Co"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Info(ctx()); got.Name != "Kantoor & Co" {
		t.Errorf("name = %q, want %q (XML escaping round-trip)", got.Name, "Kantoor & Co")
	}
}

func TestSetMargeAccount(t *testing.T) {
	_, c := newSim(t)
	if err := c.SetMargeAccount(ctx(), "7654321", "tok"); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.Info(ctx()); got.Account != "7654321" {
		t.Errorf("account = %q, want 7654321", got.Account)
	}
}

func TestZoneLifecycle(t *testing.T) {
	_, c := newSim(t)
	m := Member{DeviceID: "F4E11E3B013F", IP: "192.168.1.10"}
	a := Member{DeviceID: "A0F6FD123456", IP: "192.168.1.11"}
	b := Member{DeviceID: "B1E7AE789ABC", IP: "192.168.1.12"}

	if err := c.SetZone(ctx(), m, []Member{a}); err != nil {
		t.Fatal(err)
	}
	z, err := c.GetZone(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if z.Master != m.DeviceID || len(z.Members) != 2 {
		t.Fatalf("after SetZone: %+v", z)
	}

	if err := c.AddZoneSlave(ctx(), m, []Member{b}); err != nil {
		t.Fatal(err)
	}
	if z, _ := c.GetZone(ctx()); len(z.Members) != 3 {
		t.Errorf("after AddZoneSlave: %+v", z)
	}

	// Removing both slaves leaves only the master -> zone dissolves.
	if err := c.RemoveZoneSlave(ctx(), m, []Member{a, b}); err != nil {
		t.Fatal(err)
	}
	z, _ = c.GetZone(ctx())
	if z.Master != "" || len(z.Members) != 0 {
		t.Errorf("zone should be dissolved: %+v", z)
	}
}

func TestWakeFromStandby(t *testing.T) {
	_, c := newSim(t)
	if np, _ := c.NowPlaying(ctx()); np.Source != "STANDBY" {
		t.Fatalf("precondition: not in standby: %+v", np)
	}
	c.Wake(ctx())
	if np, _ := c.NowPlaying(ctx()); np.Source == "STANDBY" {
		t.Errorf("Wake did not leave standby: %+v", np)
	}
}

func TestTrebleRoundTrip(t *testing.T) {
	_, c := newSim(t)
	tr, err := c.Treble(ctx())
	if err != nil {
		t.Fatalf("Treble: %v", err)
	}
	if tr.Min != -100 || tr.Max != 100 || tr.Step != 10 || tr.Value != 0 {
		t.Errorf("treble caps = %+v, want value 0 range -100..100 step 10", tr)
	}
	if err := c.SetTreble(ctx(), 30); err != nil {
		t.Fatal(err)
	}
	if tr, _ := c.Treble(ctx()); tr.Value != 30 {
		t.Errorf("treble after set = %d, want 30", tr.Value)
	}
}

func TestWifiOptimizedRoundTrip(t *testing.T) {
	_, c := newSim(t)
	// Fresh sim has power-saving off, so Wi-Fi is "optimized" (stays awake).
	if opt, err := c.WifiOptimized(ctx()); err != nil || !opt {
		t.Fatalf("initial WifiOptimized = %v, err %v; want true", opt, err)
	}
	if err := c.SetWifiOptimized(ctx(), false); err != nil {
		t.Fatal(err)
	}
	if opt, _ := c.WifiOptimized(ctx()); opt {
		t.Errorf("after disabling optimization, WifiOptimized = true; want false")
	}
}

func TestNetworkInfoRoundTrip(t *testing.T) {
	_, c := newSim(t)
	n, err := c.NetworkInfo(ctx())
	if err != nil {
		t.Fatalf("NetworkInfo: %v", err)
	}
	if n.Type != "wifi" || n.SSID != "HomeWiFi" || n.Signal != "good" || n.IP != "192.168.1.42" {
		t.Errorf("network = %+v", n)
	}
}

func TestPresetPlayback(t *testing.T) {
	_, c := newSim(t)
	if err := c.StorePreset(ctx(), 2, "TUNEIN", "stationurl", "/v1/playback/station/s1", "Jazz FM", ""); err != nil {
		t.Fatal(err)
	}
	// Playing the preset wakes the speaker and presses the matching key, like the
	// hardware button.
	if err := c.PlayPreset(ctx(), 2); err != nil {
		t.Fatal(err)
	}
	np, err := c.NowPlaying(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if np.Source != "TUNEIN" || np.Station != "Jazz FM" || np.PlayStatus != "PLAY_STATE" {
		t.Errorf("now playing after preset = %+v, want TUNEIN/Jazz FM/PLAY_STATE", np)
	}
}
