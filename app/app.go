// Package app is retouch's composable entrypoint: main.go just calls Run().
//
// Besides starting the web UI and the marge pairing stub, Run() also starts any
// background services registered via RegisterService before startup. That hook is
// generic — app has no knowledge of what registers — so the binary can be composed
// from an external wrapper that adds extra long-running tasks into this single Go
// runtime. The default build registers none, so behaviour is unchanged.
package app

import (
	"context"
	"flag"
	"hash/fnv"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/stein155/retouch/internal/autopair"
	"github.com/stein155/retouch/internal/habridge"
	"github.com/stein155/retouch/internal/marge"
	"github.com/stein155/retouch/internal/mdns"
	"github.com/stein155/retouch/internal/plugins"
	"github.com/stein155/retouch/internal/settings"
	"github.com/stein155/retouch/internal/speaker"
	"github.com/stein155/retouch/internal/store"
	"github.com/stein155/retouch/internal/tunein"
	"github.com/stein155/retouch/internal/update"
	"github.com/stein155/retouch/internal/web"
)

var version = "dev"

// SetVersion stamps the build version (main passes its -ldflags value). Call before Run().
func SetVersion(v string) { version = v }

// services holds background tasks registered before Run(). Each runs as its own
// goroutine bound to the run context and keeps the process alive like the listeners.
var services []func(context.Context)

// RegisterService registers a background task to run for the life of the process.
// Generic: app neither knows nor cares what f does. Call before Run().
func RegisterService(f func(context.Context)) { services = append(services, f) }

