<p align="center">
  <img src="docs/readme-header.png" width="820"
       alt="ReTouch — for SoundTouch. Bose switched off the SoundTouch servers; ReTouch brings the radio back, running entirely on the speaker with nothing else to set up." />
</p>

# ReTouch · for SoundTouch

<p>
  <a href="https://github.com/stein155/retouch/releases"><img src="https://img.shields.io/github/v/release/stein155/retouch?color=c47a52&label=release" alt="Latest release" /></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/stein155/retouch?color=c47a52" alt="License: MIT" /></a>
  <img src="https://img.shields.io/github/go-mod/go-version/stein155/retouch?color=c47a52&logo=go&logoColor=white" alt="Go version" />
  <img src="https://img.shields.io/badge/runs%20on-SoundTouch%20speaker-c47a52" alt="Runs on the SoundTouch speaker" />
</p>

**Your SoundTouch went quiet when Bose switched off its servers. ReTouch brings
the radio back — running entirely on the speaker, with nothing else to set up.**

You install it once from a terminal. After that, everything lives in a clean little
web app on the speaker itself: search stations, set your six presets, group speakers
for multiroom, and keep it updated — all from your phone.

> "SoundTouch" and "Bose" are trademarks of Bose Corporation. ReTouch is an
> unofficial community project — use at your own risk.

### Is this you?

If Bose shutting down the SoundTouch servers left you with any of these, ReTouch fixes it:

- Internet radio stopped working and your presets play nothing
- The SoundTouch app can't find or add music sources anymore
- The speaker still powers on and plays Bluetooth/AUX, but the "smart" half is dead
- You looked into a Raspberry Pi or always-on server as a workaround and would rather not

## No extra hardware. No cloud. No fuss.

The whole thing is one tiny program that lives on a speaker you already own. There
is nothing else to buy, run, or keep powered on:

- ✅ **Runs on the speaker itself**, on your own Wi-Fi — that's the only "server"
- ❌ **No Raspberry Pi** and no always-on computer humming in a closet
- ❌ **No VPS, no cloud, no account** — nothing leaves your home network
- ❌ **No desktop app** — after the one-time install you never touch a terminal again
- 📱 **It's just a web page** — open the link, *Add to Home Screen*, and use it like any app

### How that compares to the usual approach

Most ways to revive a SoundTouch put a second box on your network — a Raspberry Pi
or an always-on computer that has to keep running for the speaker to work. ReTouch
takes the other route and lives on the speaker itself:

| | The usual approach | **ReTouch** |
|---|---|---|
| Where it runs | A separate Pi / PC / server | **On the speaker itself** |
| Extra hardware to buy | Usually yes | **None** |
| Something always-on | Yes — if it's off, the radio's off | **No — just the speaker** |
| Where your data goes | Often via an external box or account | **Stays on your home network** |
| Setup | Flash an image, wire it up, maintain it | **One install line, then a web app** |
| Reversible | Varies | **Yes — one uninstall script** |

## Screenshots

| Home | Search | Settings |
|:---:|:---:|:---:|
| ![Home](screenshots/home.png) | ![Search](screenshots/search.png) | ![Settings](screenshots/settings.png) |

## What you get

- 🔎 **Search internet radio** — TuneIn's public directory, no account or key
- ⭐ **Six presets**, stored as the speaker's own native presets
- ▶️ **Play / stop and volume**, with live now-playing (station name + logo)
- 🔗 **Multiroom** — find your other ReTouch speakers and group them so they play
  in sync, using Bose's own native zones
- 🏠 **Home Assistant (MQTT)** — connect the speaker to your MQTT broker and it
  appears in Home Assistant automatically, with volume, station switching, transport,
  power on/off, and now-playing — plus a native update entity that notifies you when a
  new ReTouch release is out and installs it over the air
