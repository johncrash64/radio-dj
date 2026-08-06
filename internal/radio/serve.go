// Package radio is the 24/7 loop with a PERSISTENT source + prefetch:
//   - ONE ffmpeg master (package icecast) stays connected to Icecast forever.
//   - A producer goroutine builds the NEXT tanda (GLM+qohl voices) while the
//     current one plays, so the master always has PCM to encode → it never
//     starves → icecast never drops the source → no 404 between tandas.
//
// Now-playing is set synchronously when each track starts (no timing drift).
package radio

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"radio-dj/internal/config"
	"radio-dj/internal/dj"
	"radio-dj/internal/i18n"
	"radio-dj/internal/icecast"
	"radio-dj/internal/library"
	"radio-dj/internal/skills"
	"radio-dj/internal/status"
	"radio-dj/internal/supervisor"
	"radio-dj/internal/voice"
)

// Segment is one item fed to the streamer: a DJ voice clip or a music track.
type Segment struct {
	Path     string
	IsVoice  bool
	LiveTime bool          // voice generated at air-time (clock skill) — no pre-baked Path
	Midroll  bool          // voice fires mid-song (~50%), not at the start
	Meta     library.Track // valid when !IsVoice
	Text     string        // DJ speech text — logged at air-time, not build-time
	Req      string        // request text that matched this track — air-time log
}

// djLogPath is set in Serve(); logDJ appends DJ speech, requests and the
// spoken clock to it so /dj-log can surface what aired (the feedback view).
var djLogPath string

