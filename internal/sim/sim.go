// Package sim is a SoundTouch speaker simulator: it answers the firmware's local
// REST API (port 8090) and diagnostic CLI (port 17000) with the same wire format a
// real Bose SoundTouch speaker produces, and holds the matching state in memory.
//
// It exists so the firmware (internal/speaker and friends) can be exercised in tests
// and by hand without a physical speaker. The handler is mountable on httptest in
// tests; cmd/soundtouch-sim runs it on the real 8090/17000 ports for manual use.
//
// Scope: the endpoints internal/speaker actually calls. State transitions mirror the
// speaker's observable behaviour (set volume -> /volume reports it, store a preset ->
// /presets lists it, select -> /now_playing reflects it, zones, wake-from-standby).
package sim

import (
	"bufio"
	"encoding/xml"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Preset is one stored preset slot on the simulated speaker.
type Preset struct {
	Source, Type, Location, Name, Art string
}

// member is one speaker in the simulated multiroom zone.
type member struct{ deviceID, ip string }

// Speaker is a simulated SoundTouch speaker. The zero value is not usable; use New.
// All exported fields are part of the simulated /info identity and are safe to set
// before the server starts serving. Everything else is guarded by mu.
type Speaker struct {
	DeviceID  string
	Name      string
	Type      string
	Software  string
	IP        string
	Account   string
	MargeURL  string
	SerialSCM string
	SerialPkg string

	mu sync.Mutex

	volume      int
	bassTarget  int
	bassActual  int
	bassMin     int
	bassMax     int
	bassDefault int

	trebleTarget int
	trebleMin    int
	trebleMax    int
	trebleStep   int

	powerSaving bool   // /systemtimeout: Wi-Fi sleeps in standby when true
	wifiSSID    string // /networkInfo: connected Wi-Fi network
	wifiSignal  string // /networkInfo: e.g. "GOOD_SIGNAL"

	presets map[int]Preset

	source     string // "STANDBY" when idle/off
	lastSource string // last non-standby source, restored on wake
	track      string
	artist     string
	station    string
	location   string
	art        string
	playStatus string

	zoneMaster  string // master deviceID; "" when ungrouped
	zoneSenders string // master IP (senderIPAddress)
	zoneMembers []member
}

// New returns a simulated speaker seeded with sensible defaults. Identity fields can
// be overridden before serving.
func New() *Speaker {
	return &Speaker{
		DeviceID:     "F4E11E3B013F",
		Name:         "Keuken",
		Type:         "SoundTouch 10",
		Software:     "27.0.6.46330.5043500",
		IP:           "192.168.1.42",
		Account:      "1234567",
		MargeURL:     "https://streaming.bose.com",
		SerialSCM:    "07294150369404420AE",
		SerialPkg:    "071624P70360032AE",
		volume:       25,
		bassTarget:   0,
		bassActual:   0,
		bassMin:      -9,
		bassMax:      0,
		bassDefault:  0,
		trebleTarget: 0,
		trebleMin:    -100,
		trebleMax:    100,
		trebleStep:   10,
		powerSaving:  false,
		wifiSSID:     "HomeWiFi",
		wifiSignal:   "GOOD_SIGNAL",
		presets:      map[int]Preset{},
		source:       "STANDBY",
		playStatus:   "STOP_STATE",
	}
}

// Handler returns the speaker's :8090 REST API as an http.Handler, ready to mount on
// an httptest.Server.
func (s *Speaker) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/info", s.getInfo)
	mux.HandleFunc("/now_playing", s.getNowPlaying)
	mux.HandleFunc("/presets", s.getPresets)
	mux.HandleFunc("/volume", s.volumeHandler)
	mux.HandleFunc("/bass", s.bassHandler)
	mux.HandleFunc("/bassCapabilities", s.getBassCaps)
	mux.HandleFunc("/audioproducttonecontrols", s.toneHandler)
	mux.HandleFunc("/systemtimeout", s.systemTimeoutHandler)
	mux.HandleFunc("/networkInfo", s.getNetworkInfo)
	mux.HandleFunc("/getZone", s.getZone)
	// These endpoints mutate state; the real speaker only accepts them over POST and
	// returns N/A for GET, so reject other methods to mirror that.
	mux.HandleFunc("/name", postOnly(s.postName))
	mux.HandleFunc("/key", postOnly(s.postKey))
	mux.HandleFunc("/select", postOnly(s.postSelect))
	mux.HandleFunc("/storePreset", postOnly(s.postStorePreset))
	mux.HandleFunc("/removePreset", postOnly(s.postRemovePreset))
	mux.HandleFunc("/setMargeAccount", postOnly(s.postMargeAccount))
	mux.HandleFunc("/setZone", postOnly(s.postSetZone))
	mux.HandleFunc("/addZoneSlave", postOnly(s.postAddZoneSlave))
	mux.HandleFunc("/removeZoneSlave", postOnly(s.postRemoveZoneSlave))
	return mux
}

