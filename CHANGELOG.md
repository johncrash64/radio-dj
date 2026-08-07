# Changelog

All notable changes to this project are documented here.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.5.0] — 2026-08-07

### Added
- **Skip & previous controls for the live broadcast.** The player grows `◀◀` /
  `▶▶` transport keys, backed by a new `POST /control` endpoint:
  - `{"action":"next"}` skips forward, `{"action":"previous"}` replays the last
    track. `202` when accepted, `409` when nothing is on air (between songs),
    `400` on an invalid action.
  - **Listeners never drop.** `SkipCurrent()` kills only the in-flight music
    decoder and leaves the master Icecast source untouched — the filtergraph
    just stops receiving new PCM, so the connection stays up and the stream
    keeps flowing. The radio loop then advances to the next track or replays
    the previous one.
  - **It is a shared broadcast** — a skip changes what *every* listener hears,
    not just the person who pressed the key.
  - Rapid clicks coalesce into one pending action. The keys latch down until
    the track actually changes, like a deck key staying engaged while it works,
    then spring back — with a 4s safety timer so a key can never stick, an
    interlock so `◀◀`/`▶▶` release each other, and Stop popping both.
- **`radio-dj version`.** Release builds report their tag, development builds
  report `dev`. `scripts/build-release.sh` had been injecting
  `-X main.version=<tag>` into a symbol that did not exist — the linker drops
  `-X` for an absent symbol, so the stamp was a silent no-op and no build could
  say what it was.

### Fixed
- **Two data races in the control path**, both reachable from an HTTP goroutine
  and invisible without `-race`:
  - `Server.controlHandler` was written by the radio loop and read by the
    `/control` handler with no synchronization. The loop wires it only once the
    streamer is up — seconds after `ListenAndServeHTTP` is already serving — so
    the window was wide rather than theoretical.
  - The control closure captures `streamer`, which the loop reassigns when the
    master dies and is reopened: a plain write racing a plain read. The
    close+reopen now happens under the same mutex the handler takes.
- **`SkipCurrent` could panic an HTTP request.** A failed reopen leaves the
  loop's streamer nil, and this is the one `Streamer` method reachable from a
  request; it now returns false on a nil receiver. The underlying reopen bug is
  untouched — separate debt.
- **`install.sh` could not install a pinned version.** GitHub serves the
  floating `latest` from `/releases/latest/download/<asset>` but a pinned tag
  from `/releases/download/<tag>/<asset>`. The script built the pinned form the
  `latest` way, so `RDJ_VERSION=v0.4.0` returned 404.
- **`install.sh`'s build-from-source fallback never worked.** `go.mod` declares
  `module radio-dj`, not the GitHub path, so
  `go install github.com/johncrash64/radio-dj@…` failed with a module path
  mismatch. It now clones the tag and builds. Combined with the 404 above the
  two compounded: a pinned install 404'd, fell through to the source build, and
  the source build failed too — only the default `curl | bash` path worked.
- **Music-only stations could not open the player UI.**

### Known limitations
- `previous` on the first track after startup behaves as `next` — there is no
  previous track recorded yet.
- A skip during a `previous` replay is consumed as "return to the interrupted
  track".
- Returning from a replay re-emits the request DJ-log line for that track.
- There is no client-side reconnect, so a transport key springs back when the
  metadata changes — a buffer-depth ahead of what the listener actually hears
  (≈ the Icecast burst during continuous play, more after a pause/resume).

## [0.4.0] — 2026-08-06

### Added
- **Configurable, cross-platform broadcast encoder.** The audio encoder is now
  platform-aware and overridable via `RDJ_ENCODER`:
  - **macOS** → `aac_at` (Apple AudioToolbox, **hardware-accelerated**) — ~128×
    realtime, dramatically lower CPU than software MP3.
  - **Linux / Windows** → `aac` (ffmpeg's built-in, always present — no extra lib).
  - **Override:** `RDJ_ENCODER=libmp3lame` (or any ffmpeg audio encoder).
- New `internal/codec` package — the single source of truth mapping an encoder
  to its stream metadata (muxer, content-type, mount suffix). The icecast
  `<mount-name>`, the reverse-proxy route, and the web `<audio>` src all derive
  from it, so the player can never desync from the stream.

### Changed
- **Default stream mount is now `/stream.aac`** (was `/stream.mp3`). The mount
  follows the encoder (`aac*` → `.aac`, `libmp3lame` → `.mp3`). Listeners with a
  hardcoded `/stream.mp3` URL: either update it, or set `RDJ_ENCODER=libmp3lame`
  to keep MP3.
- **Default bitrate stays 192 kbps.** AAC at 192 kbps transparently beats MP3 at
  the same rate; lower it with `RDJ_BITRATE` if you prefer.

### Fixed
- **Web player could 404 after an encoder switch.** The `<audio>` src was
  hardcoded to `stream.mp3` in the page JS, so it desynced from the reverse
  proxy whenever the mount changed. It now follows `{{.StreamPath}}` from config.

### Performance
- The always-on broadcast encoder is **~1.6× faster** on Apple Silicon
  (`aac_at` 128× vs `libmp3lame` ~78× realtime). Note: the live DJ ducking
  filtergraph (`sidechaincompress` + `amix`) remains the bulk of the master
  process CPU by design — that's the cost of real-time voice-over ducking.

## [0.3.1]

- UI: render legacy dj-log lines without stray bracket.
