// Package speaker talks to the SoundTouch firmware's own local interfaces: the REST
// API on port 8090 (now-playing, volume) and the diagnostic CLI on port 17000
// (wake from standby). All local, no cloud.
package speaker

import (
	"context"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// InternetRadioIcon is the stock SoundTouch local display asset for internet
// radio. ST20 can render this file path on its OLED; browsers cannot.
const InternetRadioIcon = "file:///opt/Bose/Internet-Radio.png"

// Client controls one speaker.
type Client struct {
	host    string
	apiPort string // SoundTouch REST API port (8090 on a real speaker)
	cliPort string // diagnostic CLI port (17000 on a real speaker)
	http    *http.Client
}

// New returns a Client for the given host. host is the speaker IP (or 127.0.0.1
// on-speaker); it may include a REST API port ("127.0.0.1:9090") for off-speaker
// testing, otherwise the firmware's default 8090 is used. The diagnostic CLI port
// is always 17000 on real hardware.
func New(host string) *Client {
	apiPort := "8090"
	if h, p, err := net.SplitHostPort(host); err == nil {
		host, apiPort = h, p
	}
	return &Client{host: host, apiPort: apiPort, cliPort: "17000", http: &http.Client{Timeout: 4 * time.Second}}
}

// Info is a trimmed view of /info: the speaker's identity. Used to fill the
// pairing stub's replayed account documents with this speaker's real values.
type Info struct {
	DeviceID  string // e.g. "F4E11E3B013F" (also the SCM MAC)
	Account   string // margeAccountUUID; empty when the speaker is not paired
	Name      string // user-visible speaker name
	Type      string // device model, e.g. "SoundTouch 10"
	Software  string // SCM firmware/software version
	IP        string // current LAN address
	SerialSCM string // SCM component serial
	SerialPkg string // PackagedProduct component serial
	MargeURL  string // current cloud URL the speaker reports (the one we redirect)
}

// Info reads the speaker's identity from /info.
func (c *Client) Info(ctx context.Context) (*Info, error) {
	body, err := c.get(ctx, "/info")
	if err != nil {
		return nil, err
	}
	var x struct {
		DeviceID   string `xml:"deviceID,attr"`
		Name       string `xml:"name"`
		Type       string `xml:"type"`
		Account    string `xml:"margeAccountUUID"`
		MargeURL   string `xml:"margeURL"`
		Components []struct {
			Category string `xml:"componentCategory"`
			Software string `xml:"softwareVersion"`
			Serial   string `xml:"serialNumber"`
		} `xml:"components>component"`
		Network []struct {
			IP string `xml:"ipAddress"`
		} `xml:"networkInfo"`
	}
	if err := xml.Unmarshal(body, &x); err != nil {
		return nil, err
	}
	info := &Info{DeviceID: x.DeviceID, Account: x.Account, Name: x.Name, Type: x.Type, MargeURL: strings.TrimSpace(x.MargeURL)}
	for _, comp := range x.Components {
		switch comp.Category {
		case "SCM":
			info.SerialSCM = comp.Serial
			info.Software = comp.Software
		case "PackagedProduct":
			info.SerialPkg = comp.Serial
		}
	}
	for _, n := range x.Network {
		if n.IP != "" {
			info.IP = n.IP
			break
		}
	}
	return info, nil
}

// SetMargeAccount points the speaker at a marge account, moving it to the
// associated (logged-in) state so native sources are allowed. The speaker resolves
// the account against its configured margeServerUrl (our stub).
func (c *Client) SetMargeAccount(ctx context.Context, accountID, authToken string) error {
	body := `<PairDeviceWithAccount><accountId>` + xmlEsc(accountID) +
		`</accountId><userAuthToken>` + xmlEsc(authToken) + `</userAuthToken></PairDeviceWithAccount>`
	return c.post(ctx, "/setMargeAccount", body)
}

// NowPlaying is a trimmed view of /now_playing.
type NowPlaying struct {
	Source     string `json:"source"`
	Track      string `json:"track"`
	Artist     string `json:"artist"`
	Station    string `json:"station"`
	StationID  string `json:"stationId,omitempty"`
	Art        string `json:"art"`
	PlayStatus string `json:"playStatus"`
}

// NowPlaying reads the speaker's current playback state.
func (c *Client) NowPlaying(ctx context.Context) (*NowPlaying, error) {
	body, err := c.get(ctx, "/now_playing")
	if err != nil {
		return nil, err
	}
	var np struct {
		Source string `xml:"source,attr"`
		CI     struct {
			Location string `xml:"location,attr"`
			Name     string `xml:"itemName"`
		} `xml:"ContentItem"`
		Track      string `xml:"track"`
		Artist     string `xml:"artist"`
		Station    string `xml:"stationName"`
		Art        string `xml:"art"`
		PlayStatus string `xml:"playStatus"`
	}
	if err := xml.Unmarshal(body, &np); err != nil {
		return nil, err
	}
	station := strings.TrimSpace(np.Station)
	if station == "" {
		station = strings.TrimSpace(np.CI.Name)
	}
	return &NowPlaying{
		Source: np.Source, Track: np.Track, Artist: np.Artist,
		Station: station, StationID: stationIDFromLocation(np.CI.Location), Art: np.Art, PlayStatus: np.PlayStatus,
	}, nil
}

func stationIDFromLocation(location string) string {
	location = strings.TrimSpace(location)
	if location == "" {
		return ""
	}
	if i := strings.LastIndex(location, "/"); i >= 0 {
		return location[i+1:]
	}
	return location
}

// Preset is one of the speaker's native preset buttons, as reported by /presets.
type Preset struct {
	Slot      int    `json:"slot"`
	Name      string `json:"name"`
	StationID string `json:"stationId"` // TuneIn guide id parsed from the location
	Location  string `json:"location"`
	Logo      string `json:"logo,omitempty"`
}

// Presets reads the speaker's native presets from /presets. These are the buttons
// the speaker actually has (set on the device or via marge), so the UI shows the real
// speaker state rather than a separate local list.
func (c *Client) Presets(ctx context.Context) ([]Preset, error) {
	body, err := c.get(ctx, "/presets")
	if err != nil {
		return nil, err
	}
	var x struct {
		Presets []struct {
			ID int `xml:"id,attr"`
			CI struct {
				Source   string `xml:"source,attr"`
				Location string `xml:"location,attr"`
				Name     string `xml:"itemName"`
				Art      string `xml:"containerArt"`
			} `xml:"ContentItem"`
		} `xml:"preset"`
	}
	if err := xml.Unmarshal(body, &x); err != nil {
		return nil, err
	}
	out := make([]Preset, 0, len(x.Presets))
	for _, p := range x.Presets {
		if p.CI.Location == "" {
			continue
		}
		out = append(out, Preset{
			Slot:      p.ID,
			Name:      p.CI.Name,
			StationID: stationIDFromLocation(p.CI.Location),
			Location:  p.CI.Location,
			Logo:      p.CI.Art,
		})
	}
	return out, nil
}

// SetName sets the speaker's display name via /name.
func (c *Client) SetName(ctx context.Context, name string) error {
	return c.post(ctx, "/name", "<name>"+xmlEsc(name)+"</name>")
}

// DefaultAppKey is the Bose Developer application key the /speaker audio-notification
// endpoint expects. The firmware validates it against Bose's (now-dead) auth host, so
// on-speaker ReTouch stands in for that host and accepts any key — but the firmware
// still sends one, so we send the well-known developer key the SoundTouch API uses.
const DefaultAppKey = "Ml7YGAI9JWjFhU7D348e86JPXtisddBa"

// Notification is an audio notification to play via the firmware's /speaker endpoint:
// a short clip that interrupts whatever is playing (or wakes the speaker from standby)
// and then resumes the previous source. Only URL is required.
type Notification struct {
	URL    string // http(s) URL of the clip; MP3/AAC/WMA/etc. Required.
	Volume int    // temporary playback volume; 0 keeps the current volume (clamped to 10–70 otherwise)
	Artist string // shown as NowPlaying artist (<service>)
	Album  string // shown as NowPlaying album (<message>)
	Track  string // shown as NowPlaying track (<reason>)
	AppKey string // Bose Developer app key; DefaultAppKey when empty
}

// PlayNotification plays an audio notification via POST /speaker. The speaker pauses
// the current source (or wakes from standby), plays the clip, then resumes. Audio
// notifications are only supported on the SoundTouch 10 and the Series III 20/30;
// other models answer 403 and that error is returned unchanged.
func (c *Client) PlayNotification(ctx context.Context, n Notification) error {
	if strings.TrimSpace(n.URL) == "" {
		return fmt.Errorf("notification URL is required")
	}
	appKey := n.AppKey
	if appKey == "" {
		appKey = DefaultAppKey
	}
	var b strings.Builder
	b.WriteString("<play_info><url>" + xmlEsc(n.URL) + "</url>")
	// The firmware rejects the request outright for volumes below 10 or above 70;
	// clamp into range, and omit the node entirely for 0 to keep the current volume.
	if n.Volume > 0 {
		v := n.Volume
		if v < 10 {
			v = 10
		} else if v > 70 {
			v = 70
		}
		fmt.Fprintf(&b, "<volume>%d</volume>", v)
	}
	b.WriteString("<app_key>" + xmlEsc(appKey) + "</app_key>")
	if n.Artist != "" {
		b.WriteString("<service>" + xmlEsc(n.Artist) + "</service>")
	}
	if n.Album != "" {
		b.WriteString("<message>" + xmlEsc(n.Album) + "</message>")
	}
	if n.Track != "" {
		b.WriteString("<reason>" + xmlEsc(n.Track) + "</reason>")
	}
	b.WriteString("</play_info>")
	return c.post(ctx, "/speaker", b.String())
}

// Bass is the speaker's bass level + its capability range.
type Bass struct {
	Actual  int `json:"actual"`
	Target  int `json:"target"`
	Min     int `json:"min"`
	Max     int `json:"max"`
	Default int `json:"default"`
}

// Bass reads the current bass level (/bass) and range (/bassCapabilities).
func (c *Client) Bass(ctx context.Context) (*Bass, error) {
	lvl, err := c.get(ctx, "/bass")
	if err != nil {
		return nil, err
	}
	var l struct {
		Target int `xml:"targetbass"`
		Actual int `xml:"actualbass"`
	}
	if err := xml.Unmarshal(lvl, &l); err != nil {
		return nil, err
	}
	b := &Bass{Actual: l.Actual, Target: l.Target}
	if caps, err := c.get(ctx, "/bassCapabilities"); err == nil {
		var cp struct {
			Min     int `xml:"bassMin"`
			Max     int `xml:"bassMax"`
			Default int `xml:"bassDefault"`
		}
		if xml.Unmarshal(caps, &cp) == nil {
			b.Min, b.Max, b.Default = cp.Min, cp.Max, cp.Default
		}
	}
	return b, nil
}

// SetBass sets the bass level via /bass (clamped to the capability range).
func (c *Client) SetBass(ctx context.Context, level int) error {
	return c.post(ctx, "/bass", fmt.Sprintf("<bass>%d</bass>", level))
}

// Tone is a tone control (treble) value and its capability range, read from
// /audioproducttonecontrols. Step is the increment the speaker accepts.
type Tone struct {
	Value int `json:"value"`
	Min   int `json:"min"`
	Max   int `json:"max"`
	Step  int `json:"step"`
}

// Treble reads the treble control via /audioproducttonecontrols. This endpoint is
// capability-gated: speakers without tone controls (most SoundTouch models) do not
// expose it, so the error here is the signal to hide the control in the UI.
func (c *Client) Treble(ctx context.Context) (*Tone, error) {
	body, err := c.get(ctx, "/audioproducttonecontrols")
	if err != nil {
		return nil, err
	}
	var x struct {
		Treble struct {
			Value int `xml:"value,attr"`
			Min   int `xml:"minValue,attr"`
			Max   int `xml:"maxValue,attr"`
			Step  int `xml:"step,attr"`
		} `xml:"treble"`
	}
	if err := xml.Unmarshal(body, &x); err != nil {
		return nil, err
	}
	if x.Treble.Min == 0 && x.Treble.Max == 0 {
		return nil, fmt.Errorf("speaker has no treble control")
	}
	t := &Tone{Value: x.Treble.Value, Min: x.Treble.Min, Max: x.Treble.Max, Step: x.Treble.Step}
	if t.Step <= 0 {
		t.Step = 1
	}
	return t, nil
}

// SetTreble sets the treble value via /audioproducttonecontrols (bass is left
// untouched by omitting it from the request).
func (c *Client) SetTreble(ctx context.Context, value int) error {
	return c.post(ctx, "/audioproducttonecontrols",
		fmt.Sprintf(`<audioproducttonecontrols><treble value="%d"/></audioproducttonecontrols>`, value))
}

// WifiOptimized reports whether the speaker keeps its Wi-Fi awake in standby
// (power-saving disabled) via /systemtimeout. With Wi-Fi kept awake, AirPlay and
// streaming wake the speaker instantly; with power-saving on, the radio sleeps to
// save power and takes longer to respond. Returns an error when the speaker does
// not expose the setting, which the UI uses to hide the toggle.
func (c *Client) WifiOptimized(ctx context.Context) (bool, error) {
	body, err := c.get(ctx, "/systemtimeout")
	if err != nil {
		return false, err
	}
	var x struct {
		PowerSaving *bool `xml:"powersaving_enabled"`
	}
	if err := xml.Unmarshal(body, &x); err != nil {
		return false, err
	}
	if x.PowerSaving == nil {
		return false, fmt.Errorf("speaker has no power-saving setting")
	}
	// "Optimized" means power-saving is OFF, so the Wi-Fi radio stays awake.
	return !*x.PowerSaving, nil
}

// SetWifiOptimized turns the streaming/AirPlay Wi-Fi optimization on or off by
// toggling the speaker's power-saving the opposite way via /systemtimeout.
func (c *Client) SetWifiOptimized(ctx context.Context, optimized bool) error {
	return c.post(ctx, "/systemtimeout",
		fmt.Sprintf("<systemtimeout><powersaving_enabled>%t</powersaving_enabled></systemtimeout>", !optimized))
}

// Network is a compact, read-only view of the speaker's active network connection,
// read from /networkInfo (the connected interface — Wi-Fi preferred).
type Network struct {
	Type   string `json:"type"`             // "wifi" or "ethernet"
	SSID   string `json:"ssid,omitempty"`   // Wi-Fi network name
	Signal string `json:"signal,omitempty"` // "excellent" | "good" | "fair" | "poor"
	IP     string `json:"ip,omitempty"`     // current LAN address
}

// NetworkInfo reads /networkInfo and returns the active connection. It prefers a
// connected Wi-Fi interface, then any interface carrying an IP address.
func (c *Client) NetworkInfo(ctx context.Context) (*Network, error) {
	body, err := c.get(ctx, "/networkInfo")
	if err != nil {
		return nil, err
	}
	var x struct {
		Interfaces []struct {
			Type   string `xml:"type,attr"`
			SSID   string `xml:"ssid,attr"`
			IP     string `xml:"ipAddress,attr"`
			Signal string `xml:"signal,attr"`
		} `xml:"interfaces>interface"`
	}
	if err := xml.Unmarshal(body, &x); err != nil {
		return nil, err
	}
	var fallback *Network
	for _, in := range x.Interfaces {
		wifi := strings.Contains(strings.ToUpper(in.Type), "WIFI")
		n := &Network{IP: in.IP, SSID: in.SSID}
		if wifi {
			n.Type = "wifi"
			n.Signal = prettySignal(in.Signal)
		} else {
			n.Type = "ethernet"
		}
		if wifi && in.IP != "" {
			return n, nil // prefer a connected Wi-Fi interface
		}
		if fallback == nil && in.IP != "" {
			fallback = n
		}
	}
	if fallback == nil {
		return nil, fmt.Errorf("no active network interface")
	}
	return fallback, nil
}

// prettySignal turns the speaker's EXCELLENT_SIGNAL-style enum into a short token
// the UI can localise; unknown values pass through lower-cased.
func prettySignal(s string) string {
	switch strings.ToUpper(s) {
	case "EXCELLENT_SIGNAL":
		return "excellent"
	case "GOOD_SIGNAL":
		return "good"
	case "FAIR_SIGNAL", "MARGINAL_SIGNAL":
		return "fair"
	case "POOR_SIGNAL", "BAD_SIGNAL":
		return "poor"
	case "":
		return ""
	default:
		return strings.ToLower(strings.TrimSuffix(s, "_SIGNAL"))
	}
}

// WifiNetwork is one nearby Wi-Fi network seen during a site survey (see ScanWifi).
type WifiNetwork struct {
	SSID   string `json:"ssid"`
	Signal string `json:"signal,omitempty"` // "excellent" | "good" | "fair" | "poor"
	Secure bool   `json:"secure"`           // needs a password to join
}

// ScanWifi surveys nearby Wi-Fi networks via the native /performWirelessSiteSurvey
// endpoint (:8090). Not every model exposes it (the firmware gates it behind its
// supportedUris), so a device that can't survey returns an error here — the web
// layer turns that into an empty list and the UI falls back to manual SSID entry.
//
// A site survey makes the radio sweep every channel, which takes many seconds —
// far longer than the shared client's short timeout — so it runs on a dedicated
// long-timeout request bounded by ctx.
func (c *Client) ScanWifi(ctx context.Context) ([]WifiNetwork, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor("/performWirelessSiteSurvey"), nil)
	resp, err := (&http.Client{Timeout: 25 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET /performWirelessSiteSurvey status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, err
	}
	return parseSiteSurvey(body), nil
}

// parseSiteSurvey reads the /performWirelessSiteSurvey response:
//
//	<performWirelessSiteSurvey><items>
//	  <item ssid="Home" signalStrength="72" secure="true">
//	    <securityTypes><type>wpa_or_wpa2</type></securityTypes>
//	  </item>
//	</items></performWirelessSiteSurvey>
//
// Networks without an SSID (hidden) are skipped and duplicate SSIDs collapsed.
func parseSiteSurvey(body []byte) []WifiNetwork {
	var x struct {
		Items []struct {
			SSID           string `xml:"ssid,attr"`
			SignalStrength string `xml:"signalStrength,attr"`
			Secure         string `xml:"secure,attr"`
		} `xml:"items>item"`
	}
	if err := xml.Unmarshal(body, &x); err != nil {
		return nil
	}
	var out []WifiNetwork
	seen := map[string]bool{}
	for _, it := range x.Items {
		if it.SSID == "" || seen[it.SSID] {
			continue
		}
		seen[it.SSID] = true
		out = append(out, WifiNetwork{
			SSID:   it.SSID,
			Signal: signalStrengthToken(it.SignalStrength),
			Secure: !strings.EqualFold(it.Secure, "false") && it.Secure != "0",
		})
	}
	return out
}

// signalStrengthToken buckets the survey's numeric signalStrength into the same
// tokens NetworkInfo uses, so the UI can localise it. Firmware reports either
// quality (0..100) or RSSI in dBm (negative); anything else yields "" and the UI
// then shows no signal label.
func signalStrengthToken(s string) string {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return ""
	}
	if n < 0 {
		switch {
		case n >= -60:
			return "excellent"
		case n >= -70:
			return "good"
		case n >= -80:
			return "fair"
		default:
			return "poor"
		}
	}
	if n > 100 {
		return ""
	}
	switch {
	case n >= 75:
		return "excellent"
	case n >= 50:
		return "good"
	case n >= 25:
		return "fair"
	default:
		return "poor"
	}
}