// postOnly rejects non-POST requests with 405, as the real speaker does for its
// state-changing endpoints.
func postOnly(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		h(w, r)
	}
}

// ServeCLI runs the :17000 diagnostic CLI on ln until ln is closed. It handles the
// commands internal/speaker sends: "sys power" (wake/standby toggle), a Wi-Fi site
// survey ("network wifi scan"), and joining a network ("network wifi profiles add").
// Unknown commands are accepted and ignored, like the real CLI's prompt.
func (s *Speaker) ServeCLI(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go s.handleCLI(conn)
	}
}

func (s *Speaker) handleCLI(conn net.Conn) {
	defer func() { _ = conn.Close() }()
	sc := bufio.NewScanner(conn)
	for sc.Scan() {
		cmd := strings.TrimSpace(sc.Text())
		switch {
		case cmd == "sys power":
			s.togglePower()
		case strings.HasPrefix(cmd, "network wifi scan"):
			_, _ = conn.Write([]byte(s.wifiScanReply()))
		case strings.HasPrefix(cmd, "network wifi profiles add "):
			s.addWifiProfile(strings.TrimPrefix(cmd, "network wifi profiles add "))
		}
	}
}

// wifiScanReply returns a plausible site-survey response. The real firmware's wire
// format is undocumented, so this mirrors the attribute style parseWifiScan expects.
func (s *Speaker) wifiScanReply() string {
	s.mu.Lock()
	current := s.wifiSSID
	s.mu.Unlock()
	return `<WiFiScanResults>` +
		`<scanResult ssid="` + esc(current) + `" signal="EXCELLENT_SIGNAL" secure="true"/>` +
		`<scanResult ssid="Neighbour 5G" signal="GOOD_SIGNAL" secure="true"/>` +
		`<scanResult ssid="CoffeeBar Free" signal="FAIR_SIGNAL" secure="false"/>` +
		`</WiFiScanResults>` + "\n"
}

// addWifiProfile records a joined network so /networkInfo reflects the switch. args
// is "<ssid> <security> [<password>]"; the SSID is the first whitespace field.
func (s *Speaker) addWifiProfile(args string) {
	fields := strings.Fields(args)
	if len(fields) == 0 {
		return
	}
	s.mu.Lock()
	s.wifiSSID = fields[0]
	s.mu.Unlock()
}

func (s *Speaker) togglePower() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.source == "STANDBY" {
		if s.lastSource == "" {
			s.lastSource = "TUNEIN"
		}
		s.source = s.lastSource
	} else {
		s.lastSource = s.source
		s.source = "STANDBY"
	}
}

// --- GET handlers ---

