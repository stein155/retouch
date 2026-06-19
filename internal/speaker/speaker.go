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
	"strings"
	"time"
)

// Client controls one speaker.
type Client struct {
	host string
	http *http.Client
}

// New returns a Client for the given host (speaker IP, or 127.0.0.1 on-speaker).
func New(host string) *Client {
	return &Client{host: host, http: &http.Client{Timeout: 4 * time.Second}}
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
	info := &Info{DeviceID: x.DeviceID, Account: x.Account, Name: x.Name, Type: x.Type}
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
		Source     string `xml:"source,attr"`
		Track      string `xml:"track"`
		Artist     string `xml:"artist"`
		Station    string `xml:"stationName"`
		Art        string `xml:"art"`
		PlayStatus string `xml:"playStatus"`
	}
	if err := xml.Unmarshal(body, &np); err != nil {
		return nil, err
	}
	return &NowPlaying{
		Source: np.Source, Track: np.Track, Artist: np.Artist,
		Station: np.Station, Art: np.Art, PlayStatus: np.PlayStatus,
	}, nil
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
			StationID: p.CI.Location[strings.LastIndex(p.CI.Location, "/")+1:],
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

// StorePreset writes a native preset on the speaker (slot 1..6) via /storePreset,
// so it persists as a real button — exactly what the web UI's "save preset" needs.
func (c *Client) StorePreset(ctx context.Context, slot int, source, itemType, location, name, art string) error {
	ci := `<ContentItem source="` + xmlEsc(source) + `" type="` + xmlEsc(itemType) +
		`" location="` + xmlEsc(location) + `" sourceAccount="" isPresetable="true"><itemName>` + xmlEsc(name) + `</itemName>`
	if art != "" {
		ci += `<containerArt>` + xmlEsc(art) + `</containerArt>`
	}
	ci += `</ContentItem>`
	now := time.Now().Unix()
	body := fmt.Sprintf(`<preset id="%d" createdOn="%d" updatedOn="%d">%s</preset>`, slot, now, now, ci)
	return c.post(ctx, "/storePreset", body)
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

// Select tells the speaker to play a NATIVE ContentItem (e.g. a TUNEIN station or
// a LOCAL_INTERNET_RADIO custom stream). The speaker resolves the location against
// our local BMX service and plays it itself, so now_playing reports the real
// source (TUNEIN), not UPNP. location is a service-relative path such as
// "/v1/playback/station/s47309".
func (c *Client) Select(ctx context.Context, source, itemType, location, name, account string) error {
	ci := `<ContentItem source="` + xmlEsc(source) + `" type="` + xmlEsc(itemType) +
		`" location="` + xmlEsc(location) + `" sourceAccount="` + xmlEsc(account) +
		`" isPresetable="true"><itemName>` + xmlEsc(name) + `</itemName></ContentItem>`
	return c.post(ctx, "/select", ci)
}

func xmlEsc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}

// Wake nudges the speaker out of standby via the :17000 CLI (`sys power`), then
// briefly waits for it to leave STANDBY so a following Play is not dropped.
func (c *Client) Wake(ctx context.Context) {
	_ = c.cli(ctx, "sys power")
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		if np, err := c.NowPlaying(ctx); err == nil && np.Source != "STANDBY" {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

func (c *Client) get(ctx context.Context, path string) ([]byte, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, c.urlFor(path), nil)
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
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, c.urlFor(path), strings.NewReader(body))
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
	return fmt.Sprintf("http://%s:8090%s", c.host, path)
}

// cli sends one command to the :17000 diagnostic CLI and discards the reply.
func (c *Client) cli(ctx context.Context, cmd string) error {
	d := net.Dialer{Timeout: 3 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(c.host, "17000"))
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Write([]byte(cmd + "\n"))
	return err
}
