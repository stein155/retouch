package marge

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

// presetSlot is one of the speaker's six native TUNEIN preset buttons.
type presetSlot struct {
	Button   int    `json:"button"`
	Name     string `json:"name"`
	Location string `json:"location"` // e.g. /v1/playback/station/s6712
	Type     string `json:"type"`     // contentItemType, e.g. stationurl
	Art      string `json:"art,omitempty"`
}

// PresetSeed is one native preset read from the speaker at startup.
type PresetSeed struct {
	Slot     int
	Name     string
	Location string
	Logo     string
}

// presets holds the six preset buttons. It is seeded from the embedded capture and
// then becomes the source of truth: the speaker's long-press stores are written through
// here and persisted, so presets/all reflects edits across restarts.
type presets struct {
	mu    sync.Mutex
	slots map[int]presetSlot
	path  string // JSON persistence file; "" disables persistence
	seq   int    // bumped on every change; drives the ETag
}

// newPresets seeds slots from the embedded (already token-substituted) presets/all
// body, then overlays any persisted edits from path.
func newPresets(seed []byte, path string) *presets {
	p := &presets{slots: map[int]presetSlot{}, path: path}
	p.seedFrom(seed)
	p.loadOverlay()
	return p
}

func (p *presets) seedNative(ps []PresetSeed) {
	if len(ps) == 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, sp := range ps {
		if sp.Slot < 1 || sp.Slot > 6 || sp.Location == "" {
			continue
		}
		p.slots[sp.Slot] = presetSlot{
			Button:   sp.Slot,
			Name:     sp.Name,
			Location: sp.Location,
			Type:     "stationurl",
			Art:      sp.Logo,
		}
	}
	p.seq++
	p.persistLocked()
}

func (p *presets) seedFrom(body []byte) {
	var doc struct {
		Presets []struct {
			Button   int    `xml:"buttonNumber,attr"`
			Name     string `xml:"name"`
			Location string `xml:"location"`
			Type     string `xml:"contentItemType"`
			Art      string `xml:"containerArt"`
		} `xml:"preset"`
	}
	if err := xml.Unmarshal(body, &doc); err != nil {
		return
	}
	for _, s := range doc.Presets {
		if s.Button >= 1 && s.Button <= 6 {
			p.slots[s.Button] = presetSlot{Button: s.Button, Name: s.Name, Location: s.Location, Type: s.Type, Art: s.Art}
		}
	}
}

func (p *presets) loadOverlay() {
	if p.path == "" {
		return
	}
	data, err := os.ReadFile(p.path)
	if err != nil {
		return
	}
	var list []presetSlot
	if json.Unmarshal(data, &list) != nil {
		return
	}
	for _, s := range list {
		if s.Button >= 1 && s.Button <= 6 {
			p.slots[s.Button] = s
		}
	}
}

func (p *presets) persistLocked() {
	if p.path == "" {
		return
	}
	list := make([]presetSlot, 0, len(p.slots))
	for _, s := range p.slots {
		list = append(list, s)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Button < list[j].Button })
	if data, err := json.MarshalIndent(list, "", "  "); err == nil {
		tmp := p.path + ".tmp"
		if os.WriteFile(tmp, data, 0o644) == nil {
			_ = os.Rename(tmp, p.path)
		}
	}
}

// set writes a button through and persists. button must be 1..6.
func (p *presets) set(button int, s presetSlot) {
	if button < 1 || button > 6 {
		return
	}
	if s.Type == "" {
		s.Type = "stationurl"
	}
	s.Button = button
	p.mu.Lock()
	p.slots[button] = s
	p.seq++
	p.persistLocked()
	p.mu.Unlock()
}

func (p *presets) remove(button int) {
	if button < 1 || button > 6 {
		return
	}
	p.mu.Lock()
	delete(p.slots, button)
	p.seq++
	p.persistLocked()
	p.mu.Unlock()
}

// render serialises the six buttons in the firmware's presets/all format. The
// TUNEIN <source> block is constant (the speaker plays the location itself).
func (p *presets) render() ([]byte, string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := []byte(p.renderLocked(true))
	return out, fmt.Sprintf("p%d", p.seq)
}

func (p *presets) renderInner() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.renderLocked(false)
}

func (p *presets) renderLocked(root bool) string {
	ts := time.Now().UTC().Format("2006-01-02T15:04:05.000+00:00")
	var b strings.Builder
	if root {
		b.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n<presets>\n")
	}
	for btn := 1; btn <= 6; btn++ {
		s, ok := p.slots[btn]
		if !ok {
			continue
		}
		fmt.Fprintf(&b, presetTmpl, btn, xmlText(s.Art), xmlText(orDefault(s.Type, "stationurl")),
			ts, xmlText(s.Location), xmlText(s.Name), ts, xmlText(s.Name))
	}
	if root {
		b.WriteString("</presets>")
	}
	return b.String()
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// presetTmpl mirrors the captured per-preset block. Args (in order): button, art,
// contentItemType, createdOn, location, name, updatedOn, username(=name). The
// nested <source> is the constant TUNEIN descriptor (sourceproviderid 25, the
// anonymous tunein token) the speaker resolves against the live TuneIn service.
const presetTmpl = `  <preset buttonNumber="%d">
    <containerArt>%s</containerArt>
    <contentItemType>%s</contentItemType>
    <createdOn>%s</createdOn>
    <location>%s</location>
    <name>%s</name>
    <source id="10004" type="Audio">
      <createdOn>2017-07-20T16:43:48.000+00:00</createdOn>
      <credential type="token">eyJzZXJpYWwiOiJ0dW5laW4ifQ==</credential>
      <name></name>
      <sourceproviderid>25</sourceproviderid>
      <sourcename></sourcename>
      <sourceSettings></sourceSettings>
      <updatedOn>2017-07-20T16:43:48.000+00:00</updatedOn>
      <username></username>
    </source>
    <updatedOn>%s</updatedOn>
    <username>%s</username>
  </preset>
`
