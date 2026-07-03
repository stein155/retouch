// Package habridge couples a Bose SoundTouch speaker to Home Assistant over MQTT.
//
// Home Assistant has no MQTT media_player, so the speaker is modelled as one MQTT
// device (grouped via the discovery `device` block) made of standard entities:
//
//	number  volume        set/read the volume 0..100
//	switch  power         turn the speaker on / put it in standby
//	select  preset        switch between the six native presets (stations)
//	button  play/pause/…  transport keys
//	sensor  station/…     current channel, track, artist, play status
//	sensor  sw_version    speaker software version + model
//	button  ota           trigger a ReTouch self-update (optional)
//
// The bridge publishes MQTT discovery so the device appears in Home Assistant
// automatically, mirrors the speaker's state on a poll, and routes command topics
// back to the speaker. It self-gates on the persisted MQTT settings and can be
// told to reconnect after a settings change via Reload.
package habridge

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stein155/retouch/internal/mqtt"
	"github.com/stein155/retouch/internal/speaker"
)

// Updater backs the Home Assistant `update` entity. UpdateInfo reports the running
// version, the latest available version, a release URL, and whether updating is
// possible here (only on an installed speaker); UpdateToLatest performs the install.
// The web server implements this; kept as an interface so habridge doesn't depend on
// the web package.
type Updater interface {
	UpdateInfo(ctx context.Context) (installed, latest, releaseURL string, updatable bool, err error)
	UpdateToLatest(ctx context.Context) error
}

// Config is the MQTT connection configuration, sourced from the settings store.
type Config struct {
	Enabled         bool
	Host            string
	Port            int
	Username        string
	Password        string
	BaseTopic       string // topic root; default "retouch/<deviceID>"
	DiscoveryPrefix string // default "homeassistant"
	TLS             bool
}

func (c Config) addr() string {
	port := c.Port
	if port == 0 {
		if c.TLS {
			port = 8883
		} else {
			port = 1883
		}
	}
	return c.Host + ":" + strconv.Itoa(port)
}

// availabilityOnline / availabilityOffline are the availability payloads (also the
// HA discovery defaults).
const (
	availabilityOnline  = "online"
	availabilityOffline = "offline"
	pollInterval        = 5 * time.Second
	reconnectDelay      = 15 * time.Second
	updateCheckInterval = 6 * time.Hour
)

// Bridge runs the MQTT integration for the life of the process.
type Bridge struct {
	sp    *speaker.Client
	cfgFn func() Config
	// updater, when non-nil, backs a Home Assistant `update` entity: it reports the
	// installed/available versions (so HA shows an update notification) and performs
	// the install when HA asks for it.
	updater Updater
	// nowPlaying, when set, returns the enriched now-playing (live track/artist
	// from the stream metadata). The speaker's raw /now_playing only carries the
	// station, so without this HA would never see the actual song. Falls back to
	// the raw speaker read when nil.
	nowPlaying func(context.Context) (*speaker.NowPlaying, error)
	log        *slog.Logger

	reload chan struct{}

	mu        sync.Mutex
	connected bool
	lastErr   string
}

// New builds a Bridge. cfgFn is read on every (re)connect so a settings change is
// picked up by Reload. updater is optional (nil disables the HA update entity).
func New(sp *speaker.Client, cfgFn func() Config, updater Updater, log *slog.Logger) *Bridge {
	return &Bridge{
		sp:      sp,
		cfgFn:   cfgFn,
		updater: updater,
		log:     log,
		reload:  make(chan struct{}, 1),
	}
}

// SetNowPlaying supplies the enriched now-playing source (the web layer's
// EnrichedNowPlaying), so Home Assistant sees the live track and artist, not just
// the station. Optional; without it the bridge uses the speaker's raw read.
func (b *Bridge) SetNowPlaying(fn func(context.Context) (*speaker.NowPlaying, error)) {
	b.nowPlaying = fn
}

// Reload signals the run loop to drop any current connection and reconnect using
// the latest settings. Non-blocking and safe to call from HTTP handlers.
func (b *Bridge) Reload() {
	select {
	case b.reload <- struct{}{}:
	default: // a reload is already pending
	}
}

