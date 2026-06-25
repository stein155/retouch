package homekit

import (
	"context"
	"testing"

	"github.com/brutella/hap"
	"github.com/brutella/hap/accessory"
	"github.com/brutella/hap/characteristic"
	"github.com/brutella/hap/service"

	"github.com/stein155/retouch/internal/speaker"
)

// newTestRadio builds a radio + its accessory without touching a real speaker
// (build does no I/O). The client host is unreachable on purpose.
func newTestRadio(t *testing.T) (*radio, *accessory.A) {
	t.Helper()
	r := &radio{
		bc:        speaker.New("127.0.0.1:0"),
		base:      context.Background(),
		byStation: map[string]int{},
		lastVol:   defaultUnmuteVolume,
	}
	a := r.build("Kitchen", &speaker.Info{Type: "SoundTouch 20", DeviceID: "ABC123", Software: "27"})
	return r, a
}

func findS(a *accessory.A, typ string) *service.S {
	for _, s := range a.Ss {
		if s.Type == typ {
			return s
		}
	}
	return nil
}

// TestServerAccepts runs hap's own server validation, which assigns instance ids
// and rejects duplicates or unnamed accessories. If this passes, the tree is a
// structurally valid HAP accessory database.
func TestServerAccepts(t *testing.T) {
	_, a := newTestRadio(t)
	if _, err := hap.NewServer(hap.NewMemStore(), a); err != nil {
		t.Fatalf("hap rejected the accessory tree: %v", err)
	}
}

func TestIsTelevision(t *testing.T) {
	_, a := newTestRadio(t)
	if a.Type != accessory.TypeTelevision {
		t.Fatalf("accessory category = %d, want Television (%d)", a.Type, accessory.TypeTelevision)
	}
}

// TestTelevisionWired checks the Television service has the controls the Home app
// needs and links to every input plus the speaker — the wiring whose absence makes
// the Home app report "no controls available".
func TestTelevisionWired(t *testing.T) {
	_, a := newTestRadio(t)
	tv := findS(a, service.TypeTelevision)
	if tv == nil {
		t.Fatal("no Television service")
	}
	for _, typ := range []string{
		characteristic.TypeActive,
		characteristic.TypeActiveIdentifier,
		characteristic.TypeConfiguredName,
		characteristic.TypeSleepDiscoveryMode,
	} {
		if tv.C(typ) == nil {
			t.Errorf("Television missing required characteristic %s", typ)
		}
	}
	if !tv.Primary {
		t.Error("Television service should be the primary service")
	}
	// 6 inputs + 1 speaker linked.
	if got := len(tv.Linked); got != numPresets+1 {
		t.Errorf("Television linked services = %d, want %d", got, numPresets+1)
	}
}

func TestSpeakerWired(t *testing.T) {
	_, a := newTestRadio(t)
	spk := findS(a, service.TypeSpeaker)
	if spk == nil {
		t.Fatal("no Speaker service")
	}
	for _, typ := range []string{
		characteristic.TypeActive,
		characteristic.TypeVolume,
		characteristic.TypeVolumeControlType,
		characteristic.TypeMute,
		characteristic.TypeVolumeSelector,
	} {
		if spk.C(typ) == nil {
			t.Errorf("Speaker missing characteristic %s (volume would not work)", typ)
		}
	}
	// Relative is what makes the Home app's Remote volume buttons work for a TV;
	// Absolute leaves the volume uncontrollable (the slider is never shown).
	if v := spk.C(characteristic.TypeVolumeControlType); v != nil {
		if got := v.Value(); got != characteristic.VolumeControlTypeRelative {
			t.Errorf("VolumeControlType = %v, want Relative", got)
		}
	}
}

// TestInputSources checks there is one fully-described input per preset, with the
// slot as its identifier so ActiveIdentifier can select it.
func TestInputSources(t *testing.T) {
	_, a := newTestRadio(t)
	var inputs []*service.S
	for _, s := range a.Ss {
		if s.Type == service.TypeInputSource {
			inputs = append(inputs, s)
		}
	}
	if len(inputs) != numPresets {
		t.Fatalf("input sources = %d, want %d", len(inputs), numPresets)
	}
	ids := map[int]bool{}
	for _, in := range inputs {
		for _, typ := range []string{
			characteristic.TypeIdentifier,
			characteristic.TypeConfiguredName,
			characteristic.TypeName,
			characteristic.TypeInputSourceType,
			characteristic.TypeIsConfigured,
			characteristic.TypeCurrentVisibilityState,
		} {
			if in.C(typ) == nil {
				t.Errorf("input source missing characteristic %s", typ)
			}
		}
		if c := in.C(characteristic.TypeIdentifier); c != nil {
			if id, ok := c.Value().(int); ok {
				ids[id] = true
			}
		}
	}
	for slot := 1; slot <= numPresets; slot++ {
		if !ids[slot] {
			t.Errorf("no input source with identifier %d", slot)
		}
	}
}

// TestHapFirmware checks the firmware string is coerced into the
// major[.minor[.revision]] form HomeKit requires; a non-conforming value makes
// iOS refuse the accessory as "out of compliance".
func TestHapFirmware(t *testing.T) {
	cases := []struct{ in, want string }{
		{"27.0.6.46330.5043500", "27.0.6"},                           // Bose: 5 components -> 3
		{"27.0.6.46330.5043500 epdbuild.trunk.2018-06-21", "27.0.6"}, // + build suffix
		{"27", "27"},
		{"27.0", "27.0"},
		{"1.2.3", "1.2.3"},
		{"07.0.6", "7.0.6"}, // normalise leading zero
		{"", "1.0"},
		{"   ", "1.0"},
		{"v27.0", "1.0"}, // leading non-numeric -> fallback
	}
	for _, c := range cases {
		if got := hapFirmware(c.in); got != c.want {
			t.Errorf("hapFirmware(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestPinForIsStableAndValid(t *testing.T) {
	a := PinFor("", "ABC123")
	b := PinFor("", "ABC123")
	if a != b {
		t.Errorf("PinFor not stable: %q vs %q", a, b)
	}
	if len(a) != 8 {
		t.Errorf("pin %q is not 8 digits", a)
	}
	// A valid 8-digit override is kept verbatim (12345678 would be rejected as a
	// forbidden sequential HomeKit code, so use one HAP accepts).
	if got := PinFor("3142-5369", "ABC123"); got != "31425369" {
		t.Errorf("override pin = %q, want 31425369", got)
	}
}