func (s *Speaker) getInfo(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeXML(w, fmt.Sprintf(`<info deviceID="%s">`+
		`<name>%s</name><type>%s</type>`+
		`<margeAccountUUID>%s</margeAccountUUID><margeURL>%s</margeURL>`+
		`<components>`+
		`<component><componentCategory>SCM</componentCategory><softwareVersion>%s</softwareVersion><serialNumber>%s</serialNumber></component>`+
		`<component><componentCategory>PackagedProduct</componentCategory><serialNumber>%s</serialNumber></component>`+
		`</components>`+
		`<networkInfo type="SCM"><ipAddress>%s</ipAddress></networkInfo>`+
		`</info>`,
		esc(s.DeviceID), esc(s.Name), esc(s.Type), esc(s.Account), esc(s.MargeURL),
		esc(s.Software), esc(s.SerialSCM), esc(s.SerialPkg), esc(s.IP)))
}

func (s *Speaker) getNowPlaying(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.source == "STANDBY" {
		writeXML(w, fmt.Sprintf(`<nowPlaying deviceID="%s" source="STANDBY"><ContentItem source="STANDBY" isPresetable="false"/></nowPlaying>`, esc(s.DeviceID)))
		return
	}
	writeXML(w, fmt.Sprintf(`<nowPlaying deviceID="%s" source="%s">`+
		`<ContentItem source="%s" type="stationurl" location="%s" sourceAccount="" isPresetable="true"><itemName>%s</itemName></ContentItem>`+
		`<track>%s</track><artist>%s</artist><stationName>%s</stationName>`+
		`<art artImageStatus="IMAGE_PRESENT">%s</art><playStatus>%s</playStatus>`+
		`<streamType>RADIO_STREAMING</streamType></nowPlaying>`,
		esc(s.DeviceID), esc(s.source), esc(s.source), esc(s.location), esc(s.station),
		esc(s.track), esc(s.artist), esc(s.station), esc(s.art), esc(s.playStatus)))
}

func (s *Speaker) getPresets(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var b strings.Builder
	b.WriteString("<presets>")
	for _, slot := range sortedSlots(s.presets) {
		p := s.presets[slot]
		fmt.Fprintf(&b, `<preset id="%d"><ContentItem source="%s" type="%s" location="%s" sourceAccount="" isPresetable="true"><itemName>%s</itemName>`,
			slot, esc(p.Source), esc(p.Type), esc(p.Location), esc(p.Name))
		if p.Art != "" {
			fmt.Fprintf(&b, `<containerArt>%s</containerArt>`, esc(p.Art))
		}
		b.WriteString("</ContentItem></preset>")
	}
	b.WriteString("</presets>")
	writeXML(w, b.String())
}

func (s *Speaker) getBassCaps(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeXML(w, fmt.Sprintf(`<bassCapabilities deviceID="%s"><bassAvailable>true</bassAvailable><bassMin>%d</bassMin><bassMax>%d</bassMax><bassDefault>%d</bassDefault></bassCapabilities>`,
		esc(s.DeviceID), s.bassMin, s.bassMax, s.bassDefault))
}

// toneHandler serves /audioproducttonecontrols: GET reports bass + treble with their
// ranges; POST applies any field present (the client only sends treble here).
func (s *Speaker) toneHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var t struct {
			Bass *struct {
				Value int `xml:"value,attr"`
			} `xml:"bass"`
			Treble *struct {
				Value int `xml:"value,attr"`
			} `xml:"treble"`
		}
		if !decode(w, r, &t) {
			return
		}
		s.mu.Lock()
		if t.Bass != nil {
			s.bassTarget, s.bassActual = clamp(t.Bass.Value, s.bassMin, s.bassMax), clamp(t.Bass.Value, s.bassMin, s.bassMax)
		}
		if t.Treble != nil {
			s.trebleTarget = clamp(t.Treble.Value, s.trebleMin, s.trebleMax)
		}
		s.mu.Unlock()
		writeXML(w, "<status>/audioproducttonecontrols</status>")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeXML(w, fmt.Sprintf(`<audioproducttonecontrols>`+
		`<bass value="%d" minValue="%d" maxValue="%d" step="1"/>`+
		`<treble value="%d" minValue="%d" maxValue="%d" step="%d"/>`+
		`</audioproducttonecontrols>`,
		s.bassTarget, s.bassMin, s.bassMax,
		s.trebleTarget, s.trebleMin, s.trebleMax, s.trebleStep))
}