// SetWifi joins a Wi-Fi network via the native /addWirelessProfile endpoint (:8090).
// security is "none", "wep" or "wpa_or_wpa2" (the default). Switching networks can
// briefly drop the speaker's current connection, so the caller warns the user first.
func (c *Client) SetWifi(ctx context.Context, ssid, security, password string) error {
	ssid = strings.TrimSpace(ssid)
	if ssid == "" {
		return fmt.Errorf("ssid required")
	}
	switch security {
	case "", "wpa_or_wpa2":
		security = "wpa_or_wpa2"
	case "none", "wep":
	default:
		return fmt.Errorf("unknown security %q", security)
	}
	body := fmt.Sprintf(`<AddWirelessProfile timeout="30"><profile ssid="%s" password="%s" securityType="%s"/></AddWirelessProfile>`,
		xmlEsc(ssid), xmlEsc(password), security)
	return c.post(ctx, "/addWirelessProfile", body)
}

// StorePreset writes a native preset on the speaker (slot 1..6) via /storePreset,
// so it persists as a real button — exactly what the web UI's "save preset" needs.
func (c *Client) StorePreset(ctx context.Context, slot int, source, itemType, location, name, art string) error {
	ci := contentItemXML(source, itemType, location, "", name, art)
	now := time.Now().Unix()
	body := fmt.Sprintf(`<preset id="%d" createdOn="%d" updatedOn="%d">%s</preset>`, slot, now, now, ci)
	return c.post(ctx, "/storePreset", body)
}