func logDJ(kind, text string) {
	if djLogPath == "" {
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	// One JSON object per line (JSONL) — the /dj-log reader parses structured
	// entries instead of regex-matching a human format, so kind/text with
	// arbitrary characters can't break the parse.
	b, _ := json.Marshal(status.DJLogEntry{T: time.Now().Format("15:04:05"), Kind: kind, Text: text})
	if f, err := os.OpenFile(djLogPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		_, _ = f.Write(append(b, '\n'))
		_ = f.Close()
	}
}

// Serve runs the station until fatally errored. The streamer is opened once
// and kept alive across the whole loop.
func Serve(cfg config.Config) error {
	lib, err := library.New(cfg.Source, cfg.Library, cfg.NavidromeURL, cfg.NavidromeUser, cfg.NavidromePass)
	if err != nil {
		return fmt.Errorf("library: %w", err)
	}
	st := status.New(cfg.StateDir, cfg.NeedsSetup(), cfg.IcecastMount)
	st.SetLanguage(cfg.Language)
	djLogPath = filepath.Join(cfg.StateDir, "dj-log.txt") // /dj-log tails this for feedback
	st.ListenAndServeHTTP(cfg.StatusPort)
	log.Printf("[radio-dj] UI :%d · stream :%d/stream.aac · POST /request", cfg.StatusPort, cfg.IcecastPort)

	var djx *dj.DJ
	var vox *voice.Voice
	var pool *skills.Pool
	if cfg.DJEnabled {
		prompts, perr := i18n.Load(cfg.Language)
		if perr != nil {
			log.Printf("[radio-dj] WARN i18n: %v", perr)
		}
		djx = dj.New(cfg.LLMProvider, cfg.GLMBaseURL, cfg.GLMAPIKey, cfg.GLMModel, cfg.StationName, cfg.LocationName, prompts)
		vox = voice.New(cfg.VoiceProvider, cfg.Voice, cfg.VoiceCmd)
		pool = skills.NewPool(cfg.StationName, cfg.LocationName, cfg.Latitude, cfg.Longitude, skills.LoadDir(cfg.StateDir, cfg.Language))
		log.Printf("[radio-dj] DJ on: %s @ %s · every %d · bed=%s",
			cfg.GLMModel, cfg.LocationName, cfg.DJEvery, or(cfg.Bed, "(none)"))
	} else {
		log.Printf("[radio-dj] DJ off (ZAI_API_KEY + RDJ_VOICE_CMD to enable)")
	}

	// Bring up icecast ourselves unless an external one is configured.
	srcPw := cfg.IcecastSourcePW
	if srcPw == "" {
		ic, ierr := supervisor.EnsureIcecast(cfg.StateDir, cfg.IcecastHost, cfg.IcecastPort, cfg.IcecastMount)
		if ierr != nil {
			return fmt.Errorf("ensure icecast: %w", ierr)
		}
		srcPw = ic.SourcePassword()
		st.SetIcecast(fmt.Sprintf("http://%s:%d", cfg.IcecastHost, cfg.IcecastPort), ic.AdminPassword())
		log.Printf("[radio-dj] icecast supervisado (source pw %s…)", srcPw[:8])
	}

	streamer, err := icecast.OpenStreamer(cfg.IcecastHost, cfg.IcecastPort, cfg.IcecastMount, cfg.Encoder, srcPw, cfg.StationName, cfg.Bitrate)
	if err != nil {
		return fmt.Errorf("open streamer: %w", err)
	}
	defer streamer.Close()
	// controls: skip/previous from the player UI. Buffered(1) so rapid clicks
	// coalesce into one pending action; the loop drains it after Play returns.
	controls := make(chan string, 1)
	var controlMu sync.Mutex
	takeControl := func() string {
		controlMu.Lock()
		defer controlMu.Unlock()
		select {
		case action := <-controls:
			return action
		default:
			return ""
		}
	}
	st.SetControlHandler(func(action string) bool {
		controlMu.Lock()
		defer controlMu.Unlock()
		// SkipCurrent kills the in-flight decoder immediately; reject if no
		// decoder is active (between songs) or a skip is already pending.
		if len(controls) > 0 || !streamer.SkipCurrent() {
			return false
		}
		controls <- action
		return true
	})
	st.MarkPlaying(true)
	log.Printf("[radio-dj] source persistente ON AIR ✓")

	// Producer: prefetch tandas so the master never starves.
	prepared := make(chan []Segment, 2)
	go func() {
		tc := 0
		for {
			segs, reqs, berr := buildTanda(cfg, lib, djx, vox, pool, st, &tc)
			if berr != nil {
				log.Printf("[radio-dj] build: %v — retry 10s", berr)
				time.Sleep(10 * time.Second)
				continue
			}
			log.Printf("[radio-dj] tanda lista (%d segmentos%s) — prefetched", len(segs), reqs)
			prepared <- segs
		}
	}()

	// Consumer: play each tanda as it arrives; the next is already being built.
	var previousTrack *Segment
	tandaN := 0
	for segs := range prepared {
		tandaN++
		log.Printf("[radio-dj] ▶ tanda #%d al aire (%d segmentos)", tandaN, len(segs))
		pendingVoicePath := ""
		pendingVoiceText := ""
		pendingMidrollPath := ""
		pendingMidrollText := ""
		pendingLiveTime := false
		for i, seg := range segs {
			if seg.IsVoice {
				switch {
				case seg.LiveTime:
					pendingLiveTime = true // clock skill — voice built at air-time
				case seg.Midroll:
					pendingMidrollPath = seg.Path // fire mid-song (~50%)
					pendingMidrollText = seg.Text
				default:
					pendingVoicePath = seg.Path // overlay over the next song (live ducking)
					pendingVoiceText = seg.Text
				}
				continue
			}
		playCurrent:
			st.SetCurrent(toStatus(seg.Meta), toStatus(nextTrack(segs, i)))
			log.Printf("▶ %s — %s", seg.Meta.Title, seg.Meta.Artist)
			if seg.Req != "" {
				logDJ(status.LogKindReq, seg.Req) // air-time: the requested track starts now
			}
			if pendingLiveTime {
				// clock skill: generate the voice NOW so the hour isn't stale.
				// Song is already playing (ducked via Interject), so GLM+TTS
				// latency (2-5s) hides under it — no dead air.
				pendingLiveTime = false
				go func() {
					text := djx.Say(pool.Prompt("time", map[string]string{"time": time.Now().Format("15:04")}))
					logDJ(status.LogKindTime, text)
					vf, verr := vox.Speak(text)
					if verr != nil {
						log.Printf("[dj] time voice: %v", verr)
						return
					}
					if err := streamer.Interject(vf); err != nil {
						log.Printf("[dj] interject: %v", err)
					}
				}()
			} else if pendingVoicePath != "" {
				vf := pendingVoicePath
				vt := pendingVoiceText
				pendingVoicePath = ""
				pendingVoiceText = ""
				go func() {
					time.Sleep(700 * time.Millisecond) // let the intro land
					logDJ(status.LogKindDJ, vt)        // air-time: the intro overlays the song now
					if err := streamer.Interject(vf); err != nil {
						log.Printf("[dj] interject: %v", err)
					}
				}()
			}
			// midroll: fire at ~50% of the song duration
			if pendingMidrollPath != "" {
				mf := pendingMidrollPath
				mt := pendingMidrollText
				src := seg.Meta.Src
				pendingMidrollPath = ""
				pendingMidrollText = ""
				go func() {
					dur := library.Duration(src).Seconds()
					if dur < 30 {
						return // too short for midroll
					}
					time.Sleep(time.Duration(dur * 0.5 * float64(time.Second)))
					logDJ(status.LogKindDJ, mt)
					if err := streamer.Interject(mf); err != nil {
						log.Printf("[dj] midroll interject: %v", err)
					}
				}()
			}
			perr := streamer.Play(seg.Path)
			control := takeControl()
			if perr != nil && control == "" {
				log.Printf("[radio-dj] segment error: %v", perr)
			}
			if control == "previous" && previousTrack != nil {
				prev := *previousTrack
				log.Printf("[radio-dj] ◀ replay %s — %s", prev.Meta.Title, prev.Meta.Artist)
				st.SetCurrent(toStatus(prev.Meta), toStatus(seg.Meta))
				_ = streamer.Play(prev.Path)
				// a "next" while replaying the previous track returns to the
				// interrupted current track; discard that consumed command.
				_ = takeControl()
				goto playCurrent
			}
			played := seg
			previousTrack = &played
			if !streamer.Alive() {
				log.Printf("[radio-dj] master caído — reabriendo source")
				streamer.Close()
				streamer, err = icecast.OpenStreamer(cfg.IcecastHost, cfg.IcecastPort, cfg.IcecastMount, cfg.Encoder, srcPw, cfg.StationName, cfg.Bitrate)
				if err != nil {
					log.Printf("[radio-dj] reopen failed: %v — retry 5s", err)
					time.Sleep(5 * time.Second)
				}
			}
		}
		// tail voice (e.g. outro with no song after it) — overlay over silence
		if pendingLiveTime {
			go func() {
				text := djx.Say(pool.Prompt("time", map[string]string{"time": time.Now().Format("15:04")}))
				logDJ(status.LogKindTime, text)
				if vf, verr := vox.Speak(text); verr == nil {
					_ = streamer.Interject(vf)
				}
			}()
		} else if pendingVoicePath != "" {
			logDJ(status.LogKindDJ, pendingVoiceText)
			if err := streamer.Interject(pendingVoicePath); err != nil {
				log.Printf("[dj] interject: %v", err)
			}
		}
	}
	return nil
}

// buildTanda returns the ordered segments for one batch (requested songs
// first, then fresh picks), with DJ voice intros interleaved. Voices are
// generated here (GLM+qohl) — called by the producer ahead of playback.
func buildTanda(cfg config.Config, lib library.Library, djx *dj.DJ, vox *voice.Voice, pool *skills.Pool, st *status.Server, trackCount *int) (segs []Segment, reqs string, err error) {
	addVoice := func(text string, midroll bool) {
		if !cfg.DJEnabled || vox == nil || strings.TrimSpace(text) == "" {
			return
		}
		if vf, verr := vox.Speak(text); verr == nil {
			segs = append(segs, Segment{Path: vf, IsVoice: true, Text: text, Midroll: midroll})
			log.Printf("[dj] %s", text) // stderr (debug); the air-time log fires in the consumer
		} else {
			log.Printf("[dj] voice: %v", verr)
		}
	}
	addTrack := func(t library.Track) {
		segs = append(segs, Segment{Path: t.Src, Meta: t})
	}

	matched := 0
	var reqCtx []dj.Req
	for _, req := range st.DrainRequests() {
		ms, _ := lib.Search(req.Text)
		if len(ms) > 0 {
			t := ms[0]
			if cfg.DJEnabled {
				addVoice(skills.RequestAck(djx, t, req.From, req.Text), false)
			}
			addTrack(t)
			lib.MarkPlayed(t.Src)
			reqCtx = append(reqCtx, dj.Req{From: req.From, Query: req.Text, Title: t.Title, Artist: t.Artist})
			matched++
			who := req.Text
			if req.From != "" {
				who = req.From + ": " + req.Text
			}
			log.Printf("[request] %q → %s — %s", who, t.Title, t.Artist)
			segs[len(segs)-1].Req = fmt.Sprintf("%q → %s — %s", who, t.Title, t.Artist)
		} else {
			log.Printf("[request] no match %q", req.Text)
		}
	}

	// DJ Director: one structured GLM call plans the whole setlist + talk breaks.
	// The LLM picks+orders a coherent arc from a shortlist and decides WHEN to
	// talk (intro/trivia/wiki/history/time/none), modulated by cfg.DJTalk.
	// On any failure → random fallback so the station never stops.
	if cfg.DJEnabled && djx != nil {
		cands := lib.Sample(12)
		if len(cands) > 0 {
			ctx := dj.Ctx{
				Talk:       cfg.DJTalk,
				TimeOfDay:  timeOfDay(time.Now()),
				History:    histCands(st.Current().History),
				Candidates: libCands(cands),
				Requests:   reqCtx,
			}
			if plan, perr := djx.DirectPlan(ctx); perr == nil {
				bm := map[int][]dj.Break{}
				for _, b := range plan.Breaks {
					bm[b.Before] = append(bm[b.Before], b)
				}
				for pos, id := range plan.Setlist {
					for _, b := range bm[pos] {
						switch {
						case b.Kind == "time":
							segs = append(segs, Segment{IsVoice: true, LiveTime: true})
						case b.Kind == "none" || b.Kind == "":
							// skip
						case b.At == "mid":
							addVoice(djx.SayMidroll(cands[id].Title, cands[id].Artist), true)
						case b.Kind == "wiki":
							addVoice(djx.SayWiki(cands[id].Artist, cands[id].Title), false)
						default:
							addVoice(djx.Banter(cands[id].Title, cands[id].Artist, cands[id].Album), false)
						}
					}
					addTrack(cands[id])
					*trackCount++
				}
				// commit only the chosen tracks; the rest stay available next tanda
				for _, id := range plan.Setlist {
					lib.MarkPlayed(cands[id].Src)
				}
			} else {
				log.Printf("[dj] director falló (%v) — random fallback", perr)
				for i := 0; i < cfg.Chunk; i++ {
					if t, e := lib.Next(); e == nil {
						addTrack(t)
						*trackCount++
					}
				}
			}
		}
	} else {
		for i := 0; i < cfg.Chunk; i++ {
			if t, e := lib.Next(); e == nil {
				addTrack(t)
				*trackCount++
			}
		}
	}

	if len(segs) == 0 {
		return nil, "", fmt.Errorf("no segments")
	}
	if matched > 0 {
		reqs = fmt.Sprintf(", %d pedido(s)", matched)
	}
	return segs, reqs, nil
}

func nextTrack(segs []Segment, from int) library.Track {
	for j := from + 1; j < len(segs); j++ {
		if !segs[j].IsVoice {
			return segs[j].Meta
		}
	}
	return library.Track{}
}

func toStatus(t library.Track) status.Track {
	d := 0.0
	// Duration only for local files — ffprobe on a Navidrome stream URL is a
	// slow network probe we don't want on every SetCurrent.
	if t.Src != "" && !strings.Contains(t.Src, "://") {
		d = library.Duration(t.Src).Seconds()
	}
	return status.Track{Title: t.Title, Artist: t.Artist, Album: t.Album, Year: t.Year, BPM: t.BPM, Duration: d, Src: t.Src}
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// libCands / histCands map library + status tracks into the director's Cand
// shape (the director only knows its own types — dj is decoupled from
// library/status to avoid import cycles).
func libCands(ts []library.Track) []dj.Cand {
	out := make([]dj.Cand, len(ts))
	for i, t := range ts {
		out[i] = dj.Cand{ID: i, Title: t.Title, Artist: t.Artist, Album: t.Album}
	}
	return out
}

func histCands(ts []status.Track) []dj.Cand {
	out := make([]dj.Cand, len(ts))
	for i, t := range ts {
		out[i] = dj.Cand{Title: t.Title, Artist: t.Artist, Album: t.Album}
	}
	return out
}

// timeOfDay returns a coarse ES time-of-day tag for the director's context.
func timeOfDay(t time.Time) string {
	switch h := t.Hour(); {
	case h < 6:
		return "madrugada"
	case h < 12:
		return "mañana"
	case h < 19:
		return "tarde"
	default:
		return "noche"
	}
}
