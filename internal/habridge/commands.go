package habridge

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/stein155/retouch/internal/mqtt"
	"github.com/stein155/retouch/internal/speaker"
)

// refresh reads the speaker's current state and publishes any values that changed
// since the last poll (retained, so Home Assistant sees them on reconnect). The
// published map is the per-connection memory of the last value sent per topic.
func (b *Bridge) refresh(ctx context.Context, client *mqtt.Client, tp topics, published map[string]string) {
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()

	send := func(topic, value string) {
		if published[topic] == value {
			return
		}
		if err := client.Publish(topic, []byte(value), true); err == nil {
			published[topic] = value
		}
	}

	if vol, err := b.sp.Volume(c); err == nil {
		send(tp.volumeState(), itoa(vol))
	}

	np, err := b.sp.NowPlaying(c)
	if err != nil {
		return
	}
	send(tp.powerState(), powerState(np.Source))
	send(tp.station(), np.Station)
	send(tp.track(), np.Track)
	send(tp.artist(), np.Artist)
	send(tp.status(), prettyStatus(np.PlayStatus))
	// The preset select reflects the current station, so it shows as "selected"
	// in HA when the speaker is playing one of the preset stations.
	send(tp.presetState(), np.Station)
}

// handle routes one inbound command message to the speaker. It runs on its own
// goroutine (see serve), so speaker calls here may block briefly.
func (b *Bridge) handle(ctx context.Context, client *mqtt.Client, tp topics, disc string, info *speaker.Info, topic, payload string) {
	c, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	switch {
	case topic == tp.volumeSet():
		level, err := strconv.Atoi(strings.TrimSpace(payload))
		if err != nil {
			b.log.Warn("mqtt bad volume", "payload", payload)
			return
		}
		if err := b.sp.SetVolume(c, level); err != nil {
			b.log.Warn("mqtt set volume", "err", err)
			return
		}
		_ = client.Publish(tp.volumeState(), []byte(itoa(clampVol(level))), true)

	case topic == tp.powerSet():
		b.setPower(c, client, tp, strings.EqualFold(strings.TrimSpace(payload), "ON"))

	case topic == tp.presetSet():
		b.selectPreset(c, client, tp, strings.TrimSpace(payload))

	case topic == tp.otaSet():
		if b.update == nil {
			return
		}
		if err := b.update(ctx); err != nil {
			b.log.Warn("mqtt ota trigger", "err", err)
		}

	case topic == disc+"/status":
		// Home Assistant restarted; re-publish discovery and refresh state so the
		// device reappears without waiting for the next reconnect.
		if strings.EqualFold(strings.TrimSpace(payload), availabilityOnline) {
			b.announce(ctx, client, tp, disc, info)
			b.refresh(ctx, client, tp, map[string]string{})
		}

	default:
		// Transport buttons: <base>/transport/<key>/set.
		for _, tb := range transportButtons {
			if topic == tp.transportSet(tb.key) {
				if err := b.sp.Key(c, tb.speakerKey); err != nil {
					b.log.Warn("mqtt transport", "key", tb.speakerKey, "err", err)
				}
				return
			}
		}
	}
}

// setPower turns the speaker on (wake from standby) or off (into standby), only
// acting when the current state differs so the switch is idempotent.
func (b *Bridge) setPower(ctx context.Context, client *mqtt.Client, tp topics, on bool) {
	np, err := b.sp.NowPlaying(ctx)
	if err != nil {
		b.log.Warn("mqtt power: read state", "err", err)
		return
	}
	standby := isStandby(np.Source)
	switch {
	case on && standby:
		b.sp.Wake(ctx)
	case !on && !standby:
		if err := b.sp.Key(ctx, "POWER"); err != nil {
			b.log.Warn("mqtt power off", "err", err)
			return
		}
	}
	_ = client.Publish(tp.powerState(), []byte(boolOnOff(on)), true)
}

// selectPreset plays the native preset whose name matches the chosen option.
func (b *Bridge) selectPreset(ctx context.Context, client *mqtt.Client, tp topics, name string) {
	if name == "" || name == presetPlaceholder {
		return
	}
	presets, err := b.sp.Presets(ctx)
	if err != nil {
		b.log.Warn("mqtt preset: read presets", "err", err)
		return
	}
	for _, p := range presets {
		if p.Name == name {
			b.sp.Wake(ctx)
			if err := b.sp.Key(ctx, "PRESET_"+strconv.Itoa(p.Slot)); err != nil {
				b.log.Warn("mqtt preset play", "slot", p.Slot, "err", err)
				return
			}
			_ = client.Publish(tp.presetState(), []byte(name), true)
			return
		}
	}
	b.log.Warn("mqtt preset: no match", "name", name)
}

// isStandby reports whether the now-playing source means the speaker is off.
func isStandby(source string) bool {
	switch strings.ToUpper(strings.TrimSpace(source)) {
	case "", "STANDBY", "INVALID_SOURCE":
		return true
	}
	return false
}

func powerState(source string) string {
	if isStandby(source) {
		return "OFF"
	}
	return "ON"
}

func boolOnOff(on bool) string {
	if on {
		return "ON"
	}
	return "OFF"
}

func clampVol(v int) int {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

// prettyStatus turns the firmware's play-state token (e.g. "PLAY_STATE") into a
// short lower-case word for the HA sensor.
func prettyStatus(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(strings.ToUpper(s), "_STATE")
	switch s {
	case "PLAY":
		return "playing"
	case "PAUSE":
		return "paused"
	case "STOP":
		return "stopped"
	case "BUFFERING":
		return "buffering"
	case "":
		return ""
	default:
		return strings.ToLower(s)
	}
}
