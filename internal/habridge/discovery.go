package habridge

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/stein155/retouch/internal/mqtt"
	"github.com/stein155/retouch/internal/speaker"
)

// transportButtons are the transport keys exposed as HA buttons. The map value is
// the SoundTouch remote key sent to the speaker.
var transportButtons = []struct{ key, speakerKey, name string }{
	{"play", "PLAY", "Play"},
	{"pause", "PAUSE", "Pause"},
	{"stop", "STOP", "Stop"},
	{"next", "NEXT_TRACK", "Next"},
	{"prev", "PREV_TRACK", "Previous"},
}

// announce publishes the retained MQTT discovery configs so Home Assistant creates
// (or refreshes) the device and all its entities, then publishes the static
// diagnostic state (software version, model). Called on connect and whenever HA
// signals a restart (birth message).
func (b *Bridge) announce(ctx context.Context, client *mqtt.Client, tp topics, disc string, info *speaker.Info) {
	dev := map[string]any{
		"identifiers":  []string{info.DeviceID},
		"name":         info.Name,
		"manufacturer": "Bose",
	}
	if info.Type != "" {
		dev["model"] = info.Type
	}
	if info.Software != "" {
		dev["sw_version"] = info.Software
	}
	origin := map[string]any{"name": "ReTouch"}

	// entity assembles one discovery payload with the shared device/availability/
	// origin blocks folded in.
	entity := func(name, key string, extra map[string]any) map[string]any {
		cfg := map[string]any{
			"name":               name,
			"unique_id":          info.DeviceID + "_" + key,
			"availability_topic": tp.availability(),
			"device":             dev,
			"origin":             origin,
		}
		for k, v := range extra {
			cfg[k] = v
		}
		return cfg
	}

	publish := func(component, key string, cfg map[string]any) {
		payload, err := json.Marshal(cfg)
		if err != nil {
			return
		}
		topic := disc + "/" + component + "/" + info.DeviceID + "/" + key + "/config"
		_ = client.Publish(topic, payload, true)
	}

	publish("number", "volume", entity("Volume", "volume", map[string]any{
		"command_topic": tp.volumeSet(),
		"state_topic":   tp.volumeState(),
		"min":           0,
		"max":           100,
		"step":          1,
		"icon":          "mdi:volume-high",
	}))

	publish("switch", "power", entity("Power", "power", map[string]any{
		"command_topic": tp.powerSet(),
		"state_topic":   tp.powerState(),
		"payload_on":    "ON",
		"payload_off":   "OFF",
		"icon":          "mdi:power",
	}))

	publish("select", "preset", entity("Preset", "preset", map[string]any{
		"command_topic": tp.presetSet(),
		"state_topic":   tp.presetState(),
		"options":       b.presetOptions(ctx),
		"icon":          "mdi:radio",
	}))

	for _, tb := range transportButtons {
		publish("button", tb.key, entity(tb.name, tb.key, map[string]any{
			"command_topic": tp.transportSet(tb.key),
		}))
	}

	sensor := func(name, key, stateTopic, icon string, diagnostic bool) {
		extra := map[string]any{"state_topic": stateTopic}
		if icon != "" {
			extra["icon"] = icon
		}
		if diagnostic {
			extra["entity_category"] = "diagnostic"
		}
		publish("sensor", key, entity(name, key, extra))
	}
	sensor("Station", "station", tp.station(), "mdi:radio", false)
	sensor("Track", "track", tp.track(), "mdi:music-note", false)
	sensor("Artist", "artist", tp.artist(), "mdi:account-music", false)
	sensor("Status", "status", tp.status(), "mdi:play-pause", false)
	sensor("Software version", "sw_version", tp.swVersion(), "", true)
	sensor("Model", "model", tp.model(), "", true)

	// An `update` entity: Home Assistant shows it under Settings → Updates with a
	// notification when a newer release is available, plus an Install button — the
	// native update experience, not just a button on the device.
	if b.updater != nil {
		publish("update", "update", entity("ReTouch", "update", map[string]any{
			"state_topic":     tp.updateState(),
			"command_topic":   tp.updateInstall(),
			"payload_install": "install",
			"device_class":    "firmware",
			"icon":            "mdi:update",
		}))
	}

	// Static diagnostics: publish once (retained) so they survive HA restarts.
	if info.Software != "" {
		_ = client.Publish(tp.swVersion(), []byte(info.Software), true)
	}
	if info.Type != "" {
		_ = client.Publish(tp.model(), []byte(info.Type), true)
	}
}

// publishUpdateState publishes the HA `update` entity's JSON state (installed and
// latest version + release URL). MQTT Update reads installed_version/latest_version
// from this payload and raises the update notification when they differ. A transient
// version-check failure leaves the last retained state in place rather than clearing
// it. No-op when no updater is wired in.
func (b *Bridge) publishUpdateState(ctx context.Context, client *mqtt.Client, tp topics) {
	if b.updater == nil {
		return
	}
	c, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	installed, latest, releaseURL, updatable, err := b.updater.UpdateInfo(c)
	if err != nil {
		b.log.Warn("mqtt update check", "err", err)
		return
	}
	if !updatable || latest == "" {
		latest = installed // nothing to offer -> HA shows "up to date"
	}
	payload := map[string]string{"installed_version": installed, "latest_version": latest}
	if releaseURL != "" && latest != installed {
		payload["release_url"] = releaseURL
	}
	if data, err := json.Marshal(payload); err == nil {
		_ = client.Publish(tp.updateState(), data, true)
	}
}

// presetOptions returns the names of the speaker's non-empty presets, for the HA
// select. HA requires a non-empty options list, so it falls back to a placeholder
// (ignored by the command handler) when the speaker has no presets yet.
func (b *Bridge) presetOptions(ctx context.Context) []string {
	c, cancel := context.WithTimeout(ctx, 6*time.Second)
	defer cancel()
	presets, err := b.sp.Presets(c)
	if err != nil {
		return []string{presetPlaceholder}
	}
	var names []string
	for _, p := range presets {
		if name := p.Name; name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return []string{presetPlaceholder}
	}
	return names
}

// presetPlaceholder is the select option shown when there are no presets. Selecting
// it is a no-op.
const presetPlaceholder = "—"

// itoa is a tiny helper so callers reading volume don't import strconv.
func itoa(n int) string { return strconv.Itoa(n) }
