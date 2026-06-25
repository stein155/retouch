// Package homekit exposes a ReTouch speaker to Apple Home over the HomeKit
// Accessory Protocol (HAP), so the speaker can be controlled from the Home app
// and Siri alongside ReTouch's own web UI.
//
// The speaker is published as a single HomeKit **Television** accessory — the
// media type the Home app renders as a real radio you can turn on and off, with
// a volume control and a list of sources:
//
//   - Television service   -> on/off (Active) and the current station (ActiveIdentifier,
//     which the Home app shows as "now playing")
//   - a linked Speaker     -> volume slider + mute (and +/- for the remote's buttons)
//   - one linked InputSource per preset -> the six presets/stations; selecting one
//     plays it, and the playing one is shown as the active source
//
// An earlier version modelled this as a bridge of plain switches plus a
// "volume" lightbulb. That is valid HAP, but it does not read as a radio and the
// Home app could not present it as one. A Television accessory is the idiomatic
// fit — provided it is wired completely: the volume characteristics live on a
// linked Speaker service and every preset is a linked InputSource. Omitting
// either is what makes the Home app say "no controls available"; both are wired
// here.
//
// Everything maps onto the existing speaker control surface (internal/speaker);
// HomeKit issues the same /key and /volume calls the web UI already uses, so the
// speaker plays radio itself exactly as before. ReTouch only stands in as the
// HomeKit accessory.
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
	"github.com/brutella/hap/service"

	"github.com/stein155/retouch/internal/speaker"
)

const (
	// numPresets is the fixed number of native preset buttons a SoundTouch has.
	numPresets = 6
	// pollEvery is how often we read the speaker and mirror its state into HomeKit.
	pollEvery = 3 * time.Second
	// presetsEvery throttles the (rarer) refresh of preset/station names.
	presetsEvery = 30 * time.Second
	// actionTimeout bounds a single speaker command kicked off by a Home request.
	actionTimeout = 12 * time.Second
	// defaultUnmuteVolume is the level used when unmuting with no remembered level.
	defaultUnmuteVolume = 20
	// volumeStep is how much the remote's +/- buttons (relative volume) move by.
	volumeStep = 5
)

// Config controls the HomeKit accessory.
type Config struct {
	Pin        string // 8-digit setup code; derived from the device id when empty
	Name       string // accessory name shown in the Home app
	Addr       string // TCP listen address for the HAP server (e.g. ":51827")
	StorageDir string // directory for HAP pairing state (persisted across reboots)
}

// presetInput is one preset exposed as a HomeKit TV input source. Its identifier
// is the preset slot, so the Television's ActiveIdentifier doubles as "which
// preset is playing".
type presetInput struct {
	slot int
	name *characteristic.ConfiguredName // user-facing label (the station name)
	disp *characteristic.Name           // accessory-defined name (kept in sync too)
}

// radio holds the live characteristics of the Television accessory and the speaker
// client they drive.
type radio struct {
	bc   *speaker.Client
	log  *slog.Logger
	base context.Context

	power  *characteristic.Active           // TV on/off
	source *characteristic.ActiveIdentifier // playing preset slot (0 = none)
	volume *characteristic.Volume
	mute   *characteristic.Mute
	inputs []*presetInput

	mu        sync.Mutex
	byStation map[string]int // TuneIn station id -> preset slot
	lastVol   int            // last non-zero volume, for un-muting
}

// Run builds the Television accessory, starts the HAP server and mirrors speaker
// state into HomeKit until ctx is cancelled. It blocks; run it in its own goroutine.
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

	r := &radio{bc: bc, log: logger, base: ctx, byStation: map[string]int{}, lastVol: defaultUnmuteVolume}
	a := r.build(name, info)

	store := hap.NewFsStore(cfg.StorageDir)
	srv, err := hap.NewServer(store, a) // a single, non-bridged Television accessory
	if err != nil {
		return err
	}
	srv.Pin = pin
	if cfg.Addr != "" {
		srv.Addr = cfg.Addr
	}

	logger.Info("homekit ready — add the accessory in the Apple Home app",
		"name", name, "code", fmtPin(pin), "addr", cfg.Addr, "paired", srv.IsPaired())

	go r.syncLoop(ctx)

	return srv.ListenAndServe(ctx)
}

