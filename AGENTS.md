# AGENTS.md

Guidance for AI coding agents working in this repository. Keep it accurate as the
project evolves.

## What this is

ReTouch brings internet radio back to Bose SoundTouch speakers after Bose shut down
its cloud servers. It is a single Go program that runs **on the speaker itself** and:

1. Emulates the small part of the Bose cloud API the firmware checks in with, so the
   speaker re-enables its own native internet-radio sources.
2. Serves a web app (port `:8000`, reached on `:80` via a boot-time redirect) for
   searching stations, managing the six presets, controlling playback, grouping
   other ReTouch speakers into multiroom zones, and updating itself over the air.

It does **not** stream or re-route audio — the speaker plays radio itself, exactly
as it did before. ReTouch only stands in for the API that used to live in the cloud.

## Layout

```
main.go              flags + HTTP servers (web app + local cloud-API emulation)
internal/tunein/     TuneIn directory client (search / resolve / describe)
internal/speaker/    speaker control (play, presets, volume, name, bass, multiroom zones)
internal/discover/   finds other ReTouch speakers on the LAN (for multiroom)
internal/marge/      local emulation of the Bose cloud API the firmware expects
internal/autopair/   keeps the speaker's sources enabled
internal/settings/   persisted app settings (name, bass, language)
internal/store/      small on-disk state (presets, etc.)
internal/web/        JSON API + the embedded web app (built from frontend/)
frontend/            web app source (React + Vite, embedded via go:embed)
install/             wireless install: install.sh / netinstall.sh / uninstall.sh
.github/             CI: build + publish releases, Release Drafter
```

## Build & test

This is a stdlib-only Go module (`go.mod`). **Do not install toolchains on the host
— always build inside Docker** and clean up afterward.

```sh
# build for the speaker (ARMv7) and for local testing (amd64)
docker run --rm -v "$PWD":/src -w /src golang:1.22-alpine \
  sh -c 'GOOS=linux GOARCH=arm GOARM=7 go build -o build/retouch-armv7l . && go build -o build/retouch-amd64 .'

# tests / vet
docker run --rm -v "$PWD":/src -w /src golang:1.22-alpine sh -c 'go vet ./... && go test ./...'
```

The web app is built separately and embedded via `go:embed` into `internal/web/dist`:

```sh
docker run --rm -v "$PWD/frontend":/app -w /app node:20-alpine \
  sh -c 'npm ci && npm run build'
```

`frontend/node_modules` is gitignored; the built `dist` is committed so the Go build
is self-contained. Rebuild the frontend whenever you change anything under
`frontend/src`.

## How it runs on the speaker

- The web app listens on `:8000`, but is exposed on exactly one uniform port — `:8080` —
  via a boot-time `iptables` redirect, while a `raw`-table rule hides direct LAN access to
  `:8000` (loopback stays open for the agent). `:8080` is the only port that works on every
  speaker: on the dual-processor SoundTouch 20/30 the LAN `:80` is owned by a second
  processor (can't be redirected) and `:8000` is firewalled, but `:8080` is forwarded to the
  main processor. Bose's own setup servers are left untouched. The cloud-API emulation
  listens on a loopback port.
- The speaker's service URLs are pointed at the on-speaker emulation; the factory
  configuration is backed up first so it can be fully restored.
- An autostart entry relaunches ReTouch on every boot; on boot it re-checks in so the
  native radio sources come back automatically.
- Everything is reversible via `install/uninstall.sh`.

When referring to a speaker in code, docs, or scripts, use a placeholder such as
`<speaker-ip>` — never hard-code a specific address.

## Conventions

- **Stdlib-only Go.** Don't add Go dependencies without a strong reason.
- **Reversible by default.** Anything that changes the speaker must back up what it
  replaces and be undoable from `uninstall.sh`.
- **No domain jargon in user-facing text.** Installer and UI copy should read plainly
  for a non-technical user.
- Match the surrounding code's style; keep changes small and focused.
- Releases are built and published by CI (`.github/`); the install scripts pull the
  latest GitHub release.
