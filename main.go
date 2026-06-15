// Command stlocal is a tiny on-speaker agent for Bose SoundTouch speakers after
// the Bose cloud shutdown. It keeps the speaker's NATIVE music sources alive with
// a minimal local pairing stub (so TUNEIN / INTERNET_RADIO keep working — the speaker
// resolves + streams them itself against the live TuneIn service), and serves a
// web UI on :8000 to search TuneIn, manage the 6 presets, play, and set volume.
// No cloud, no desktop app, no UPnP. Can also run on a PC pointed at a speaker IP
// for testing (-speaker-host <ip>).
package main

import (
	"context"
	"flag"
	"hash/fnv"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"time"

	"github.com/stein155/retouch/internal/autopair"
	"github.com/stein155/retouch/internal/marge"
	"github.com/stein155/retouch/internal/settings"
	"github.com/stein155/retouch/internal/speaker"
	"github.com/stein155/retouch/internal/store"
	"github.com/stein155/retouch/internal/tunein"
	"github.com/stein155/retouch/internal/web"
)

func main() {
	listen := flag.String("listen", ":8000", "web UI / API listen address")
	margeAddr := flag.String("listen-marge", ":9080", "pairing-stub HTTP listen address; point the speaker's margeServerUrl / bmxRegistryUrl here")
	margeBase := flag.String("marge-base", "", "base URL the speaker reaches the pairing stub at (default http://127.0.0.1<listen-marge>); rewritten into the BMX registry")
	host := flag.String("speaker-host", "127.0.0.1", "speaker host (127.0.0.1 on-speaker; the speaker IP for off-speaker testing)")
	presets := flag.String("presets", "presets.json", "path to the presets JSON file")
	accountID := flag.String("account-id", "", "marge account UUID to keep the speaker paired to (default: whatever the speaker reports); enables autopair")
	pairEvery := flag.Duration("pair-interval", 5*time.Minute, "how often autopair re-checks the speaker's association")
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
		base = "http://127.0.0.1" + *margeAddr
	}

	bc := speaker.New(*host)
	// Personalise the replayed account documents with this speaker's own identity.
	infoCtx, infoCancel := context.WithTimeout(context.Background(), 5*time.Second)
	info, err := bc.Info(infoCtx)
	infoCancel()
	if err != nil {
		logger.Warn("read speaker /info (account docs keep placeholders)", "err", err, "speaker", *host)
		info = &speaker.Info{}
	} else {
		logger.Info("speaker identity", "device", info.DeviceID, "account", info.Account, "name", info.Name)
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

	tc := tunein.New()
	set := settings.Open(*presets + ".settings")
	webSrv := web.New(tc, bc, st, set, logger)
	margeSrv, err := marge.New(base, info, *presets+".marge", tc, logger.With("comp", "marge"))
	if err != nil {
		logger.Error("init marge stub", "err", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	// Keep the speaker paired to our marge account so native sources stay enabled.
	go autopair.New(bc, info.Account, autopair.DefaultAuthToken, *pairEvery, logger.With("comp", "autopair")).Run(ctx)

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

	logger.Info("stlocal up", "webui", *listen, "marge", *margeAddr, "marge-base", base, "speaker", *host, "presets", *presets)
	wg.Wait()
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
