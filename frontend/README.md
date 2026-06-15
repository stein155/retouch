# Radio · Bose SoundTouch

A small, warm web app to control your **Bose SoundTouch** speakers from your phone or
desktop. Add your speakers by IP, browse thousands of stations via TuneIn, and tap a
preset to play. Everything is stored locally on the device — no account, no database, no
backend.

<p align="center">
  <img src="screenshots/01-home.png" width="180" alt="Home with presets" />
  <img src="screenshots/02-settings.png" width="180" alt="Radio settings" />
  <img src="screenshots/03-editor.png" width="180" alt="Edit a radio" />
  <img src="screenshots/04-search.png" width="180" alt="Discover stations" />
  <img src="screenshots/05-language.png" width="180" alt="Language picker" />
</p>

## Features

- **Configurable radios** — add, edit and remove speakers yourself. Each radio has a
  **name**, **IP address** and **emoji** icon.
- **Local-only settings** — your radios live in `localStorage`, so the configuration is
  **per device**. No accounts, no server-side state.
- **Multiple languages** — the interface is available in English, Deutsch, Nederlands,
  Français, Español and Afrikaans. It auto-detects your browser language and remembers your
  choice locally.
- **Switch between speakers** with a segmented control.
- **Play / stop** and adjust **volume** in the mini player.
- **6 presets per speaker**, read straight from the device; assign new ones from search.
- **Discover stations** — search TuneIn plus a curated catalogue, with typographic logos.

## How it talks to your speakers

The browser talks to each speaker **directly** over your LAN using the Bose SoundTouch HTTP
API:

```
Browser ──▶ http://<speaker-ip>:8090   (now playing, volume, presets, play/stop)
```

TuneIn (station search + logos) is the only non-speaker traffic; it goes through a small
same-origin proxy because TuneIn doesn't allow browser CORS (Vite dev server in dev, nginx
in production — see [`vite.config.js`](vite.config.js) / [`nginx.conf`](nginx.conf)).

> ⚠️ **Serve the app over HTTP on your local network.** Browsers block an **HTTPS** page
> from calling a plain-`http://` speaker (mixed content). So a cloud HTTPS deployment such
> as `https://radio.example.com` **cannot** reach speakers at `192.168.x.x`. Run the app on
> the LAN over HTTP (e.g. `http://<host-ip>` from the Docker container below) and open it
> from a device on the same network.

## Configuration

There is nothing to configure at build time. Open the app, tap the **⚙️ gear** in the
header, and manage your radios:

1. **Radio toevoegen** — pick an emoji, give it a name, and enter the speaker's IP address
   (validated as IPv4).
2. Tap an existing radio to **edit** or **delete** it.

On first run two example radios (`Keuken` and `Kantoor`) are seeded so the app isn't empty;
edit or remove them to match your setup. Pick your **language** lower down in the same
Settings sheet. All changes are saved instantly to `localStorage` under the key
`bose-radio.radios.v1`.

> **Tip:** find a speaker's IP in the Bose app, or in your router's client list.

## Tech stack

| Layer     | Technology                      |
|-----------|---------------------------------|
| UI        | React 18 + Vite                 |
| Styling   | Plain CSS (design tokens)       |
| API       | Bose SoundTouch HTTP API + TuneIn |
| Container | Docker (multi-stage) + nginx    |

## Getting started

### Development

```bash
npm install
npm run dev
```

The dev server runs on `http://localhost:5173`.

### Production build

```bash
npm run build      # outputs static files to dist/
npm run preview    # preview the production build
```

### Docker

```bash
docker compose up -d
```

The app is served on port 80. The image is also built and published to GHCR on every push
to `main`:

```
ghcr.io/stein155/soundtouch-react:latest
```

## Project layout

```
src/
  App.jsx                  app shell, radio state, settings wiring
  lib/
    radios.js              radio model + localStorage persistence
    i18n.js                translations, language context + helpers
    api.js                 Bose SoundTouch + TuneIn API client
    stations.js            curated station catalogue + logos
  hooks/
    useSpeaker.js          per-speaker polling (now playing, volume, presets)
  components/
    SettingsSheet.jsx      radio list + add/edit/delete editor
    Presets.jsx            preset grid
    MiniPlayer.jsx         bottom player + volume
    SearchSheet.jsx        station discovery
    StationLogo.jsx        station lettermarks / artwork
    Icons.jsx              inline SVG icons
```