// build assembles the Television accessory: the Television service (on/off + current
// source), a linked Speaker service (volume + mute), and one linked InputSource per
// preset. Returns the accessory ready to publish.
func (r *radio) build(name string, info *speaker.Info) *accessory.A {
	model := strings.TrimSpace(info.Type)
	if model == "" {
		model = "SoundTouch"
	}
	serial := info.DeviceID
	if serial == "" {
		serial = "retouch"
	}
	fw := hapFirmware(info.Software)

	a := accessory.New(accessory.Info{
		Name:         name,
		Manufacturer: "Bose (ReTouch)",
		Model:        model,
		SerialNumber: serial,
		Firmware:     fw,
	}, accessory.TypeTelevision)

	// The Television itself: power + the active source.
	tv := service.NewTelevision()
	tv.S.Primary = true
	tv.ConfiguredName.SetValue(name)
	tv.SleepDiscoveryMode.SetValue(characteristic.SleepDiscoveryModeAlwaysDiscoverable)
	tv.Active.OnValueRemoteUpdate(func(v int) { r.setPower(v == characteristic.ActiveActive) })
	tv.ActiveIdentifier.OnValueRemoteUpdate(func(id int) { r.selectInput(id) })
	r.power = tv.Active
	r.source = tv.ActiveIdentifier
	a.AddS(tv.S)

	// One input source per preset, each linked to the Television so the Home app
	// lists it as a selectable source. The identifier is the preset slot.
	r.inputs = make([]*presetInput, 0, numPresets)
	for slot := 1; slot <= numPresets; slot++ {
		in := service.NewInputSource()

		id := characteristic.NewIdentifier() // required so ActiveIdentifier can point here
		id.SetValue(slot)
		in.AddC(id.C)

		disp := characteristic.NewName()
		label := "Preset " + strconv.Itoa(slot)
		disp.SetValue(label)
		in.AddC(disp.C)

		in.ConfiguredName.SetValue(label)
		in.InputSourceType.SetValue(characteristic.InputSourceTypeTuner) // a radio tuner
		in.IsConfigured.SetValue(characteristic.IsConfiguredConfigured)
		in.CurrentVisibilityState.SetValue(characteristic.CurrentVisibilityStateShown)

		a.AddS(in.S)
		tv.AddS(in.S) // link the source to the Television

		r.inputs = append(r.inputs, &presetInput{slot: slot, name: in.ConfiguredName, disp: disp})
	}

	// A Speaker service carrying the volume controls, linked to the Television. The
	// stock NewSpeaker() only has Mute, so we add Active + a Volume characteristic +
	// a relative VolumeSelector (the remote's hardware +/- buttons).
	spk := service.New(service.TypeSpeaker)

	spkActive := characteristic.NewActive()
	spkActive.SetValue(characteristic.ActiveActive)
	spk.AddC(spkActive.C)

	spkName := characteristic.NewName()
	spkName.SetValue(name + " volume")
	spk.AddC(spkName.C)

	// Relative, not Absolute: for a HomeKit Television the Home app never shows an
	// absolute volume slider — volume is changed from the Remote widget, whose +/-
	// (the iPhone's hardware volume buttons) send relative VolumeSelector events. With
	// Absolute, iOS suppresses those buttons and nothing controls the volume. We still
	// serve the absolute Volume characteristic below, but only to report the level.
	vct := characteristic.NewVolumeControlType()
	vct.SetValue(characteristic.VolumeControlTypeRelative)
	spk.AddC(vct.C)

	mute := characteristic.NewMute()
	mute.OnValueRemoteUpdate(func(on bool) { r.setMute(on) })
	spk.AddC(mute.C)

	vol := characteristic.NewVolume()
	vol.OnValueRemoteUpdate(func(v int) { r.setVolume(v) })
	spk.AddC(vol.C)

	sel := characteristic.NewVolumeSelector()
	sel.OnValueRemoteUpdate(func(dir int) { r.nudgeVolume(dir) })
	spk.AddC(sel.C)

	r.volume, r.mute = vol, mute
	a.AddS(spk)
	tv.AddS(spk) // link the speaker to the Television

	return a
}

