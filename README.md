<div align="center">

# radio-dj

**A 24/7 AI-DJ internet radio station in a single Go binary.**
Point it at your music folder, bring your own LLM key, and it broadcasts a
continuous Icecast MP3 stream the DJ talks over — live, mid-song.

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![macOS](https://img.shields.io/badge/Platform-macOS%20%7C%20Linux-lightgrey)]()
[![LinkedIn](https://img.shields.io/badge/Follow-johncrash64-0A66C2?logo=linkedin&logoColor=white)](https://www.linkedin.com/in/johncrash64/)

English · [Español](README.es.md)

</div>

---

<img src="docs/screenshots/cassette-running.gif" alt="radio-dj cassette spinning" align="right" width="320" />

`radio-dj` is one Go binary (~9 MB) that runs a complete radio station. It
picks tracks from your library, an AI DJ speaks between songs — track intros,
the time, **real weather**, curiosity facts with web search, and live request
shoutouts — and broadcasts a standard Icecast MP3 stream that any player can
tune into.

No Docker. No bloat. One **8.7 MB binary**, **~20–30 MB of RAM** (~50–60 MB with Icecast + ffmpeg) — measured on macOS, light enough for a Raspberry Pi.

<br clear="right" />

## ✨ Features

- **24/7 radio** — a continuous Icecast MP3 stream (192 kbps) that doesn't drop.
- **AI DJ** speaking between songs — intros, time, real weather, curiosity
  facts (web search), and live request shoutouts. Voice in any language.
- **Live mid-song ducking** — the DJ can speak *over* the music
  (`sidechaincompress`), hardware-mixer style.
- **Live requests** — listeners request songs from the web UI or the API,
  with a name the DJ reads on air.
- **Neo-brutalist UI** — animated cassette tape, now-playing + next, three
  info panels (history, DJ log, requests).
- **Editable skills** — reprogram the DJ's segments as text files. No
  recompiling.
- **BYOK LLM** — GLM, OpenAI, OpenRouter, Ollama, Groq, or any
  OpenAI-compatible provider.
- **Always-on** — installs as a macOS launchd, Linux systemd, or OpenRC service, survives reboots.
- **Installable PWA** — add the radio UI to your dock/home screen (Chrome install button or Safari → Add to Dock). App shell works offline.

---

## 📸 Screenshots

| DJ on air (desktop) | Mobile | Setup wizard |
|:---:|:---:|:---:|
| <img src="docs/screenshots/dj-onair.png" alt="DJ on air — desktop UI with animated cassette" width="100%" /> | <img src="docs/screenshots/mobile.png" alt="Mobile view" width="200" /> | <img src="docs/screenshots/onboarding.png" alt="Onboarding wizard — first-run setup" width="180" /> |

---

## 🚀 Quick start

### One-liner (recommended)

```bash
curl -fsSL https://github.com/johncrash64/radio-dj/raw/master/install.sh | bash
```

Detects your OS (macOS or Linux), installs `icecast` + `ffmpeg` + `edge-tts`,
and downloads a prebuilt binary (or builds from source if none exists).

### From source

**macOS (Homebrew):**
```bash
brew install icecast ffmpeg
pipx install edge-tts

git clone https://github.com/johncrash64/radio-dj.git
cd radio-dj && go build -o radio-dj .
```

**Linux (apt):**
```bash
sudo apt-get install icecast2 ffmpeg
pipx install edge-tts            # or: pip3 install --user edge-tts

git clone https://github.com/johncrash64/radio-dj.git
cd radio-dj && go build -o radio-dj .
```

### Configure & launch

Run `radio-dj serve` — on first run it opens an **onboarding wizard** that
writes everything to `~/.radio-dj/config.json`. Or set env vars (see below):

> **Reconfigure any time:** edit `~/.radio-dj/config.json` and restart, or
> delete it to re-run the wizard.

```bash
export ZAI_API_KEY=your_key             # GLM-5.2 (any OpenAI-compatible works)
export RDJ_LIBRARY=~/Music/library      # your music folder
export RDJ_LOCATION="La Paz"            # for time + weather
export RDJ_VOICE_CMD="edge-tts --voice es-CO-SalomeNeural --text {text} --write-media {out}"
# Optional: override the broadcast encoder (default: aac_at on macOS, aac elsewhere).
# The stream mount follows it: aac → /stream.aac, libmp3lame → /stream.mp3.
# export RDJ_ENCODER=libmp3lame
```

### Always-on service (survives reboots)

```bash
radio-dj install      # macOS → launchd agent · Linux → systemd user unit or OpenRC (needs sudo/doas)
radio-dj uninstall    # stop and remove the service
```

Then open **http://localhost:7710** (UI) and **http://localhost:7702/stream.aac** (stream).

### Install as an app (PWA)

The radio UI is a Progressive Web App — install it for a native-app feel:

- **Chrome / Edge** — open the UI, click the **install icon** in the address bar.
- **macOS Safari** — **File → Add to Dock**.
- **iOS Safari** — Share → **Add to Home Screen**.

Once installed it opens in its own window, no browser chrome, and the app
shell loads offline (the live stream itself needs a connection).

---

## 🎙️ The DJ

The DJ is driven by an **LLM Director** that plans each *tanda* (batch) in one
structured call — it picks the setlist and decides what to say between songs:

- **Track intros** — "Up next, *Song* by *Artist* from *Album*…"
- **Station IDs** — name, location, listener count.
- **Time & weather** — real, via your location.
- **Curiosity facts** — web search via the LLM, or the free Wikipedia API.
- **Request shoutouts** — "This one goes out to *María* — *she asked for…*"

Each voice line: **LLM** writes the text (with track context) → **your TTS**
synthesizes it → it airs before or mid-track. Mid-song segments use
`sidechaincompress` to duck the music while the DJ speaks.

---

## 🧩 Skills (editable)

The DJ's segments live as text files under `~/.radio-dj/skills/`. Edit the
built-ins or add your own — radio-dj loads them at startup. No recompiling.

---

## 📡 API

Simple HTTP — works with any client (mobile app, Home Assistant, PanelHUD):

| Method | Path | Description |
|---|---|---|
| `GET` | `/now-playing` | current + next track, requests, status |
| `POST` | `/request` | `{"from":"María","text":"Bohemian Rhapsody"}` — request a song |
| `POST` | `/control` | `{"action":"next"}` or `{"action":"previous"}` — skip/replay the current track (shared broadcast) |
| `GET` | `/stream.aac` | the audio stream (Icecast) |
| `GET` | `/listen.pls` · `/listen.m3u` | playlist for Sonos/VLC/car |
| `GET` | `/health` | liveness |

The stream is a **standard Icecast / HTTP-MP3 URL** — paste it into VLC, mpv,
any browser, TuneIn, Radio Garden, Sonos, or iOS/Android radio apps.

Full reference: **[docs/API.md](docs/API.md)** · **[docs/openapi.yaml](docs/openapi.yaml)**.

---

## ⚙️ Configuration

radio-dj reads config from **three layers, lowest wins**:

```
env vars (RDJ_*)  >  ~/.radio-dj/config.json  >  defaults
```

**You only need one.** The onboarding wizard writes `config.json` for you.
Power users / CI can override anything with env vars.

### `config.json` keys (written by the wizard, or hand-edit)

```json
{
  "library":       "/home/me/Music/library",
  "source":        "folder",
  "glm_api_key":   "your-key",
  "glm_model":     "glm-5.2",
  "llm_provider":  "glm",
  "voice_provider":"edge-tts",
  "voice":         "es-CO-SalomeNeural",
  "location":      "La Paz",
  "lat":           -16.5,
  "lon":           -68.15,
  "language":      "es",
  "dj_talk":       "regular",
  "chunk":         8,
  "bitrate":       192,
  "station_name":  "radio-dj"
}
```

### Full reference (env var → config.json key → default)

| Env var | `config.json` key | Default | What |
|---|---|---|---|
| `ZAI_API_KEY` / `RDJ_GLM_API_KEY` | `glm_api_key` | — | your LLM key (BYOK) |
| `RDJ_LIBRARY` | `library` | `~/Music/library` | music folder |
| `RDJ_SOURCE` | `source` | `folder` | `folder` or `navidrome` |
| `RDJ_NAVIDROME_URL` | `navidrome_url` | `http://localhost:4533` | Navidrome server |
| `RDJ_NAVIDROME_USER` | `navidrome_user` | — | Navidrome user |
| `RDJ_NAVIDROME_PASS` | `navidrome_pass` | — | Navidrome password |
| `RDJ_GLM_MODEL` | `glm_model` | `glm-5.2` | model name |
| `RDJ_LLM_PROVIDER` | `llm_provider` | `glm` | preset: `glm`/`openai`/`openrouter`/… |
| `RDJ_VOICE_CMD` | `voice_cmd` | — | raw TTS command (`{text}` / `{out}` placeholders) |
| `RDJ_VOICE_PROVIDER` | `voice_provider` | — | `edge-tts` / `piper` / `say` |
| `RDJ_VOICE` | `voice` | — | voice id for the provider |
| `RDJ_LOCATION` | `location` | `La Paz` | city name (time + weather) |
| `RDJ_LAT` / `RDJ_LON` | `lat` / `lon` | La Paz coords | precise coords for weather |
| `RDJ_LANGUAGE` | `language` | `es` | `es` or `en` (DJ prompts + UI) |
| `RDJ_DJ_TALK` | `dj_talk` | `regular` | `low`/`regular`/`high`/`verbose` — how chatty |
| `RDJ_DJ_EVERY` | `dj_every` | `3` | songs between DJ talks (legacy floor) |
| `RDJ_CHUNK` | `chunk` | `8` | songs per batch (prefetch window) |
| `RDJ_BITRATE` | `bitrate` | `192` | stream bitrate (kbps) |
| `RDJ_STATION_NAME` | `station_name` | `radio-dj` | station name |
| `RDJ_BED` | `bed` | — | instrumental bed for ducking (DJ over music) |
| `RDJ_ICECAST_HOST` | — | `localhost` | icecast host (env only) |
| `RDJ_ICECAST_PORT` | — | `7702` | icecast port (env only) |
| `RDJ_ICECAST_SOURCE_PW` | — | — | icecast source password (env only) |
| `RDJ_STATUS_PORT` | — | `7710` | web UI + API port (env only) |

> **DJ off?** The DJ stays silent when `glm_api_key` is unset or no voice is
> configured. The station still streams music-only.

---

## 🛠️ Architecture

```
radio-dj (Go, ~20–30 MB RAM)
  ├─ supervisor   spawns + watches icecast (child, auto-restart)
  ├─ producer     builds next batch (LLM Director + TTS) via prefetch
  ├─ master ffmpeg (persistent)   PCM → MP3 → icecast, never reconnects
  └─ decoders ffmpeg (per segment)   track/voice → PCM into the master
```

One icecast connection that never drops = zero listener cutouts.

```
  ┌──────────┐   PCM    ┌──────────────┐  MP3   ┌──────────┐
  │ producer │─────────▶│ master ffmpeg │──────▶│ icecast  │──▶ listeners
  └──────────┘          └──────────────┘        └──────────┘
       │                        ▲
       │ TTS voice              │ PCM (fd4)
       ▼                        │
  ┌──────────┐          ┌───────────────┐
  │ LLM + TTS │          │ sidechaincomp │  ← ducks music for voice
  └──────────┘          └───────────────┘
```

---

## 🪶 License

**MIT** — do whatever you want. See [LICENSE](LICENSE).

---

<div align="center">

Made by **[johncrash64](https://www.linkedin.com/in/johncrash64/)** ·
[Report a bug](https://github.com/johncrash64/radio-dj/issues) ·
[Request a feature](https://github.com/johncrash64/radio-dj/issues)

</div>