// contentItemXML builds the <ContentItem> element shared by /storePreset and
// /select. Field names/casing mirror the SoundTouch wire format; account and art
// are optional (empty ones are omitted/blank exactly as the firmware expects).
func contentItemXML(source, itemType, location, account, name, art string) string {
	ci := `<ContentItem source="` + xmlEsc(source) + `" type="` + xmlEsc(itemType) +
		`" location="` + xmlEsc(location) + `" sourceAccount="` + xmlEsc(account) +
		`" isPresetable="true"><itemName>` + xmlEsc(name) + `</itemName>`
	if art != "" {
		ci += `<containerArt>` + xmlEsc(art) + `</containerArt>`
	}
	return ci + `</ContentItem>`
}

// RemovePreset clears a native preset slot (1..6) via /removePreset.
func (c *Client) RemovePreset(ctx context.Context, slot int) error {
	return c.post(ctx, "/removePreset", fmt.Sprintf(`<preset id="%d"></preset>`, slot))
}

// Volume returns the speaker's current (actual) volume, 0..100.
func (c *Client) Volume(ctx context.Context) (int, error) {
	body, err := c.get(ctx, "/volume")
	if err != nil {
		return 0, err
	}
	var v struct {
		Actual int `xml:"actualvolume"`
	}
	if err := xml.Unmarshal(body, &v); err != nil {
		return 0, err
	}
	return v.Actual, nil
}

