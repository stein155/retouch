package homekit

import (
	"context"
	"strconv"
	"strings"
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
		characteristic.TypeCurrentMediaState, // play/pause state
		characteristic.TypeTargetMediaState,  // play/pause control
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

// TestMediaStateMapping checks the speaker's PlayStatus maps onto the right
// HomeKit media state, and that standby reads as stopped.
func TestMediaStateMapping(t *testing.T) {
	r, _ := newTestRadio(t)
	cases := []struct {
		on         bool
		playStatus string
		want       int
	}{
		{true, "PLAY_STATE", characteristic.CurrentMediaStatePlay},
		{true, "BUFFERING_STATE", characteristic.CurrentMediaStatePlay},
		{true, "PAUSE_STATE", characteristic.CurrentMediaStatePause},
		{true, "STOP_STATE", characteristic.CurrentMediaStateStop},
		{true, "", characteristic.CurrentMediaStateStop},
		{false, "PLAY_STATE", characteristic.CurrentMediaStateStop}, // standby wins
	}
	for _, c := range cases {
		r.setMediaState(c.on, c.playStatus)
		if got := r.media.Value(); got != c.want {
			t.Errorf("setMediaState(%v, %q) = %d, want %d", c.on, c.playStatus, got, c.want)
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

func TestSetupIDIsStableAndValid(t *testing.T) {
	a := SetupIDFor("ABC123")
	if a != SetupIDFor("ABC123") {
		t.Errorf("SetupIDFor not stable")
	}
	if len(a) != 4 {
		t.Errorf("setup id %q is not 4 chars", a)
	}
	for _, c := range a {
		if !((c >= '0' && c <= '9') || (c >= 'A' && c <= 'Z')) {
			t.Errorf("setup id %q has invalid char %q", a, c)
		}
	}
	if SetupIDFor("ABC123") == SetupIDFor("XYZ789") {
		t.Errorf("distinct device ids gave the same setup id")
	}
}

// TestSetupURIEncodesPinAndCategory decodes the X-HM payload back to its fields
// and checks the setup code and accessory category round-trip — the Home app
// reads exactly these when the QR is scanned.
func TestSetupURIEncodesPinAndCategory(t *testing.T) {
	const setupID = "ABCD"
	uri := SetupURI("31425369", setupID)
	if !strings.HasPrefix(uri, "X-HM://") {
		t.Fatalf("uri %q missing X-HM prefix", uri)
	}
	body := strings.TrimPrefix(uri, "X-HM://")
	if len(body) != 9+len(setupID) || body[9:] != setupID {
		t.Fatalf("uri %q does not end with setup id %q after 9-char payload", uri, setupID)
	}
	payload, err := strconv.ParseUint(body[:9], 36, 64)
	if err != nil {
		t.Fatalf("payload %q not base-36: %v", body[:9], err)
	}
	if code := payload & 0x7ffffff; code != 31425369 {
		t.Errorf("decoded setup code = %d, want 31425369", code)
	}
	if cat := (payload >> 31) & 0xff; cat != uint64(accessory.TypeTelevision) {
		t.Errorf("decoded category = %d, want %d", cat, accessory.TypeTelevision)
	}
	if flags := (payload >> 27) & 0xf; flags != 2 {
		t.Errorf("decoded flags = %d, want 2 (IP)", flags)
	}
	if SetupURI("bad", setupID) != "" {
		t.Errorf("SetupURI with a non-numeric pin should be empty")
	}
}