// systemTimeoutHandler serves /systemtimeout: GET reports the power-saving flag, POST
// sets it. With power-saving off the Wi-Fi radio stays awake in standby.
func (s *Speaker) systemTimeoutHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var t struct {
			PowerSaving *bool `xml:"powersaving_enabled"`
		}
		if !decode(w, r, &t) {
			return
		}
		if t.PowerSaving != nil {
			s.mu.Lock()
			s.powerSaving = *t.PowerSaving
			s.mu.Unlock()
		}
		writeXML(w, "<status>/systemtimeout</status>")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeXML(w, fmt.Sprintf(`<systemtimeout><powersaving_enabled>%t</powersaving_enabled></systemtimeout>`, s.powerSaving))
}

// getNetworkInfo serves /networkInfo with a single connected Wi-Fi interface.
func (s *Speaker) getNetworkInfo(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	writeXML(w, fmt.Sprintf(`<networkInfo wifiProfileCount="1"><interfaces>`+
		`<interface type="WIFI_INTERFACE" name="wlan0" ssid="%s" signal="%s" ipAddress="%s" state="NETWORK_WIFI_CONNECTED"/>`+
		`</interfaces></networkInfo>`,
		esc(s.wifiSSID), esc(s.wifiSignal), esc(s.IP)))
}

func (s *Speaker) getZone(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.zoneMaster == "" {
		writeXML(w, "<zone />")
		return
	}
	var b strings.Builder
	fmt.Fprintf(&b, `<zone master="%s" senderIPAddress="%s">`, esc(s.zoneMaster), esc(s.zoneSenders))
	for _, m := range s.zoneMembers {
		fmt.Fprintf(&b, `<member ipaddress="%s">%s</member>`, esc(m.ip), esc(m.deviceID))
	}
	b.WriteString("</zone>")
	writeXML(w, b.String())
}

// --- GET/POST handlers (method-switched) ---

func (s *Speaker) volumeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var v struct {
			Value int `xml:",chardata"`
		}
		if !decode(w, r, &v) {
			return
		}
		s.mu.Lock()
		s.volume = clamp(v.Value, 0, 100)
		s.mu.Unlock()
		writeXML(w, "<status>/volume</status>")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeXML(w, fmt.Sprintf(`<volume deviceID="%s"><targetvolume>%d</targetvolume><actualvolume>%d</actualvolume><muteenabled>false</muteenabled></volume>`,
		esc(s.DeviceID), s.volume, s.volume))
}

func (s *Speaker) bassHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		var b struct {
			Value int `xml:",chardata"`
		}
		if !decode(w, r, &b) {
			return
		}
		s.mu.Lock()
		lvl := clamp(b.Value, s.bassMin, s.bassMax)
		s.bassTarget, s.bassActual = lvl, lvl
		s.mu.Unlock()
		writeXML(w, "<status>/bass</status>")
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	writeXML(w, fmt.Sprintf(`<bass deviceID="%s"><targetbass>%d</targetbass><actualbass>%d</actualbass></bass>`,
		esc(s.DeviceID), s.bassTarget, s.bassActual))
}

// --- POST handlers ---

func (s *Speaker) postName(w http.ResponseWriter, r *http.Request) {
	var n struct {
		Value string `xml:",chardata"`
	}
	if !decode(w, r, &n) {
		return
	}
	s.mu.Lock()
	s.Name = n.Value
	s.mu.Unlock()
	writeXML(w, "<status>/name</status>")
}

