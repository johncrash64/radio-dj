// Package icecast runs ONE persistent ffmpeg "master" encoder connected to
// Icecast 24/7 with LIVE DUCKING. Two PCM inputs feed one filtergraph:
//
//	pipe:3 = music (songs, paced by -re per segment)
//	pipe:4 = voice (silence when idle, DJ banter on interject)
//
//	sidechaincompress ducks the music whenever voice is present, amix overlays
//
// the voice on top — so the DJ can talk OVER a song mid-playback without
// stopping it. Mirrors how a hardware radio mixer's ducking bus works.
package icecast

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"

	"radio-dj/internal/codec"
)

// chunkBytes = 100ms of s16le 44100Hz stereo PCM = 44100*2*2*0.1.
const chunkBytes = 17640

// Streamer holds the persistent master + the two pipe write ends. The voice
// pipe is owned by a feeder goroutine that paces silence/voice at real-time.
type Streamer struct {
	master    *exec.Cmd
	w         *os.File // music PCM (fd 3)
	vw        *os.File // voice PCM (fd 4)
	voiceQ    chan []byte
	done      chan struct{}
	mu        sync.Mutex // guards vw writes
	decoderMu sync.Mutex // guards decoder
	decoder   *exec.Cmd  // in-flight music decoder, nil between songs
	ffmpeg    string     // resolved ffmpeg binary (launchd has a minimal PATH)
}

// OpenStreamer starts the master with the ducking filtergraph. Both inputs are
// live pipes; the caller feeds music via Play and voice via Interject.
// findFFmpeg locates the ffmpeg binary: PATH first, then the usual Homebrew
// locations. launchd (and other minimal-PATH daemon contexts) miss
// /opt/homebrew/bin, so the master/Play/Interject must resolve it explicitly.
func findFFmpeg() (string, error) {
	if bin, err := exec.LookPath("ffmpeg"); err == nil {
		return bin, nil
	}
	for _, p := range []string{
		"/opt/homebrew/bin/ffmpeg", // macOS Apple Silicon
		"/usr/local/bin/ffmpeg",    // macOS Intel / manual install
		"/opt/homebrew/opt/ffmpeg/bin/ffmpeg",
		"/usr/bin/ffmpeg", // Linux
	} {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("ffmpeg binary not found — install it (macOS: `brew install ffmpeg`)")
}

func OpenStreamer(host string, port int, mount, encoder, sourcePw, name string, bitrate int) (*Streamer, error) {
	ffmpegBin, err := findFFmpeg()
	if err != nil {
		return nil, err
	}
	// [0:a]=music, [1:a]=voice. sidechaincompress ducks music on voice; amix
	// overlays voice. release=600ms = smooth fade back up after the DJ stops.
	m := codec.MetaFor(encoder)
	filter := "[0:a][1:a]sidechaincompress=threshold=0.015:ratio=12:attack=5:release=600[d];" +
		"[d][1:a]amix=inputs=2:duration=first:normalize=0:weights=1 1.3[a]"
	master := exec.Command(ffmpegBin,
		"-loglevel", "warning",
		"-f", "s16le", "-ar", "44100", "-ac", "2", "-i", "pipe:3",
		"-f", "s16le", "-ar", "44100", "-ac", "2", "-i", "pipe:4",
		"-filter_complex", filter,
		"-map", "[a]",
		"-c:a", m.Encoder, "-b:a", strconv.Itoa(bitrate)+"k", "-ar", "44100", "-ac", "2",
		"-content_type", m.ContentType, "-f", m.Format, "-ice_name", name,
		fmt.Sprintf("icecast://source:%s@%s:%d%s", sourcePw, host, port, mount),
	)
	mr, mw, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("music pipe: %w", err)
	}
	vr, vw, err := os.Pipe()
	if err != nil {
		mr.Close()
		mw.Close()
		return nil, fmt.Errorf("voice pipe: %w", err)
	}
	master.ExtraFiles = []*os.File{mr, vr} // → fd 3 (music), fd 4 (voice)
	master.Stderr = os.Stderr
	if err := master.Start(); err != nil {
		mr.Close()
		mw.Close()
		vr.Close()
		vw.Close()
		return nil, fmt.Errorf("master ffmpeg: %w", err)
	}
	s := &Streamer{
		master: master, w: mw, vw: vw,
		voiceQ: make(chan []byte, 8),
		done:   make(chan struct{}),
		ffmpeg: ffmpegBin,
	}
	go s.voiceFeeder()
	return s, nil
}

