// Package homekit exposes a ReTouch speaker to Apple Home over the HomeKit
// Accessory Protocol (HAP), so the speaker can be controlled from the Home app
// and Siri alongside ReTouch's own web UI.
//
// The speaker is published as a HomeKit bridge with a handful of plain accessories
// that the stock Home app renders as real, tappable tiles:
//
//   - one switch per preset  -> tap to play that preset; the playing preset shows on
//   - a power switch         -> wake the speaker / put it on standby
//   - a "volume" lightbulb   -> brightness 0..100 maps to the speaker volume (the
//     Home app has no speaker-volume slider, so a dimmable
//     light is the idiomatic way to get one; off = mute)
//
// A Television accessory (the obvious media type) was deliberately NOT used: the
// stock Home app shows "no controls available" for it and never renders its preset
// "inputs" as buttons. Switches + a brightness slider are what actually work there.
//
// Everything maps onto the existing speaker control surface (internal/speaker);
// HomeKit issues the same /key and /volume calls the web UI already uses, so the
// speaker plays radio itself exactly as before. ReTouch only stands in as the bridge.
//
// HAP needs SRP pairing, Curve25519/ChaCha20-Poly1305 transport crypto and its own
// mDNS (Bonjour) advertisement — none of which is feasible on the Go stdlib — so
// this is the project's one deliberate dependency (github.com/brutella/hap). See
// AGENTS.md. The HAP server advertises a `_hap._tcp` service of its own; it is
// independent of ReTouch's LAN discovery (internal/discover), which uses no mDNS.
package homekit

import (
	"context"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"log/slog"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"

	"github.com/stein155/retouch/internal/speaker"
)

const (
	// numPresets is the fixed number of native preset buttons a SoundTouch has.
	numPresets = 6
	// pollEvery is how often we read the speaker and mirror its state into HomeKit.
	pollEvery = 3 * time.Second
	// presetsEvery throttles the (rarer) refresh of preset names.
	presetsEvery = 30 * time.Second
	// actionTimeout bounds a single speaker command kicked off by a Home request.
	actionTimeout = 12 * time.Second
	// defaultUnmuteVolume is the level used when the volume light is switched on while
	// the speaker is muted and we have no remembered level.
	defaultUnmuteVolume = 20
)

// Config controls the HomeKit bridge.
type Config struct {
	Pin        string // 8-digit setup code; derived from the device id when empty
	Name       string // bridge name shown in the Home app
	Addr       string // TCP listen address for the HAP server (e.g. ":51827")
	StorageDir string // directory for HAP pairing state (persisted across reboots)
}

// presetSwitch is one preset exposed as a HomeKit switch.
type presetSwitch struct {
	slot int
	on   *characteristic.On
	name *characteristic.Name // the accessory's display name (best-effort live rename)
}

// bridge holds the live accessories and the speaker client they drive.
type bridge struct {
	bc   *speaker.Client
	log  *slog.Logger
	base context.Context

	power   *characteristic.On
	presets []*presetSwitch
	volOn   *characteristic.On
	bright  *characteristic.Brightness

	mu        sync.Mutex
	byStation map[string]int // TuneIn station id -> preset slot
	lastVol   int            // last non-zero volume, for un-muting
}

// Run builds the bridge, starts the HAP server and mirrors speaker state into
// HomeKit until ctx is cancelled. It blocks; run it in its own goroutine.
func Run(ctx context.Context, bc *speaker.Client, info *speaker.Info, cfg Config, logger *slog.Logger) error {
	name := strings.TrimSpace(cfg.Name)
	if name == "" {
		name = strings.TrimSpace(info.Name)
	}
	if name == "" {
		name = "ReTouch"
	}

	pin := PinFor(cfg.Pin, info.DeviceID)
	if cfg.StorageDir != "" {
		if err := os.MkdirAll(cfg.StorageDir, 0o755); err != nil {
			return err
		}
	}

	b := &bridge{bc: bc, log: logger, base: ctx, byStation: map[string]int{}, lastVol: defaultUnmuteVolume}
	root, children := b.build(name, info)

	store := hap.NewFsStore(cfg.StorageDir)
	srv, err := hap.NewServer(store, root, children...)
	if err != nil {
		return err
	}
	srv.Pin = pin
	if cfg.Addr != "" {
		srv.Addr = cfg.Addr
	}

	logger.Info("homekit ready — add the bridge in the Apple Home app",
		"name", name, "code", fmtPin(pin), "addr", cfg.Addr, "paired", srv.IsPaired())

	go b.syncLoop(ctx)

	return srv.ListenAndServe(ctx)
}