func (s *Speaker) postKey(w http.ResponseWriter, r *http.Request) {
	var k struct {
		State string `xml:"state,attr"`
		Value string `xml:",chardata"`
	}
	if !decode(w, r, &k) {
		return
	}
	// The firmware sends press then release; act once, on press.
	if k.State == "press" {
		s.applyKey(strings.TrimSpace(k.Value))
	}
	writeXML(w, "<status>/key</status>")
}

func (s *Speaker) applyKey(key string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch key {
	case "PAUSE":
		s.playStatus = "PAUSE_STATE"
	case "PLAY":
		s.playStatus = "PLAY_STATE"
	case "STOP":
		s.playStatus = "STOP_STATE"
	case "PLAY_PAUSE":
		if s.playStatus == "PLAY_STATE" {
			s.playStatus = "PAUSE_STATE"
		} else {
			s.playStatus = "PLAY_STATE"
		}
	case "POWER":
		// handled like the CLI toggle
		if s.source == "STANDBY" {
			if s.lastSource == "" {
				s.lastSource = "TUNEIN"
			}
			s.source = s.lastSource
		} else {
			s.lastSource = s.source
			s.source = "STANDBY"
		}
	default:
		// PRESET_1..PRESET_6: play the station stored on that slot, like the
		// firmware does when a preset button is pressed.
		if slot, ok := presetSlot(key); ok {
			if p, exists := s.presets[slot]; exists {
				s.source = orDefault(p.Source, "TUNEIN")
				s.lastSource = s.source
				s.location = p.Location
				s.station = p.Name
				s.art = p.Art
				s.track, s.artist = "", ""
				s.playStatus = "PLAY_STATE"
			}
		}
	}
}

// presetSlot parses a "PRESET_<n>" key into its 1..6 slot number.
func presetSlot(key string) (int, bool) {
	rest, ok := strings.CutPrefix(key, "PRESET_")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(rest)
	if err != nil || n < 1 || n > 6 {
		return 0, false
	}
	return n, true
}

func (s *Speaker) postSelect(w http.ResponseWriter, r *http.Request) {
	var ci contentItem
	if !decode(w, r, &ci) {
		return
	}
	s.mu.Lock()
	s.source = orDefault(ci.Source, "TUNEIN")
	s.lastSource = s.source
	s.location = ci.Location
	s.station = ci.Name
	s.art = ci.Art
	s.track, s.artist = "", ""
	s.playStatus = "PLAY_STATE"
	s.mu.Unlock()
	writeXML(w, "<status>/select</status>")
}

func (s *Speaker) postStorePreset(w http.ResponseWriter, r *http.Request) {
	var p struct {
		ID int         `xml:"id,attr"`
		CI contentItem `xml:"ContentItem"`
	}
	if !decode(w, r, &p) {
		return
	}
	if p.ID < 1 || p.ID > 6 {
		http.Error(w, "preset slot out of range", http.StatusBadRequest)
		return
	}
	s.mu.Lock()
	s.presets[p.ID] = Preset{Source: p.CI.Source, Type: p.CI.Type, Location: p.CI.Location, Name: p.CI.Name, Art: p.CI.Art}
	s.mu.Unlock()
	writeXML(w, "<status>/storePreset</status>")
}

func (s *Speaker) postRemovePreset(w http.ResponseWriter, r *http.Request) {
	var p struct {
		ID int `xml:"id,attr"`
	}
	if !decode(w, r, &p) {
		return
	}
	s.mu.Lock()
	delete(s.presets, p.ID)
	s.mu.Unlock()
	writeXML(w, "<status>/removePreset</status>")
}

func (s *Speaker) postMargeAccount(w http.ResponseWriter, r *http.Request) {
	var p struct {
		AccountID string `xml:"accountId"`
	}
	if !decode(w, r, &p) {
		return
	}
	s.mu.Lock()
	s.Account = p.AccountID
	s.mu.Unlock()
	writeXML(w, "<status>/setMargeAccount</status>")
}

