package speaker

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// stubClient returns a Client whose API calls hit a test server that answers each
// path from routes (missing paths => 404, which mimics an unsupported endpoint).
func stubClient(t *testing.T, routes map[string]string) *Client {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(ts.Close)
	u, _ := url.Parse(ts.URL)
	c := New(u.Hostname())
	c.apiPort = u.Port()
	return c
}

func TestTrebleParsesRange(t *testing.T) {
	c := stubClient(t, map[string]string{
		"/audioproducttonecontrols": `<audioproducttonecontrols>` +
			`<bass value="0" minValue="-100" maxValue="100" step="10"/>` +
			`<treble value="30" minValue="-100" maxValue="100" step="10"/>` +
			`</audioproducttonecontrols>`,
	})
	tr, err := c.Treble(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if tr.Value != 30 || tr.Min != -100 || tr.Max != 100 || tr.Step != 10 {
		t.Errorf("treble parsed wrong: %+v", tr)
	}
}

// A speaker without tone controls 404s the endpoint; Treble must error so the UI
// hides the control.
func TestTrebleUnsupported(t *testing.T) {
	c := stubClient(t, map[string]string{})
	if _, err := c.Treble(ctx()); err == nil {
		t.Error("expected error when the speaker has no tone controls")
	}
}

func TestWifiOptimizedMapsPowerSaving(t *testing.T) {
	// power-saving enabled => NOT optimized.
	on := stubClient(t, map[string]string{
		"/systemtimeout": `<systemtimeout><powersaving_enabled>true</powersaving_enabled></systemtimeout>`,
	})
	if opt, err := on.WifiOptimized(ctx()); err != nil || opt {
		t.Errorf("power-saving on should mean not optimized: opt=%v err=%v", opt, err)
	}
	// power-saving disabled => optimized (Wi-Fi stays awake).
	off := stubClient(t, map[string]string{
		"/systemtimeout": `<systemtimeout><powersaving_enabled>false</powersaving_enabled></systemtimeout>`,
	})
	if opt, err := off.WifiOptimized(ctx()); err != nil || !opt {
		t.Errorf("power-saving off should mean optimized: opt=%v err=%v", opt, err)
	}
}

func TestWifiOptimizedUnsupported(t *testing.T) {
	// 200 but no powersaving field => treat as unsupported.
	c := stubClient(t, map[string]string{"/systemtimeout": `<systemtimeout/>`})
	if _, err := c.WifiOptimized(ctx()); err == nil {
		t.Error("expected error when powersaving field is absent")
	}
}

func TestNetworkInfoPrefersWifi(t *testing.T) {
	c := stubClient(t, map[string]string{
		"/networkInfo": `<networkInfo wifiProfileCount="1"><interfaces>` +
			`<interface type="ETHERNET_INTERFACE" name="eth0" ipAddress="" state="NETWORK_ETHERNET_DISCONNECTED"/>` +
			`<interface type="WIFI_INTERFACE" name="wlan0" ssid="HomeNet" signal="GOOD_SIGNAL" ipAddress="192.168.2.7" state="NETWORK_WIFI_CONNECTED"/>` +
			`</interfaces></networkInfo>`,
	})
	n, err := c.NetworkInfo(ctx())
	if err != nil {
		t.Fatal(err)
	}
	if n.Type != "wifi" || n.SSID != "HomeNet" || n.Signal != "good" || n.IP != "192.168.2.7" {
		t.Errorf("network parsed wrong: %+v", n)
	}
}

func TestSignalStrengthToken(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
	}{
		// Quality percentages (simulator / some builds).
		{"82", "excellent"},
		{"55", "good"},
		{"28", "fair"},
		{"12", "poor"},
		// RSSI in dBm (real firmware), normalised via 2*(dBm+100).
		{"-55", "excellent"}, // 90
		{"-65", "good"},      // 70
		{"-78", "fair"},      // 44
		{"-90", "poor"},      // 20
		// Out of range / unparseable -> no label.
		{"101", ""},
		{"nope", ""},
	} {
		if got := signalStrengthToken(tc.in); got != tc.want {
			t.Errorf("signalStrengthToken(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSignalQualityClampsDBM(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want int
		ok   bool
	}{
		{"-30", 100, true}, // 2*(70) = 140, clamped
		{"-100", 0, true},  // 2*(0) = 0
		{"-120", 0, true},  // below the linear range, clamped
		{"73", 73, true},   // percentage passes through
		{"101", 0, false},  // percentage out of range
		{"", 0, false},     // unparseable
	} {
		got, ok := signalQuality(tc.in)
		if got != tc.want || ok != tc.ok {
			t.Errorf("signalQuality(%q) = (%d, %v), want (%d, %v)", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// recordingServer captures the last POST path+body and answers 200, so write
// methods can be checked for the exact body the speaker expects.
func recordingServer(t *testing.T, lastPath, lastBody *string) *Client {
	t.Helper()
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		*lastPath = r.URL.Path
		*lastBody = string(b)
		w.WriteHeader(200)
	}))
	t.Cleanup(ts.Close)
	u, _ := url.Parse(ts.URL)
	c := New(u.Hostname())
	c.apiPort = u.Port()
	return c
}

func TestSetWifiOptimizedInvertsPowerSaving(t *testing.T) {
	var path, body string
	c := recordingServer(t, &path, &body)
	// Optimized => Wi-Fi awake => power-saving OFF.
	if err := c.SetWifiOptimized(ctx(), true); err != nil {
		t.Fatal(err)
	}
	if path != "/systemtimeout" || !strings.Contains(body, "<powersaving_enabled>false</powersaving_enabled>") {
		t.Errorf("optimized=true sent path=%q body=%q; want powersaving false", path, body)
	}
	if err := c.SetWifiOptimized(ctx(), false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(body, "<powersaving_enabled>true</powersaving_enabled>") {
		t.Errorf("optimized=false body=%q; want powersaving true", body)
	}
}

func TestSetTrebleBody(t *testing.T) {
	var path, body string
	c := recordingServer(t, &path, &body)
	if err := c.SetTreble(ctx(), 40); err != nil {
		t.Fatal(err)
	}
	if path != "/audioproducttonecontrols" || !strings.Contains(body, `<treble value="40"/>`) {
		t.Errorf("SetTreble sent path=%q body=%q", path, body)
	}
}

func TestDeviceSettingsErrorOnBadStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", 500)
	}))
	t.Cleanup(ts.Close)
	u, _ := url.Parse(ts.URL)
	c := New(u.Hostname())
	c.apiPort = u.Port()

	if _, err := c.Treble(ctx()); err == nil {
		t.Error("Treble should error on 500")
	}
	if _, err := c.WifiOptimized(ctx()); err == nil {
		t.Error("WifiOptimized should error on 500")
	}
	if _, err := c.NetworkInfo(ctx()); err == nil {
		t.Error("NetworkInfo should error on 500")
	}
}

func TestDeviceSettingsErrorOnMalformedXML(t *testing.T) {
	c := stubClient(t, map[string]string{
		"/audioproducttonecontrols": `}{ not xml`,
		"/systemtimeout":            `}{ not xml`,
		"/networkInfo":              `}{ not xml`,
	})
	if _, err := c.Treble(ctx()); err == nil {
		t.Error("Treble should error on malformed XML")
	}
	if _, err := c.WifiOptimized(ctx()); err == nil {
		t.Error("WifiOptimized should error on malformed XML")
	}
	if _, err := c.NetworkInfo(ctx()); err == nil {
		t.Error("NetworkInfo should error on malformed XML")
	}
}
