#!/usr/bin/env bash
#
# radio-dj installer.
#   curl -fsSL https://github.com/johncrash64/radio-dj/raw/master/install.sh | bash
#
# Downloads a prebuilt binary from GitHub Releases when one exists for your
# platform; otherwise builds from source with `go install`. Also installs the
# runtime deps (icecast, ffmpeg, edge-tts) via the system package manager.
#
# Env overrides:
#   RDJ_VERSION      release tag to install (default: latest)
#   RDJ_INSTALL_DIR  where to place the binary (default: $HOME/.local/bin)
set -euo pipefail

REPO="johncrash64/radio-dj"
VERSION="${RDJ_VERSION:-latest}"

red()  { printf '\033[31m! %s\033[0m\n' "$*" >&2; }
green(){ printf '\033[32m✓ %s\033[0m\n' "$*"; }
say()  { printf '→ %s\n' "$*"; }
die()  { red "$*"; exit 1; }

# --- detect platform ---
OS="$(uname -s | tr '[:upper:]' '[:lower:]')"   # darwin | linux
ARCH="$(uname -m)"
case "$ARCH" in
  x86_64|amd64) ARCH="amd64" ;;
  arm64|aarch64) ARCH="arm64" ;;
  *) die "unsupported architecture: $ARCH (radio-dj needs amd64 or arm64)" ;;
esac
[ "$OS" = "darwin" ] || [ "$OS" = "linux" ] || die "unsupported OS: $OS"

INSTALL_DIR="${RDJ_INSTALL_DIR:-$HOME/.local/bin}"

say "installing radio-dj ($OS/$ARCH) → $INSTALL_DIR"

# --- 1. runtime deps ---
install_deps() {
  # icecast + ffmpeg: skip the package-manager step when both are present.
  if command -v icecast >/dev/null && command -v ffmpeg >/dev/null; then
    green "icecast + ffmpeg already installed"
  else
    say "runtime deps (icecast, ffmpeg)"
    case "$OS" in
      darwin)
        command -v brew >/dev/null || die "Homebrew not found — install it from https://brew.sh first"
        brew install icecast ffmpeg
        ;;
      linux)
        if command -v apt-get >/dev/null; then
          sudo apt-get update -qq && sudo apt-get install -y icecast2 ffmpeg
        elif command -v dnf >/dev/null; then
          sudo dnf install -y icecast ffmpeg
        else
          die "install icecast + ffmpeg with your package manager, then re-run this script"
        fi
        ;;
    esac
  fi
  # edge-tts provides the default Spanish (es-CO) voice
  if command -v edge-tts >/dev/null; then
    green "edge-tts already installed"
  else
    say "voice (edge-tts)"
    if command -v pipx >/dev/null; then
      pipx install edge-tts || pipx upgrade edge-tts
    elif command -v pip3 >/dev/null; then
      pip3 install --user edge-tts
    else
      say "(skipped — install later with: pipx install edge-tts)"
    fi
  fi
}

# --- 2. binary: prefer prebuilt release, fall back to building from source ---

# GitHub serves the floating "latest" and a pinned tag from different URL
# shapes:  latest → /releases/latest/download/<asset>
#          v0.5.0 → /releases/download/v0.5.0/<asset>
# Building the pinned form the "latest" way 404s, which is why RDJ_VERSION
# always fell through to the source build.
release_url() {
  if [ "$VERSION" = "latest" ]; then
    echo "https://github.com/$REPO/releases/latest/download/radio-dj-$OS-$ARCH"
  else
    echo "https://github.com/$REPO/releases/download/$VERSION/radio-dj-$OS-$ARCH"
  fi
}

# go.mod declares `module radio-dj`, not the GitHub path, so
# `go install github.com/$REPO@...` fails with a module path mismatch. Clone
# and build instead — same result, and it stamps main.version the way the
# release binaries do.
build_from_source() {
  command -v go  >/dev/null || die "Go not found — install it from https://go.dev/dl/, then re-run"
  command -v git >/dev/null || die "git not found — install it, then re-run"
  local src; src="$(mktemp -d)"
  if [ "$VERSION" = "latest" ]; then
    git clone --depth 1 -q "https://github.com/$REPO" "$src"
  else
    git clone --depth 1 -q --branch "$VERSION" "https://github.com/$REPO" "$src" \
      || { rm -rf "$src"; die "no such tag or branch: $VERSION"; }
  fi
  local stamp; stamp="$(git -C "$src" describe --tags --always 2>/dev/null || echo "$VERSION")"
  ( cd "$src" && go build -trimpath -ldflags="-s -w -X main.version=$stamp" -o "$INSTALL_DIR/radio-dj" . ) \
    || { rm -rf "$src"; die "build failed"; }
  rm -rf "$src"
}

install_binary() {
  mkdir -p "$INSTALL_DIR"
  local url; url="$(release_url)"
  say "trying prebuilt binary: $url"
  local tmp; tmp="$(mktemp)"
  if curl -fsSL "$url" -o "$tmp"; then
    install -m 0755 "$tmp" "$INSTALL_DIR/radio-dj"
    rm -f "$tmp"
    green "binary installed"
  else
    rm -f "$tmp"
    say "no prebuilt binary — building from source"
    build_from_source
    green "built and installed"
  fi
}

# --- 3. next steps ---
next_steps() {
  echo ""
  green "radio-dj is installed"
  echo ""
  echo "  First run (opens the onboarding wizard):"
  echo "    radio-dj serve"
  echo ""
  echo "  Make it always-on (macOS launchd):"
  echo "    radio-dj install"
  echo ""
  case ":$PATH:" in
    *":$INSTALL_DIR:"*) ;;
    *) say "add $INSTALL_DIR to your PATH:  export PATH=\"$INSTALL_DIR:\$PATH\"" ;;
  esac
}

install_deps
install_binary
next_steps