func (s *Speaker) postSetZone(w http.ResponseWriter, r *http.Request) {
	z, ok := decodeZone(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	s.zoneMaster = z.Master
	s.zoneSenders = z.Sender
	// getZone lists the master first, then the slaves carried in the body.
	s.zoneMembers = append([]member{{deviceID: z.Master, ip: z.Sender}}, z.members()...)
	s.mu.Unlock()
	writeXML(w, "<status>/setZone</status>")
}

func (s *Speaker) postAddZoneSlave(w http.ResponseWriter, r *http.Request) {
	z, ok := decodeZone(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	if s.zoneMaster == "" {
		// Creating the zone fresh: seed it with the master first, as getZone and
		// setZone both list the master ahead of the slaves.
		s.zoneMaster = z.Master
		s.zoneSenders = z.Sender
		s.zoneMembers = append(s.zoneMembers, member{deviceID: z.Master, ip: z.Sender})
	}
	for _, m := range z.members() {
		if !s.hasMember(m.deviceID) {
			s.zoneMembers = append(s.zoneMembers, m)
		}
	}
	s.mu.Unlock()
	writeXML(w, "<status>/addZoneSlave</status>")
}

func (s *Speaker) postRemoveZoneSlave(w http.ResponseWriter, r *http.Request) {
	z, ok := decodeZone(w, r)
	if !ok {
		return
	}
	s.mu.Lock()
	drop := map[string]bool{}
	for _, m := range z.members() {
		drop[m.deviceID] = true
	}
	kept := s.zoneMembers[:0:0]
	for _, m := range s.zoneMembers {
		if !drop[m.deviceID] {
			kept = append(kept, m)
		}
	}
	s.zoneMembers = kept
	// A zone with only its master left is dissolved, like the firmware does.
	if len(s.zoneMembers) <= 1 {
		s.zoneMaster, s.zoneSenders, s.zoneMembers = "", "", nil
	}
	s.mu.Unlock()
	writeXML(w, "<status>/removeZoneSlave</status>")
}

func (s *Speaker) hasMember(id string) bool {
	for _, m := range s.zoneMembers {
		if m.deviceID == id {
			return true
		}
	}
	return false
}

// --- helpers ---

type contentItem struct {
	Source   string `xml:"source,attr"`
	Type     string `xml:"type,attr"`
	Location string `xml:"location,attr"`
	Name     string `xml:"itemName"`
	Art      string `xml:"containerArt"`
}

type zoneReq struct {
	Master  string `xml:"master,attr"`
	Sender  string `xml:"senderIPAddress,attr"`
	Members []struct {
		IP       string `xml:"ipaddress,attr"`
		DeviceID string `xml:",chardata"`
	} `xml:"member"`
}

func (z zoneReq) members() []member {
	out := make([]member, 0, len(z.Members))
	for _, m := range z.Members {
		out = append(out, member{deviceID: strings.TrimSpace(m.DeviceID), ip: strings.TrimSpace(m.IP)})
	}
	return out
}

func decodeZone(w http.ResponseWriter, r *http.Request) (zoneReq, bool) {
	var z zoneReq
	ok := decode(w, r, &z)
	return z, ok
}

// decode reads the request body and unmarshals it as XML into v. On a malformed body
// it writes a 400 and returns false, like the speaker rejecting a bad request.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := xml.NewDecoder(r.Body).Decode(v); err != nil {
		http.Error(w, "bad request body: "+err.Error(), http.StatusBadRequest)
		return false
	}
	return true
}

func writeXML(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/xml")
	_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8" ?>` + body))
}

func sortedSlots(m map[int]Preset) []int {
	slots := make([]int, 0, len(m))
	for k := range m {
		slots = append(slots, k)
	}
	sort.Ints(slots)
	return slots
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func esc(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;").Replace(s)
}