// Run starts retouch and blocks until interrupted.
func Run() {
	listen := flag.String("listen", ":8000", "web UI / API listen address")
	margeAddr := flag.String("listen-marge", "127.0.0.1:9080", "pairing-stub HTTP listen address; the speaker only ever reaches it on loopback, so bind loopback by default (do not expose it to the LAN)")
	margeBase := flag.String("marge-base", "", "base URL the speaker reaches the pairing stub at (default http://127.0.0.1<listen-marge>); rewritten into the BMX registry")
	host := flag.String("speaker-host", "127.0.0.1", "speaker host (127.0.0.1 on-speaker; the speaker IP for off-speaker testing)")
	presets := flag.String("presets", "presets.json", "path to the presets JSON file")
	accountID := flag.String("account-id", "", "marge account UUID to keep the speaker paired to (default: whatever the speaker reports); enables autopair")
	pairEvery := flag.Duration("pair-interval", 5*time.Minute, "how often autopair re-checks the speaker's association")
	sideload := flag.Bool("allow-sideload", false, "allow installing plugin binaries uploaded through the web UI without release verification (anyone on the LAN can then run code on the speaker; leave off unless you are developing a plugin)")
	verbose := flag.Bool("v", false, "verbose: log every speaker request to the pairing stub")
	flag.Parse()

	level := slog.LevelInfo
	if *verbose {
		level = slog.LevelDebug
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level}))

	st, err := store.Open(*presets)
	if err != nil {
		logger.Error("open preset store", "err", err)
		os.Exit(1)
	}

	base := *margeBase
	if base == "" {
		// -listen-marge may omit the host (":9080"); the speaker reaches the stub on
		// loopback, so default the base host to 127.0.0.1 in that case.
		addr := *margeAddr
		if strings.HasPrefix(addr, ":") {
			addr = "127.0.0.1" + addr
		}
		base = "http://" + addr
	}

	bc := speaker.New(*host)
	// Personalise the replayed account documents with this speaker's own identity.
	// On boot, :8090 can come up after ReTouch; do not freeze empty placeholders into
	// Marge documents just because the first request races the speaker firmware.
	info, err := waitSpeakerInfo(context.Background(), bc, 90*time.Second)
	if err != nil {
		logger.Warn("read speaker /info (account docs keep placeholders)", "err", err, "speaker", *host)
		info = &speaker.Info{}
	} else {
		logger.Info("speaker identity", "device", info.DeviceID, "account", info.Account, "name", info.Name, "software", info.Software)
	}

	// Resolve the marge account. The old margeAccountUUID was a Bose-cloud id; locally
	// it is arbitrary, so when the speaker reports none we synthesise a stable one from the
	// device id (-account-id overrides). The stub serves this account and autopair keeps
	// the speaker paired to it.
	if *accountID != "" {
		info.Account = *accountID
	} else if info.Account == "" {
		info.Account = synthAccount(info.DeviceID)
		logger.Info("synthesised local account id", "account", info.Account)
	}

	presetCtx, presetCancel := context.WithTimeout(context.Background(), 8*time.Second)
	speakerPresets, err := bc.Presets(presetCtx)
	presetCancel()
	if err != nil {
		logger.Warn("read speaker presets for marge seed", "err", err)
	} else {
		logger.Info("seed marge presets from speaker", "count", len(speakerPresets))
	}
	nativePresets := make([]marge.PresetSeed, 0, len(speakerPresets))
	for _, p := range speakerPresets {
		nativePresets = append(nativePresets, marge.PresetSeed{Slot: p.Slot, Name: p.Name, Location: p.Location, Logo: p.Logo})
	}

	tc := tunein.New()
	set := settings.Open(*presets + ".settings")
	// The update manager owns self-updates (release lookup, verified install,
	// restart); the web API and the Home Assistant bridge both drive it.
	upd := update.New(version, filepath.Dir(*presets), logger.With("comp", "update"))
	webSrv := web.New(tc, bc, st, set, upd, filepath.Dir(*presets), logger)
	margeSrv, err := marge.New(base, info, *presets+".marge", nativePresets, tc, logger.With("comp", "marge"))
	if err != nil {
		logger.Error("init marge stub", "err", err)
		os.Exit(1)
	}
	webSrv.SetPresetMirror(margeSrv)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Keep the speaker paired to our marge account so native sources stay enabled.
	pairer := autopair.New(bc, info.Account, autopair.DefaultAuthToken, *pairEvery, logger.With("comp", "autopair"))
	// A speaker that lost its pairing was factory-reset (physical access): that is
	// the recovery path for a forgotten settings password — clear it and reopen
	// telnet before re-pairing.
	pairer.OnFactoryReset(func() {
		if err := webSrv.RecoverAfterFactoryReset(); err != nil {
			logger.Warn("factory-reset recovery failed", "err", err)
		}
	})
	go pairer.Run(ctx)

	// Home Assistant MQTT bridge: reads its config from the settings store on every
	// (re)connect, so the web UI's MQTT section takes effect via bridge.Reload().
	// The HA update entity drives the same update manager as POST /api/update.
	bridge := habridge.New(bc, func() habridge.Config {
		m := set.Get().MQTT
		return habridge.Config{
			Enabled:         m.Enabled,
			Host:            m.Host,
			Port:            m.Port,
			Username:        m.Username,
			Password:        m.Password,
			BaseTopic:       m.BaseTopic,
			DiscoveryPrefix: m.DiscoveryPrefix,
			TLS:             m.TLS,
		}
	}, upd, logger.With("comp", "habridge"))
	// Feed the bridge the enriched now-playing so HA shows the live track/artist,
	// not just the station name (the speaker no longer receives track metadata).
	bridge.SetNowPlaying(webSrv.EnrichedNowPlaying)
	webSrv.SetMQTTBridge(bridge)
	go bridge.Run(ctx)

	// Advertise a friendly <name>.local so the UI is reachable without the IP.
	if info.IP != "" {
		resp := mdns.New(info.IP, info.Name, logger.With("comp", "mdns"))
		webSrv.SetMDNS(resp)
		go func() {
			if err := resp.Run(ctx); err != nil {
				logger.Warn("mdns responder stopped", "err", err)
			}
		}()
	}

	// Plugin host: downloads/verifies plugin binaries (reusing the OTA path), then
	// supervises each as a child process and reverse-proxies its config API under
	// /api/plugins/<name>/. Plugins reach the speaker's local API and call back to
	// this web server. Lives under the same home dir as the presets/state.
	speakerAddr := *host
	if _, _, err := net.SplitHostPort(speakerAddr); err != nil {
		speakerAddr = net.JoinHostPort(*host, "8090")
	}
	webHost := *listen
	if strings.HasPrefix(webHost, ":") {
		webHost = "127.0.0.1" + webHost
	}
	pm, err := plugins.New(filepath.Join(filepath.Dir(*presets), "plugins"), speakerAddr, "http://"+webHost, "ReTouch/"+version, logger.With("comp", "plugins"))
	if err != nil {
		logger.Warn("plugin host disabled", "err", err)
	} else {
		webSrv.SetPlugins(pm, *sideload)
		// Stop plugin children before the post-update restart: os.Exit skips
		// context cancellation, so without this they'd be orphaned to init and
		// duplicated by the relaunched ReTouch (two Ring agents then invalidate
		// each other's rotating refresh token).
		upd.SetBeforeRestart(func() { pm.Shutdown(3 * time.Second) })
		go pm.Run(ctx)
	}

	// Push live playback/volume state to browsers over SSE (/api/events).
	go webSrv.Run(ctx)

	var wg sync.WaitGroup
	serve := func(name, addr string, h http.Handler) {
		wg.Add(1)
		srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
		go func() {
			<-ctx.Done()
			sh, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = srv.Shutdown(sh)
		}()
		go func() {
			defer wg.Done()
			logger.Info("listener up", "name", name, "addr", addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Error("listener failed", "name", name, "addr", addr, "err", err)
			}
		}()
	}

	serve("webui", *listen, webSrv.Handler())
	serve("marge", *margeAddr, margeSrv.Handler())

	// Start any registered background services (generic; none in the default build).
	for _, svc := range services {
		svc := svc
		wg.Add(1)
		go func() {
			defer wg.Done()
			svc(ctx)
		}()
	}

	logger.Info("retouch up", "version", version, "webui", *listen, "marge", *margeAddr, "marge-base", base, "speaker", *host, "presets", *presets, "services", len(services))
	wg.Wait()
}

func waitSpeakerInfo(ctx context.Context, bc *speaker.Client, timeout time.Duration) (*speaker.Info, error) {
	deadline := time.Now().Add(timeout)
	var last error
	for {
		c, cancel := context.WithTimeout(ctx, 5*time.Second)
		info, err := bc.Info(c)
		cancel()
		if err == nil && info.DeviceID != "" && info.Name != "" && info.IP != "" && info.Software != "" {
			return info, nil
		}
		if err != nil {
			last = err
		}
		if time.Now().After(deadline) {
			if last != nil {
				return nil, last
			}
			return nil, context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
}

// synthAccount derives a stable 7-digit local marge account id from the device id.
// Locally the account is just an association handle, so any stable value works; a
// hash of the device id keeps it stable across restarts without persistence.
func synthAccount(seed string) string {
	if seed == "" {
		seed = "stlocal"
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(seed))
	return strconv.Itoa(int(h.Sum32()%9_000_000 + 1_000_000))
}