// voiceFeeder owns the voice pipe. Each 100ms tick it writes 100ms of audio:
// voice frames if any are queued, otherwise silence (zeros). Pacing at
// real-time keeps the filtergraph synced with the -re-paced music input.
func (s *Streamer) voiceFeeder() {
	silence := make([]byte, chunkBytes)
	var voice []byte
	t := time.NewTicker(100 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case v := <-s.voiceQ:
			voice = append(voice, v...)
		case <-t.C:
			s.mu.Lock()
			switch {
			case len(voice) >= chunkBytes:
				_, _ = s.vw.Write(voice[:chunkBytes])
				voice = voice[chunkBytes:]
			case len(voice) > 0:
				_, _ = s.vw.Write(voice)
				voice = nil
			default:
				_, _ = s.vw.Write(silence)
			}
			s.mu.Unlock()
		}
	}
}

// Play decodes one music segment to PCM (paced by -re) and writes it to the
// music pipe. Music-only now — ducking is live via the voice input. The
// decoder is registered so SkipCurrent can kill it; it is cleared on return
// whether the song ended naturally or was skipped.
func (s *Streamer) Play(segment string) error {
	dec := exec.Command(s.ffmpeg,
		"-loglevel", "error", "-re", "-i", segment,
		"-f", "s16le", "-ar", "44100", "-ac", "2", "pipe:1")
	dec.Stdout = s.w
	dec.Stderr = os.Stderr
	if err := dec.Start(); err != nil {
		return err
	}
	s.setDecoder(dec)
	defer s.setDecoder(nil)
	return dec.Wait()
}

// SkipCurrent kills the in-flight music decoder WITHOUT touching the master
// Icecast source — connected listeners stay on, the live stream keeps flowing
// (the filtergraph just stops receiving new music PCM). Play() returns and the
// radio loop advances to the next/previous track. Returns false when no
// decoder is active (nothing to skip).
func (s *Streamer) SkipCurrent() bool {
	// nil receiver: a failed reopen leaves the loop's streamer nil, and this is
	// the one method reachable from an HTTP goroutine — never panic a request.
	if s == nil {
		return false
	}
	s.decoderMu.Lock()
	defer s.decoderMu.Unlock()
	if s.decoder == nil || s.decoder.Process == nil {
		return false
	}
	_ = s.decoder.Process.Kill()
	return true
}

func (s *Streamer) setDecoder(c *exec.Cmd) {
	s.decoderMu.Lock()
	s.decoder = c
	s.decoderMu.Unlock()
}

// Interject decodes a voice file to PCM and queues it to the feeder — the
// master ducks the music and overlays the voice in real-time. Non-blocking:
// returns once queued. Safe to call while a song is playing.
func (s *Streamer) Interject(voiceFile string) error {
	pcm, err := exec.Command(s.ffmpeg,
		"-loglevel", "error", "-i", voiceFile,
		"-f", "s16le", "-ar", "44100", "-ac", "2", "pipe:1").Output()
	if err != nil {
		return fmt.Errorf("voice decode: %w", err)
	}
	if len(pcm) == 0 {
		return fmt.Errorf("voice decode produced no PCM")
	}
	select {
	case s.voiceQ <- pcm:
		return nil
	default:
		return fmt.Errorf("voice queue full — interject skipped")
	}
}

// Alive reports whether the master is still running.
func (s *Streamer) Alive() bool {
	return s.master.ProcessState == nil
}

// Close shuts the master + feeder down cleanly.
func (s *Streamer) Close() {
	close(s.done)
	_ = s.w.Close()
	_ = s.vw.Close()
	_ = s.master.Wait()
}
