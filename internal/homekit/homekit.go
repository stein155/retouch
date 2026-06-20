// Package homekit exposes a ReTouch speaker to Apple Home over the HomeKit
// Accessory Protocol (HAP), so the speaker can be controlled from the Home app
// and Siri alongside ReTouch's own web UI.
//
// The speaker is modelled as a HomeKit Television accessory — the canonical HAP
// pattern for a media device:
//
//   - Active (power)      -> wake the speaker / put it on standby
//   - ActiveIdentifier    -> the six native presets, shown as selectable inputs
//   - a linked Speaker    -> volume (absolute + up/down) and mute
//   - RemoteKey           -> play/pause
//
// Everything maps onto the existing speaker control surface (internal/speaker);
// HomeKit issues the same /key, /select and /volume calls the web UI already uses,
// so the speaker plays radio itself exactly as before. ReTouch only stands in as
// the HomeKit bridge.
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
	// presetsEvery throttles the (rarer) refresh of preset names.
	presetsEvery = 30 * time.Second
	// actionTimeout bounds a single speaker command kicked off by a Home request.
	actionTimeout = 12 * time.Second
)

// Config controls the HomeKit bridge.
type Config struct {
	Pin        string // 8-digit setup code; derived from the device id when empty
	Name       string // accessory name shown in the Home app
	Addr       string // TCP listen address for the HAP server (e.g. ":51827")
	StorageDir string // directory for HAP pairing state (persisted across reboots)
}

// bridge holds the live accessory and the speaker client it drives.
type bridge struct {
	bc   *speaker.Client
	log  *slog.Logger
	base context.Context

	tv      *service.Television
	inputs  []*inputSource
	volume  *characteristic.Volume
	vctype  *characteristic.VolumeControlType
	vsel    *characteristic.VolumeSelector
	spkActv *characteristic.Active
	mute    *characteristic.Mute

	mu        sync.Mutex     // guards byStation
	byStation map[string]int // TuneIn station id -> preset slot, refreshed periodically
}

// inputSource is one preset exposed as a HomeKit TV input.
type inputSource struct {
	svc  *service.InputSource
	id   *characteristic.Identifier
	name *characteristic.Name
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

	b := &bridge{bc: bc, log: logger, base: ctx, byStation: map[string]int{}}
	a := b.buildAccessory(name, info)

	store := hap.NewFsStore(cfg.StorageDir)
	srv, err := hap.NewServer(store, a)
	if err != nil {
		return err
	}
	srv.Pin = pin
	if cfg.Addr != "" {
		srv.Addr = cfg.Addr
	}

	logger.Info("homekit ready — add the accessory in the Apple Home app",
		"name", name, "code", fmtPin(pin), "addr", cfg.Addr, "paired", srv.IsPaired())

	go b.syncLoop(ctx)

	return srv.ListenAndServe(ctx)
}