// SetVolume sets the speaker volume, clamped to 0..100.
func (c *Client) SetVolume(ctx context.Context, level int) error {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	body := fmt.Sprintf("<volume>%d</volume>", level)
	return c.post(ctx, "/volume", body)
}

// Key sends a remote key press+release to the speaker (e.g. PAUSE, PLAY_PAUSE).
func (c *Client) Key(ctx context.Context, key string) error {
	if err := c.post(ctx, "/key", `<key state="press" sender="Gabbo">`+xmlEsc(key)+`</key>`); err != nil {
		return err
	}
	return c.post(ctx, "/key", `<key state="release" sender="Gabbo">`+xmlEsc(key)+`</key>`)
}

// PlayPreset plays the speaker's native preset in slot (1..6) by waking it and
// pressing the matching PRESET_n key. Centralises the wake-then-key sequence and
// the "PRESET_"+n key-name convention so callers don't reassemble the protocol.
func (c *Client) PlayPreset(ctx context.Context, slot int) error {
	c.Wake(ctx)
	return c.Key(ctx, "PRESET_"+strconv.Itoa(slot))
}

// Select tells the speaker to play a NATIVE ContentItem (e.g. a TUNEIN station or
// a LOCAL_INTERNET_RADIO custom stream). The speaker resolves the location against
// our local BMX service and plays it itself, so now_playing reports the real
// source (TUNEIN), not UPNP. location is a service-relative path such as
// "/v1/playback/station/s47309".
func (c *Client) Select(ctx context.Context, source, itemType, location, name, account, art string) error {
	return c.post(ctx, "/select", contentItemXML(source, itemType, location, account, name, art))
}