// build assembles the bridge accessory and its children: a power switch, one switch
// per preset, and a "volume" lightbulb. Returns the bridge and the child accessories.
func (b *bridge) build(name string, info *speaker.Info) (*accessory.A, []*accessory.A) {
	model := strings.TrimSpace(info.Type)
	if model == "" {
		model = "SoundTouch"
	}
	serial := info.DeviceID
	if serial == "" {
		serial = "retouch"
	}
	fw := strings.TrimSpace(info.Software)
	if fw == "" {
		fw = "1.0"
	}
	mk := func(suffix string) accessory.Info {
		n := name
		if suffix != "" {
			n = name + " " + suffix
		}
		return accessory.Info{Name: n, Manufacturer: "Bose (ReTouch)", Model: model, SerialNumber: serial + suffix, Firmware: fw}
	}

	root := accessory.NewBridge(mk("")).A

	var children []*accessory.A

	// Power: on = playing/awake, off = standby.
	pwr := accessory.NewSwitch(mk("aan/uit"))
	pwr.Switch.On.OnValueRemoteUpdate(func(on bool) { b.setPower(on) })
	b.power = pwr.Switch.On
	children = append(children, pwr.A)

	// One switch per preset. The switch reflects which preset is playing; turning the
	// active preset off pauses, turning another on plays it.
	b.presets = make([]*presetSwitch, 0, numPresets)
	for slot := 1; slot <= numPresets; slot++ {
		sw := accessory.NewSwitch(mk("preset " + strconv.Itoa(slot)))
		s := slot
		on := sw.Switch.On
		on.OnValueRemoteUpdate(func(v bool) {
			if v {
				b.playPreset(s)
			} else {
				b.key("PAUSE")
			}
		})
		b.presets = append(b.presets, &presetSwitch{slot: slot, on: on, name: sw.Info.Name})
		children = append(children, sw.A)
	}

	// Volume as a dimmable light: brightness 0..100 = volume, off = mute.
	vol := accessory.NewLightbulb(mk("volume"))
	bright := characteristic.NewBrightness()
	vol.Lightbulb.AddC(bright.C)
	bright.OnValueRemoteUpdate(func(v int) { b.setVolume(v) })
	vol.Lightbulb.On.OnValueRemoteUpdate(func(on bool) { b.setVolumeOn(on) })
	b.volOn, b.bright = vol.Lightbulb.On, bright
	children = append(children, vol.A)

	return root, children
}