- ⚙️ **Settings** — speaker name, bass, and the app's language
- ⬆️ **Over-the-air updates** — one tap in the app; the speaker fetches the latest
  release and relaunches ReTouch in a few seconds (no reboot)

## How it works

While Bose ran the cloud, the speaker checked in with Bose's servers to keep its
internet-radio sources enabled. With those servers gone, the sources stop working.

ReTouch **emulates that cloud API locally, on the speaker**, so the firmware sees
a healthy "cloud" again and re-enables its native radio. It does not stream or
re-route audio — the speaker plays radio itself, exactly as before; ReTouch only
takes the place of the API that used to live at Bose. A small web app on the
speaker adds the search, presets, and controls.

**Multiroom** works the same way — through the speaker, not around it. ReTouch
finds your other ReTouch speakers on the network and uses Bose's own zone API to
group them, so one speaker leads and the rest play in perfect sync, exactly like
multiroom did when the Bose app still worked.

**Home Assistant** is optional. Turn it on under Settings → Home Assistant (MQTT),
point it at your broker, and ReTouch publishes MQTT discovery so the speaker shows
up in Home Assistant as one device — no YAML. Home Assistant has no MQTT
media_player, so the speaker is exposed as a set of standard entities (a volume
number, a preset selector, transport buttons, a power switch, and now-playing
sensors) grouped under the one device.

## Set it up once

Installation is **wireless** — nothing is installed on your computer, and this is
the *only* time you need a terminal. Paste this one line into one (macOS or Linux):

```sh
curl -fsSL https://raw.githubusercontent.com/stein155/retouch/main/install/install.sh | sh
```

It finds the Bose speakers on your network, lets you pick one (or type the address
yourself), sets ReTouch up over the air, and prints a link like
`http://192.168.1.42:8080`. The speaker restarts once and is back in a minute or two.

> Already know the address? Skip the search:
> `curl -fsSL .../install.sh | sh -s -- 192.168.1.42`
>
> The installer only needs `curl` and `nc` (netcat), which ship with macOS and most Linux.

**That's it for the terminal.** Open the link on your phone, tap *Add to Home
Screen*, and from now on everything happens right there in the app:

- ▶️ search, play, and manage your presets
- 🔗 group your other speakers — **Settings ▸ Multiroom**
- ⬆️ update over the air — **Settings ▸ Update now** (ReTouch reloads in a few
  seconds, no reboot; does nothing if you're already current — you could also just
  re-run the install line)

**To undo everything** (restore the factory configuration and remove ReTouch), run
`install/uninstall.sh` on the speaker and reboot.

## Tested on

ReTouch has been verified on the following speakers and firmware versions:

| Speaker | Firmware |
|---|---|
| Bose SoundTouch 10 | `27.0.6.46330.5043500 epdbuild.trunk.hepdswbld04.2022-08-04T11:20:29` |
| Bose SoundTouch 10 | `27.0.3.46298.4608935 epdbuild.trunk.hepdswbld04.2021-10-06T16:35:02` |
| Bose SoundTouch 20 | `27.0.6.46330.5043500 epdbuild.trunk.hepdswbld04.2022-08-04T11:20:29` |

Other SoundTouch models and firmware versions may work too — if you try one, let us know.

## Repo layout

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
install/             wireless install: install.sh (find + set up) / netinstall.sh / uninstall.sh
.github/             CI: build + publish releases, Release Drafter
```

## Thanks

ReTouch stands on the shoulders of the SoundTouch community that refused to let
these speakers go quiet. With gratitude to:

- **[AfterTouch](https://github.com/gesellix/Bose-SoundTouch)** — for the groundwork
  on talking to the speaker and keeping its native sources alive.
- **[SoundCork](https://github.com/deborahgu/soundcork)** — for sharing how the
  firmware and its services fit together.
- **[Streborn](https://github.com/JRpersonal/streborn)** — for paving the way on
  getting code onto the speaker.

Thank you for keeping good hardware playing. 🎵