// Member is one speaker in a multiroom zone: its deviceID (which is also the
// speaker's MAC, used as the zone's master id and a member's text content) and
// its current LAN address.
type Member struct {
	DeviceID string `json:"deviceId"`
	IP       string `json:"ip"`
}

// Zone is the speaker's current multiroom grouping as reported by /getZone. Master
// is the master speaker's deviceID (empty when the speaker is not in any zone);
// Members lists every speaker in the zone (the master first, then the slaves).
type Zone struct {
	Master  string   `json:"master"`
	Members []Member `json:"members"`
}

// GetZone reads the speaker's current multiroom zone via /getZone. An ungrouped
// speaker reports an empty <zone/>, so Master == "" and Members is empty.
func (c *Client) GetZone(ctx context.Context) (*Zone, error) {
	body, err := c.get(ctx, "/getZone")
	if err != nil {
		return nil, err
	}
	var x struct {
		Master  string `xml:"master,attr"`
		Members []struct {
			IP       string `xml:"ipaddress,attr"`
			DeviceID string `xml:",chardata"`
		} `xml:"member"`
	}
	if err := xml.Unmarshal(body, &x); err != nil {
		return nil, err
	}
	z := &Zone{Master: strings.TrimSpace(x.Master)}
	for _, m := range x.Members {
		id := strings.TrimSpace(m.DeviceID)
		if id == "" {
			continue
		}
		z.Members = append(z.Members, Member{DeviceID: id, IP: strings.TrimSpace(m.IP)})
	}
	return z, nil
}