// syncLoop mirrors the speaker's live state into the HomeKit characteristics so the
// Home app reflects what the speaker is actually doing — whoever changed it.
func (r *radio) syncLoop(ctx context.Context) {
	r.refreshPresetNames(ctx)
	tick := time.NewTicker(pollEvery)
	defer tick.Stop()
	lastNames := time.Now()

	for {
		r.syncOnce(ctx)
		if time.Since(lastNames) >= presetsEvery {
			r.refreshPresetNames(ctx)
			lastNames = time.Now()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

func (r *radio) syncOnce(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	if np, err := r.bc.NowPlaying(c); err == nil {
		on := np.Source != "" && np.Source != "STANDBY"
		if on {
			r.power.SetValue(characteristic.ActiveActive)
		} else {
			r.power.SetValue(characteristic.ActiveInactive)
		}
		if on && np.StationID != "" {
			if slot := r.slotForStation(np.StationID); slot != 0 {
				_ = r.source.SetValue(slot)
			}
		}
	}
	if vol, err := r.bc.Volume(c); err == nil {
		_ = r.volume.SetValue(vol)
		r.mute.SetValue(vol == 0)
		if vol > 0 {
			r.mu.Lock()
			r.lastVol = vol
			r.mu.Unlock()
		}
	}
}

// refreshPresetNames names each input source after the station on that slot and
// keeps the station -> slot map current (used to show the playing source).
func (r *radio) refreshPresetNames(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	presets, err := r.bc.Presets(c)
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
	for _, in := range r.inputs {
		if pr, ok := byslot[in.slot]; ok && strings.TrimSpace(pr.Name) != "" {
			name := strings.TrimSpace(pr.Name)
			in.name.SetValue(name)
			in.disp.SetValue(name)
		}
	}
	r.mu.Lock()
	r.byStation = byStation
	r.mu.Unlock()
}

// slotForStation maps the now-playing station to a preset slot (0 = not a preset).
func (r *radio) slotForStation(stationID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.byStation[stationID]
}

// --- speaker actions (run async so HAP request handlers stay responsive) ---

func (r *radio) do(fn func(ctx context.Context)) {
	go func() {
		ctx, cancel := context.WithTimeout(r.base, actionTimeout)
		defer cancel()
		fn(ctx)
	}()
}

func (r *radio) setVolume(level int) {
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	r.do(func(ctx context.Context) {
		if err := r.bc.SetVolume(ctx, level); err != nil {
			r.log.Warn("homekit volume", "level", level, "err", err)
		}
	})
}

// nudgeVolume moves the volume by volumeStep in the requested direction. Used by the
// Home remote's hardware +/- buttons, which send a relative VolumeSelector. We step
// from the cached characteristic value (no per-press network read) and update it
// optimistically under r.mu, so rapid presses accumulate instead of racing; the next
// syncOnce reconciles with the speaker's real level.
func (r *radio) nudgeVolume(dir int) {
	r.mu.Lock()
	cur := r.volume.Value()
	next := cur + volumeStep
	if dir == characteristic.VolumeSelectorDecrement {
		next = cur - volumeStep
	}
	if next < 0 {
		next = 0
	}
	if next > 100 {
		next = 100
	}
	_ = r.volume.SetValue(next)
	r.mu.Unlock()

	r.do(func(ctx context.Context) {
		if err := r.bc.SetVolume(ctx, next); err != nil {
			r.log.Warn("homekit volume", "level", next, "err", err)
		}
	})
}

// setMute silences the speaker (remembering the level to restore) or unmutes back to
// the last non-zero level.
func (r *radio) setMute(on bool) {
	if on {
		r.do(func(ctx context.Context) {
			if cur, err := r.bc.Volume(ctx); err == nil && cur > 0 {
				r.mu.Lock()
				r.lastVol = cur
				r.mu.Unlock()
			}
			if err := r.bc.SetVolume(ctx, 0); err != nil {
				r.log.Warn("homekit mute", "err", err)
			}
		})
		return
	}
	r.mu.Lock()
	level := r.lastVol
	r.mu.Unlock()
	if level <= 0 {
		level = defaultUnmuteVolume
	}
	r.setVolume(level)
}

// selectInput plays the preset whose slot matches the chosen input identifier.
func (r *radio) selectInput(id int) {
	if id < 1 || id > numPresets {
		return
	}
	r.do(func(ctx context.Context) {
		r.bc.Wake(ctx)
		if err := r.bc.Key(ctx, "PRESET_"+strconv.Itoa(id)); err != nil {
			r.log.Warn("homekit preset", "slot", id, "err", err)
		}
	})
}

// setPower wakes the speaker (on) or puts it on standby (off). POWER toggles, so we
// only send it when the current state actually needs to change.
func (r *radio) setPower(on bool) {
	r.do(func(ctx context.Context) {
		if on {
			r.bc.Wake(ctx)
			return
		}
		if np, err := r.bc.NowPlaying(ctx); err == nil && np.Source != "STANDBY" {
			if err := r.bc.Key(ctx, "POWER"); err != nil {
				r.log.Warn("homekit standby", "err", err)
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

// hapFirmware coerces a speaker firmware string into the FirmwareRevision format
// HomeKit requires: major[.minor[.revision]], each a non-negative integer. Bose
// reports something like "27.0.6.46330.5043500 epdbuild.trunk…" — five numeric
// components plus a build suffix — which iOS rejects as "out of compliance",
// blocking pairing. We keep only the leading numeric, dot-separated components and
// cap them at three. Falls back to "1.0" when nothing usable remains.
func hapFirmware(s string) string {
	fields := strings.Fields(strings.TrimSpace(s)) // drop any build suffix after a space
	if len(fields) == 0 {
		return "1.0"
	}
	parts := make([]string, 0, 3)
	for _, p := range strings.Split(fields[0], ".") {
		if p == "" || keepDigits(p) != p { // stop at the first non-numeric component
			break
		}
		parts = append(parts, strconv.Itoa(mustAtoi(p))) // normalise "07" -> "7"
		if len(parts) == 3 {
			break
		}
	}
	if len(parts) == 0 {
		return "1.0"
	}
	return strings.Join(parts, ".")
}

// mustAtoi parses an all-digit string; callers guarantee the input is numeric.
func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
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