// Status reports whether the broker connection is currently up and the last error.
func (b *Bridge) Status() (connected bool, lastErr string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.connected, b.lastErr
}

func (b *Bridge) setStatus(connected bool, err error) {
	b.mu.Lock()
	b.connected = connected
	if err != nil {
		b.lastErr = err.Error()
	} else if connected {
		b.lastErr = ""
	}
	b.mu.Unlock()
}

// Run is the background service entry point (matches app.RegisterService). It
// blocks until ctx is cancelled.
func (b *Bridge) Run(ctx context.Context) {
	for {
		cfg := b.cfgFn()
		if !cfg.Enabled || cfg.Host == "" {
			b.setStatus(false, nil)
			if !b.wait(ctx, 0) { // wait for a reload or shutdown
				return
			}
			continue
		}

		err := b.serve(ctx, cfg)
		if ctx.Err() != nil {
			return
		}
		b.setStatus(false, err)
		if err != nil {
			b.log.Warn("mqtt connection ended", "err", err)
		}
		if !b.wait(ctx, reconnectDelay) {
			return
		}
	}
}

// wait blocks until a reload arrives, delay elapses (delay<=0 waits indefinitely),
// or ctx is cancelled. It returns false only when ctx is cancelled.
func (b *Bridge) wait(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		select {
		case <-ctx.Done():
			return false
		case <-b.reload:
			return true
		}
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-b.reload:
		return true
	case <-t.C:
		return true
	}
}

// serve holds one broker connection open, publishing state and routing commands,
// until the connection drops, a reload is requested, or ctx is cancelled.
func (b *Bridge) serve(ctx context.Context, cfg Config) error {
	infoCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	info, err := b.sp.Info(infoCtx)
	cancel()
	if err != nil || info.DeviceID == "" {
		if err == nil {
			err = fmt.Errorf("speaker has no device id yet")
		}
		return fmt.Errorf("read speaker identity: %w", err)
	}

	base := strings.TrimRight(cfg.BaseTopic, "/")
	if base == "" {
		base = "retouch/" + info.DeviceID
	}
	disc := cfg.DiscoveryPrefix
	if disc == "" {
		disc = "homeassistant"
	}
	tp := topics{base: base}

	client, err := mqtt.Connect(ctx, mqtt.Options{
		Addr:      cfg.addr(),
		ClientID:  "retouch-" + info.DeviceID,
		Username:  cfg.Username,
		Password:  cfg.Password,
		TLS:       cfg.TLS,
		KeepAlive: 30 * time.Second,
		Will:      &mqtt.Will{Topic: tp.availability(), Payload: []byte(availabilityOffline), Retain: true},
		Handler: func(c *mqtt.Client, topic string, payload []byte) {
			// Route on a fresh goroutine so a slow speaker call never stalls the
			// MQTT read loop (and thus other commands / the birth message). The
			// client is passed in, so there is no race on capturing it.
			go b.handle(ctx, c, tp, disc, info, topic, string(payload))
		},
	})
	if err != nil {
		return err
	}
	defer client.Disconnect()
	b.setStatus(true, nil)
	b.log.Info("mqtt connected", "broker", cfg.addr(), "base", base)

	if err := client.Subscribe(base+"/#", disc+"/status"); err != nil {
		return err
	}
	_ = client.Publish(tp.availability(), []byte(availabilityOnline), true)
	b.announce(ctx, client, tp, disc, info)

	// Publish state changes on a poll. published tracks the last value per topic so
	// retained messages are only re-sent when something actually changed.
	published := map[string]string{}
	b.refresh(ctx, client, tp, published)
	b.publishUpdateState(ctx, client, tp)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()
	// The update check hits GitHub, so it runs far less often than the state poll.
	updateTicker := time.NewTicker(updateCheckInterval)
	defer updateTicker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-b.reload:
			return nil
		case <-client.Done():
			return client.Err()
		case <-ticker.C:
			b.refresh(ctx, client, tp, published)
		case <-updateTicker.C:
			b.publishUpdateState(ctx, client, tp)
		}
	}
}
