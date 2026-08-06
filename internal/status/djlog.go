package status

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// DJ log entry kinds. Stable wire codes — the display label is localized
// client-side via the i18n data island (log_kind_*). Shared between the
// producer (radio.logDJ) and the consumer (/dj-log) so the two never drift:
// adding/renaming a kind is a one-place change, not a regex in two files.
const (
	LogKindDJ   = "dj"
	LogKindReq  = "request"
	LogKindTime = "time"
)

// DJLogEntry is one line of the on-air feedback log surfaced at /dj-log.
type DJLogEntry struct {
	T    string `json:"t"`    // HH:MM:SS (local)
	Kind string `json:"kind"` // one of LogKind*
	Text string `json:"text"`
}

// ReadDJLog loads the DJ log as structured entries (on-disk order: oldest first).
// Each line is JSON written by radio.logDJ. Lines that don't parse — e.g. legacy
// "HH:MM:SS [KIND] text" lines from before the JSONL switch — degrade to a
// raw-text entry, so the view never breaks across an upgrade.
func ReadDJLog(path string) []DJLogEntry {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var out []DJLogEntry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var e DJLogEntry
		if json.Unmarshal([]byte(line), &e) == nil && e.Text != "" {
			out = append(out, e)
			continue
		}
		out = append(out, DJLogEntry{Text: line}) // legacy/raw — surface as-is
	}
	return out
}