// syncLoop mirrors the speaker's live state into the HomeKit characteristics so the
// Home app reflects what the speaker is actually doing — whoever changed it.
func (b *bridge) syncLoop(ctx context.Context) {
	b.refreshPresetNames(ctx)
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	lastNames := time.Now()

	for {
		b.syncOnce(ctx)
		if time.Since(lastNames) >= presetsEvery {
			b.refreshPresetNames(ctx)
			lastNames = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (b *bridge) syncOnce(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	if np, err := b.bc.NowPlaying(c); err == nil {
		on := np.Source != "" && np.Source != "STANDBY"
		b.power.SetValue(on)
		active := 0
		if on && np.StationID != "" {
			active = b.slotForStation(np.StationID)
		}
		for _, p := range b.presets {
			p.on.SetValue(p.slot == active)
		}
	}
	if vol, err := b.bc.Volume(c); err == nil {
		_ = b.bright.SetValue(vol)
		b.volOn.SetValue(vol > 0)
		if vol > 0 {
			b.mu.Lock()
			b.lastVol = vol
			b.mu.Unlock()
		}
	}
}

// refreshPresetNames names each preset switch after the station on that slot and
// keeps the station -> slot map current (used to light the playing preset).
func (b *bridge) refreshPresetNames(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	presets, err := b.bc.Presets(c)
	if err != nil {
		return
	}
	byslot := make(map[int]speaker.Preset, len(presets))
	byStation := make(map[string]int, len(presets))
	for _, p := range presets {
		byslot[p.Slot] = p
		if p.StationID != "" {
			byStation[p.StationID] = p.Slot
		}
	}
	for _, p := range b.presets {
		if pr, ok := byslot[p.slot]; ok && strings.TrimSpace(pr.Name) != "" {
			p.name.SetValue(strings.TrimSpace(pr.Name))
		}
	}
	b.mu.Lock()
	b.byStation = byStation
	b.mu.Unlock()
}

// slotForStation maps the now-playing station to a preset slot (0 = not a preset).
func (b *bridge) slotForStation(stationID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.byStation[stationID]
}

// --- speaker actions (run async so HAP request handlers stay responsive) ---

func (b *bridge) do(fn func(ctx context.Context)) {
	go func() {
		ctx, cancel := context.WithTimeout(b.base, actionTimeout)
		defer cancel()
		fn(ctx)
	}()
}

func (b *bridge) key(name string) {
	b.do(func(ctx context.Context) {
		if err := b.bc.Key(ctx, name); err != nil {
			b.log.Warn("homekit key", "key", name, "err", err)
		}
	})
}

func (b *bridge) setVolume(level int) {
	b.do(func(ctx context.Context) {
		if err := b.bc.SetVolume(ctx, level); err != nil {
			b.log.Warn("homekit volume", "level", level, "err", err)
		}
	})
}

// setVolumeOn handles the volume light's on/off: off mutes (volume 0), on restores the
// current brightness, or the last non-zero volume when brightness is 0.
func (b *bridge) setVolumeOn(on bool) {
	if !on {
		b.setVolume(0)
		return
	}
	level := b.bright.Value()
	if level <= 0 {
		b.mu.Lock()
		level = b.lastVol
		b.mu.Unlock()
		if level <= 0 {
			level = defaultUnmuteVolume
		}
	}
	b.setVolume(level)
}

func (b *bridge) playPreset(slot int) {
	if slot < 1 || slot > numPresets {
		return
	}
	b.do(func(ctx context.Context) {
		b.bc.Wake(ctx)
		if err := b.bc.Key(ctx, "PRESET_"+strconv.Itoa(slot)); err != nil {
			b.log.Warn("homekit preset", "slot", slot, "err", err)
		}
	})
}

// setPower wakes the speaker (on) or puts it on standby (off). POWER toggles, so we
// only send it when the current state actually needs to change.
func (b *bridge) setPower(on bool) {
	b.do(func(ctx context.Context) {
		if on {
			b.bc.Wake(ctx)
			return
		}
		if np, err := b.bc.NowPlaying(ctx); err == nil && np.Source != "STANDBY" {
			if err := b.bc.Key(ctx, "POWER"); err != nil {
				b.log.Warn("homekit standby", "err", err)
			}
		}
	})
}

// PinFor returns the 8-digit HAP setup code: the override when valid, otherwise a
// stable code derived from the device id (so it survives restarts without storage).
func PinFor(override, deviceID string) string {
	digits := keepDigits(override)
	if len(digits) == 8 && !hap.InvalidPins[digits] {
		return digits
	}
	seed := deviceID
	if seed == "" {
		seed = "retouch"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	// Map into 10_000_000..89_999_999 so it is always 8 digits and never all-equal.
	code := strconv.Itoa(int(h.Sum32()%80_000_000 + 10_000_000))
	if hap.InvalidPins[code] {
		code = "31425369"
	}
	return code
}

// FmtPin renders an 8-digit code as the XXX-XX-XXX form shown in the Home app.
func FmtPin(pin string) string { return fmtPin(pin) }

func fmtPin(pin string) string {
	if len(pin) != 8 {
		return pin
	}
	return pin[:3] + "-" + pin[3:5] + "-" + pin[5:]
}

func keepDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}
