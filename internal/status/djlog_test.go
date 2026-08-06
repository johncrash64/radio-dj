package status

import (
	"encoding/json"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadDJLog(t *testing.T) {
	p := filepath.Join(t.TempDir(), "dj-log.txt")
	// mix of new JSONL and legacy "HH:MM:SS [KIND] text" lines
	body := strings.Join([]string{
		`{"t":"10:00:01","kind":"dj","text":"Welcome aboard"}`,
		`{"t":"10:00:02","kind":"request","text":"Play \"Hotel California\" 'ne"}`,
		`10:00:03 [HORA] legacy line with 'apostrophe`, // legacy → raw
		`{"t":"","kind":"","text":""}`,                 // empty text → raw (text=="" fallback)
		``,                                             // blank → skipped
		`not json at all`,
	}, "\n") + "\n"
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got := ReadDJLog(p)
	if len(got) != 5 { // 4 non-blank + the empty-text one becomes raw{""} = 5
		t.Fatalf("want 5 entries, got %d: %+v", len(got), got)
	}
	if got[0].Text != "Welcome aboard" || got[0].Kind != "dj" {
		t.Errorf("entry0 mismatch: %+v", got[0])
	}
	// entry with embedded double-quote and apostrophe survives intact
	if !strings.Contains(got[1].Text, `Hotel California`) || !strings.Contains(got[1].Text, `'ne`) {
		t.Errorf("entry1 text mangled: %q", got[1].Text)
	}
	// legacy line degrades to raw text (no crash, no parse)
	if got[2].Kind != "" || !strings.Contains(got[2].Text, "legacy") {
		t.Errorf("legacy entry not raw-wrapped: %+v", got[2])
	}
}

// TestIndexIslandRendersSafeJSON renders the real index template with
// html/template and proves the #i18n island is valid JSON whose strings
// survive verbatim (apostrophes, quotes) and can't break out of the script
// block. This is the regression guard for the bug class PR #4 patched by hand.
func TestIndexIslandRendersSafeJSON(t *testing.T) {
	raw := `<script type="application/json" id="i18n">{{.I18n}}</script>`
	tmpl := template.Must(template.New("i").Parse(raw))
	ui := map[string]string{
		"ui_no_djlog": "The DJ hasn't spoken yet…",
		"ui_play":     `He said "hi"`,
		"breakout":    "</script><script>alert(1)",
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, map[string]any{"I18n": ui}); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	// the only literal </script> must be the island's own closing tag
	if c := strings.Count(out, "</script>"); c != 1 {
		t.Fatalf("want exactly 1 </script> (the closer), got %d: %s", c, out)
	}
	// extract the JSON payload between the tags
	body := strings.TrimSuffix(strings.TrimPrefix(out, `<script type="application/json" id="i18n">`), "</script>")
	var got map[string]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatalf("island not valid JSON: %v\n%s", err, body)
	}
	if got["ui_no_djlog"] != "The DJ hasn't spoken yet…" {
		t.Errorf("apostrophe string lost: %q", got["ui_no_djlog"])
	}
	if got["ui_play"] != `He said "hi"` {
		t.Errorf("quoted string lost: %q", got["ui_play"])
	}
}