// buildAccessory assembles the Television accessory: the TV service, a linked
// Speaker service for volume/mute, and one linked InputSource per preset.
func (b *bridge) buildAccessory(name string, info *speaker.Info) *accessory.A {
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

	a := accessory.New(accessory.Info{
		Name:         name,
		Manufacturer: "Bose (ReTouch)",
		Model:        model,
		SerialNumber: serial,
		Firmware:     fw,
	}, accessory.TypeTelevision)

	// --- Television service: power + input selection ---
	tv := service.NewTelevision()
	tv.ConfiguredName.SetValue(name)
	tv.SleepDiscoveryMode.SetValue(characteristic.SleepDiscoveryModeAlwaysDiscoverable)
	_ = tv.Active.SetValue(characteristic.ActiveInactive)

	// Play/pause from the Home app remote.
	remoteKey := characteristic.NewRemoteKey()
	tv.AddC(remoteKey.C)
	remoteKey.OnValueRemoteUpdate(func(v int) {
		switch v {
		case characteristic.RemoteKeyPlayPause, characteristic.RemoteKeySelect:
			b.key("PLAY_PAUSE")
		}
	})

	tv.Active.OnValueRemoteUpdate(func(v int) {
		b.setPower(v == characteristic.ActiveActive)
	})
	tv.ActiveIdentifier.OnValueRemoteUpdate(func(v int) {
		b.playPreset(v)
	})
	a.AddS(tv.S)
	b.tv = tv

	// --- Speaker service (linked to the TV): volume + mute ---
	spk := service.NewSpeaker()

	vct := characteristic.NewVolumeControlType()
	_ = vct.SetValue(characteristic.VolumeControlTypeAbsolute)
	spk.AddC(vct.C)

	active := characteristic.NewActive()
	_ = active.SetValue(characteristic.ActiveActive)
	spk.AddC(active.C)

	vol := characteristic.NewVolume()
	spk.AddC(vol.C)
	vol.OnValueRemoteUpdate(func(v int) { b.setVolume(v) })

	vsel := characteristic.NewVolumeSelector()
	spk.AddC(vsel.C)
	vsel.OnValueRemoteUpdate(func(v int) {
		if v == characteristic.VolumeSelectorIncrement {
			b.key("VOLUME_UP")
		} else {
			b.key("VOLUME_DOWN")
		}
	})

	spk.Mute.OnValueRemoteUpdate(func(bool) { b.key("MUTE") })

	a.AddS(spk.S)
	tv.AddS(spk.S) // link so Home treats it as the television's speaker

	b.volume, b.vctype, b.vsel, b.spkActv, b.mute = vol, vct, vsel, active, spk.Mute

	// --- Input sources: one per preset slot ---
	b.inputs = make([]*inputSource, 0, numPresets)
	for slot := 1; slot <= numPresets; slot++ {
		in := service.NewInputSource()
		label := "Preset " + strconv.Itoa(slot)

		id := characteristic.NewIdentifier()
		_ = id.SetValue(slot)
		in.AddC(id.C)

		nm := characteristic.NewName()
		nm.SetValue(label)
		in.AddC(nm.C)

		in.ConfiguredName.SetValue(label)
		_ = in.InputSourceType.SetValue(characteristic.InputSourceTypeTuner)
		_ = in.IsConfigured.SetValue(characteristic.IsConfiguredConfigured)
		_ = in.CurrentVisibilityState.SetValue(characteristic.CurrentVisibilityStateShown)

		a.AddS(in.S)
		tv.AddS(in.S) // link as a selectable input
		b.inputs = append(b.inputs, &inputSource{svc: in, id: id, name: nm})
	}
	_ = b.tv.ActiveIdentifier.SetValue(1)

	return a
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
		b.setActive(on)
		if on && np.StationID != "" {
			if slot := b.slotForStation(np.StationID); slot > 0 {
				_ = b.tv.ActiveIdentifier.SetValue(slot)
			}
		}
	}
	if vol, err := b.bc.Volume(c); err == nil {
		_ = b.volume.SetValue(vol)
	}
}

func (b *bridge) setActive(on bool) {
	v := characteristic.ActiveInactive
	if on {
		v = characteristic.ActiveActive
	}
	_ = b.tv.Active.SetValue(v)
	_ = b.spkActv.SetValue(v)
}

// refreshPresetNames names each input after the preset on that slot (or "Preset N"
// when the slot is empty), so the Home app shows the real stations.
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
	for i, in := range b.inputs {
		slot := i + 1
		label := "Preset " + strconv.Itoa(slot)
		if p, ok := byslot[slot]; ok && strings.TrimSpace(p.Name) != "" {
			label = strings.TrimSpace(p.Name)
		}
		in.name.SetValue(label)
		in.svc.ConfiguredName.SetValue(label)
	}
	b.mu.Lock()
	b.byStation = byStation
	b.mu.Unlock()
}

// slotForStation maps the now-playing station to a preset slot using the cache
// refreshed by refreshPresetNames (0 = not one of our presets).
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
