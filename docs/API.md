# radio-dj — HTTP API

radio-dj exposes a small HTTP API on the **status port** (default `:7710`) and a
broadcast stream on the **Icecast port** (default `:7702`). Everything a client
needs — a mobile app, PanelHUD, Home Assistant, a hardware radio — is HTTP.

## Base URLs

| Service | URL |
|---|---|
| API + UI | `http://<host>:7710` |
| Audio stream | `http://<host>:7702/stream.aac` |

---

## Endpoints

### `GET /now-playing`
The primary feed. Poll every ~5s.

```json
{
  "current":  { "title": "Midnight City", "artist": "M83", "album": "Hurry Up, We're Dreaming" },
  "next":     { "title": "Open Eye Signal", "artist": "Jon Hopkins" },
  "requests": [ { "text": "play some Bowie", "askedAt": "2026-07-28T22:00:00Z", "status": "queued" } ],
  "playing":  true,
  "startedAt":"2026-07-28T22:00:00.000Z"
}
```
- `next` is `null` when nothing is queued.
- `requests` is the unresolved listener queue.

### `POST /request`
Submit a free-text song request. Resolved at the next batch boundary (searches
the library for a title/artist match).

```
POST /request
Content-Type: application/json
{ "text": "play some Bowie" }

→ 200  { "text": "play some Bowie", "askedAt": "...", "status": "queued" }
```

### `POST /control`
Skip or replay the current track on the live broadcast. The in-flight music
decoder is killed (listeners stay connected — only the decoder stops feeding
PCM, the master Icecast source keeps running); the radio loop then advances to
the next track (`next`) or replays the previous one (`previous`).

```
POST /control
Content-Type: application/json
{ "action": "next" }          // or "previous"

→ 202 {"ok":true}             // accepted, decoder killed, loop advances
→ 409 {"error":"no track playing"}  // nothing on air (between songs)
→ 400 {"error":"action must be 'previous' or 'next'"}
```

Rapid clicks coalesce into one pending action (the channel is buffered to 1).
It's a shared broadcast: the skip changes what **all** listeners hear.

### `GET /stream.aac`
The broadcast — an AAC stream (default 192 kbps, 44.1 kHz, stereo). The encoder is
platform-aware: `aac_at` (Apple AudioToolbox, hardware-accelerated) on macOS,
ffmpeg's built-in `aac` elsewhere; override with `RDJ_ENCODER` (e.g.
`libmp3lame` → `/stream.mp3`). Point any `<audio>` element, VLC, Sonos, or car
receiver here. One shared stream: every listener hears the same thing at the
same time (it's radio, not on-demand).

### `GET /listen.pls` · `GET /listen.m3u`
Playlist wrappers around the stream for players that prefer a playlist file
(Sonos, VLC, hardware radios).

### `GET /health`
Liveness probe. `200 {"status":"on-air"}`.

### `GET /` (UI)
The neo-brutalist player UI. On first run (no AI key / voice configured) it
redirects to `/onboarding`.

### `GET /onboarding` · `POST /onboarding`
First-run wizard. `POST` writes `config.json` (library, AI key, location, voice,
optional Navidrome). radio-dj reads it on next start; env vars still override.

---

## Auth
None by default — radio-dj is intended for personal/LAN use. For public
exposure, put it behind a reverse proxy (Caddy/nginx/Cloudflare) with auth, and
consider Icecast listener-auth on the stream mount.

---

## Integration recipes

### Mobile app (iOS/Android, any)
- Audio: `<audio src="http://<host>:7702/stream.aac">` (or a native player).
- Metadata: poll `GET /now-playing` every 5s, render title/artist + cover.
- Requests: `POST /request` from a text field.

### PanelHUD / dashboard (Swift, notch widget, etc.)
Poll `/now-playing` every 5s → show current + next in the notch. Wire a "request"
action to `POST /request`. Example (Swift-ish):
```swift
let np = try await JSONDecoder().decode(NowPlaying.self,
    from: URLSession.shared.data(from: URL(string: "http://localhost:7710/now-playing")!).0)
// np.current.title, np.next?.title, np.requests
```

### Home Assistant / Home Assistant sensor
A REST sensor polling `/now-playing`, with a RESTful command for `/request`.

### VLC / Sonos / car
Open `http://<host>:7702/listen.m3u` (or the `.pls`).

---

## Stream details
- Format: MP3, 192 kbps, 44.1 kHz, stereo (configurable via `RDJ_BITRATE`).
- The source is **persistent** — one encoder connected to Icecast 24/7, so the
  stream never drops between tracks. Listeners may sit ~22s behind the live edge
  due to Icecast's burst buffer (normal for internet radio).

## OpenAPI
A machine-readable spec is at [`openapi.yaml`](openapi.yaml) — import into
Postman/Insomnia or generate a client.
