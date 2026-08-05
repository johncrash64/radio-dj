package status

import (
	_ "embed"
	"net/http"
	"text/template"
)

// indexPage — neo-brutalist player: thick borders, hard offset shadows, bold
// blocky sections, pastel fills with dark frames. Cassette has real reel
// windows (spinning) + tape strip — no fake "flow" animation. Polls
// /now-playing (current + next + requests), POSTs /request. No deps.

//go:embed templates/index.html
var indexHTML string

//go:embed templates/permanent-marker.woff2
var markerFont []byte

//go:embed templates/manifest.json
var manifestJSON []byte

//go:embed templates/sw.js
var swJS []byte

//go:embed templates/icon.svg
var iconSVG []byte

//go:embed templates/icon-192.png
var icon192 []byte

//go:embed templates/icon-512.png
var icon512 []byte

//go:embed templates/favicon-32.png
var favicon32 []byte

//go:embed templates/apple-touch-icon.png
var appleTouchIcon []byte

var indexTmpl = template.Must(template.New("index").Parse(indexHTML))

// indexData is the template context for index.html: only the server-default
// UI language, which the client may still override via ?lang=.
type indexData struct {
	Lang string
}

func serveIndex(w http.ResponseWriter, lang string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = indexTmpl.Execute(w, indexData{Lang: lang})
}

// serveFont serves the self-hosted Permanent Marker woff2 (30KB) with a
// 1-year immutable cache — same origin, works offline, no FOUT.
func serveFont(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "font/woff2")
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	_, _ = w.Write(markerFont)
}

// serveStatic returns an immutable asset with the right content-type.
func serveStatic(ct string, b []byte) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", ct)
		w.Header().Set("Cache-Control", "public, max-age=604800")
		_, _ = w.Write(b)
	}
}

// registerPWA wires the manifest, service worker, icons, and favicon routes.
func registerPWA(mux *http.ServeMux) {
	mux.HandleFunc("/manifest.json", serveStatic("application/manifest+json", manifestJSON))
	mux.HandleFunc("/sw.js", serveStatic("application/javascript", swJS))
	mux.HandleFunc("/icon.svg", serveStatic("image/svg+xml", iconSVG))
	mux.HandleFunc("/icon-192.png", serveStatic("image/png", icon192))
	mux.HandleFunc("/icon-512.png", serveStatic("image/png", icon512))
	mux.HandleFunc("/favicon-32.png", serveStatic("image/png", favicon32))
	mux.HandleFunc("/apple-touch-icon.png", serveStatic("image/png", appleTouchIcon))
}
