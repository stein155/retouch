package speaker

import (
	"net/http"
	"net/http/httptest"
	"net/url"
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