// SetZone creates (or replaces) a multiroom zone with master as the controlling
// speaker and slaves as the other members; it is POSTed to the master itself.
// The Bose firmware syncs the slaves to whatever the master is playing. The
// master is listed first as a member and senderIPAddress carries the master IP —
// the form the firmware expects when establishing a fresh zone.
func (c *Client) SetZone(ctx context.Context, master Member, slaves []Member) error {
	return c.post(ctx, "/setZone", setZoneBody(master, slaves))
}

// AddZoneSlave adds slaves to the existing zone mastered by master. Unlike
// SetZone it omits senderIPAddress and lists only the speakers being added.
func (c *Client) AddZoneSlave(ctx context.Context, master Member, slaves []Member) error {
	return c.zoneMembers(ctx, "/addZoneSlave", master, slaves)
}

// RemoveZoneSlave removes slaves from the zone mastered by master. When the last
// slave is removed the firmware dissolves the zone.
func (c *Client) RemoveZoneSlave(ctx context.Context, master Member, slaves []Member) error {
	return c.zoneMembers(ctx, "/removeZoneSlave", master, slaves)
}

func (c *Client) zoneMembers(ctx context.Context, path string, master Member, slaves []Member) error {
	return c.post(ctx, path, zoneBody(master, slaves))
}

