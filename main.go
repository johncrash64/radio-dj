// Command radio-dj is a tiny 24/7 personal radio DJ.
//
//	serve                 run the station (picks tracks, talks between them, streams to Icecast)
//	now                   print what's on air
//	download <url>        (stub) fetch new music — jarasch vs radio-dj TBD
//
// Config: env (RDJ_*) > ~/.radio-dj/config.json > defaults. Folder mode runs
// with zero config. Run `serve` once to launch the onboarding wizard.
package main

import (
	"fmt"
	"os"

	"radio-dj/internal/config"
	"radio-dj/internal/install"
	"radio-dj/internal/radio"
)

// version is stamped at build time by scripts/build-release.sh via
// -ldflags "-X main.version=<tag>". Builds that skip it (make build, go build,
// go run) report "dev" — the linker silently drops -X for a symbol that
// doesn't exist, so this var is what makes the release stamp real.
var version = "dev"

func main() {
	cfg := config.Load()
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "serve":
		if err := radio.Serve(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "radio-dj: %v\n", err)
			os.Exit(1)
		}
	case "now":
		radio.PrintNow(cfg)
	case "install":
		if err := install.Install(""); err != nil {
			fmt.Fprintf(os.Stderr, "install: %v\n", err)
			os.Exit(1)
		}
	case "uninstall":
		if err := install.Uninstall(); err != nil {
			fmt.Fprintf(os.Stderr, "uninstall: %v\n", err)
			os.Exit(1)
		}
	case "download":
		// Deferred: decide jarasch (yt-dlp engine) vs a built-in downloader.
		fmt.Println("download: TBD — pending jarasch vs radio-dj decision")
		os.Exit(0)
	case "version", "--version", "-v":
		fmt.Printf("radio-dj %s\n", version)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", os.Args[1])
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Println(`radio-dj — tiny 24/7 personal DJ radio

commands:
  serve              pick tracks, talk between them, stream to Icecast
  now                what's on air right now
  download <url>     (stub) fetch new music
  install            always-on service (macOS launchd · Linux systemd/OpenRC)
  uninstall          remove the always-on service
  version            print the build version

config (three layers, lowest wins):
  env RDJ_*  >  ~/.radio-dj/config.json  >  defaults

  Run 'serve' once — the onboarding wizard writes config.json for you.`)
}
