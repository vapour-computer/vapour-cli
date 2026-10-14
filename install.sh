#!/bin/sh
# Install the vapour CLI from GitHub releases.
#
#   curl -fsSL https://raw.githubusercontent.com/vapour-computer/vapour-cli/main/install.sh | sh
#
# Environment:
#   VAPOUR_VERSION      version to install, e.g. v0.2.0 (default: latest)
#   VAPOUR_INSTALL_DIR  target directory (default: /usr/local/bin if writable, else ~/.local/bin)

set -eu

REPO="vapour-computer/vapour-cli"
BIN="vapour"

err() { printf 'error: %s\n' "$*" >&2; exit 1; }
info() { printf '%s\n' "$*"; }

have() { command -v "$1" >/dev/null 2>&1; }

if have curl; then
  fetch() { curl -fsSL "$1" -o "$2"; }
  final_url() { curl -fsSL -o /dev/null -w '%{url_effective}' "$1"; }
elif have wget; then
  fetch() { wget -q "$1" -O "$2"; }
  final_url() { wget -q --server-response --spider "$1" 2>&1 | awk '/^  Location: /{u=$2} END{print u}'; }
else
  err "curl or wget is required"
fi

have tar || err "tar is required"

case "$(uname -s)" in
  Linux)  os=linux ;;
  Darwin) os=darwin ;;
  *) err "unsupported OS: $(uname -s) (download a Windows build from https://github.com/$REPO/releases)" ;;
esac

case "$(uname -m)" in
  x86_64|amd64)  arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) err "unsupported architecture: $(uname -m)" ;;
esac

tag="${VAPOUR_VERSION:-}"
if [ -z "$tag" ]; then
  tag="$(final_url "https://github.com/$REPO/releases/latest" | sed 's|.*/tag/||')"
  case "$tag" in v*) ;; *) err "could not determine latest release" ;; esac
fi
case "$tag" in v*) ;; *) tag="v$tag" ;; esac
version="${tag#v}"

archive="${BIN}_${version}_${os}_${arch}.tar.gz"
base="https://github.com/$REPO/releases/download/$tag"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT INT TERM

info "Downloading $BIN $tag ($os/$arch)..."
fetch "$base/$archive" "$tmp/$archive" || err "download failed: $base/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || err "download failed: $base/checksums.txt"

want="$(awk -v f="$archive" '$2==f{print $1}' "$tmp/checksums.txt")"
[ -n "$want" ] || err "no checksum listed for $archive"
if have sha256sum; then
  got="$(sha256sum "$tmp/$archive" | awk '{print $1}')"
elif have shasum; then
  got="$(shasum -a 256 "$tmp/$archive" | awk '{print $1}')"
else
  err "sha256sum or shasum is required to verify the download"
fi
[ "$want" = "$got" ] || err "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp" "$BIN" || err "failed to extract $archive"

dir="${VAPOUR_INSTALL_DIR:-}"
if [ -z "$dir" ]; then
  if [ -w /usr/local/bin ]; then
    dir=/usr/local/bin
  else
    dir="$HOME/.local/bin"
  fi
fi
mkdir -p "$dir" || err "cannot create $dir"
install -m 0755 "$tmp/$BIN" "$dir/$BIN" 2>/dev/null || {
  cp "$tmp/$BIN" "$dir/$BIN" && chmod 0755 "$dir/$BIN"
} || err "cannot write to $dir (set VAPOUR_INSTALL_DIR or re-run with sudo)"

info "Installed $BIN $tag to $dir/$BIN"
case ":$PATH:" in
  *":$dir:"*) ;;
  *) info "Note: $dir is not on your PATH. Add it with:  export PATH=\"$dir:\$PATH\"" ;;
esac