// setZoneBody builds the /setZone request: the master carries senderIPAddress on
// the <zone> element, and ONLY the slaves are listed as members (the master is
// identified by the master attribute, not repeated as a member). This matches the
// battle-tested libsoundtouch / Home Assistant implementation.
func setZoneBody(master Member, slaves []Member) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<zone master="%s" senderIPAddress="%s">`, xmlEsc(master.DeviceID), xmlEsc(master.IP))
	for _, s := range slaves {
		b.WriteString(memberXML(s))
	}
	b.WriteString(`</zone>`)
	return b.String()
}

// zoneBody builds the /addZoneSlave and /removeZoneSlave request: just the master
// id and the members being added/removed (no senderIPAddress, no master member).
func zoneBody(master Member, slaves []Member) string {
	var b strings.Builder
	fmt.Fprintf(&b, `<zone master="%s">`, xmlEsc(master.DeviceID))
	for _, s := range slaves {
		b.WriteString(memberXML(s))
	}
	b.WriteString(`</zone>`)
	return b.String()
}

func memberXML(m Member) string {
	return `<member ipaddress="` + xmlEsc(m.IP) + `">` + xmlEsc(m.DeviceID) + `</member>`
}

func xmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// Wake nudges the speaker out of standby via the :17000 CLI (`sys power`), then
// briefly waits for it to leave STANDBY so a following Play is not dropped.
//
// `sys power` is a TOGGLE, not a power-on, so it is only sent when the speaker is
// actually in standby — otherwise waking an already-playing speaker would switch
// it OFF and then stall the full 6s waiting to leave a standby we just caused.
// (Mirrors habridge.setPower, which guards Wake the same way.)
func (c *Client) Wake(ctx context.Context) {
	if np, err := c.NowPlaying(ctx); err == nil && np.Source != "STANDBY" {
		return // already awake
	}
	_ = c.cli(ctx, "sys power")
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if np, err := c.NowPlaying(ctx); err == nil && np.Source != "STANDBY" {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor(path), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s status %d", path, resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 64*1024))
}

func (c *Client) post(ctx context.Context, path, body string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.urlFor(path), strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("POST %s status %d", path, resp.StatusCode)
	}
	return nil
}

func (c *Client) urlFor(path string) string {
	return fmt.Sprintf("http://%s:%s%s", c.host, c.apiPort, path)
}

// cli sends one command to the :17000 diagnostic CLI and discards the reply.
func (c *Client) cli(ctx context.Context, cmd string) error {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(c.host, c.cliPort))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Write([]byte(cmd + "\n"))
	return err
}
