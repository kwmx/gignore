#!/bin/sh
# Installs the latest gignore release on Linux, macOS, or FreeBSD.
#
#   curl -fsSL https://raw.githubusercontent.com/kwmx/gignore/main/install.sh | sh
#
# Environment variables:
#   GIGNORE_VERSION   release to install, e.g. v1.2.0 (default: latest)
#   GIGNORE_BIN_DIR   install directory (default: /usr/local/bin if writable, else ~/.local/bin)
#   GIGNORE_BASE_URL  download from this URL instead of GitHub releases (for mirrors)
set -eu

REPO="kwmx/gignore"
BIN="gignore"

say() { printf '%s\n' "$*" >&2; }
fail() { say "error: $*"; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "$1 is required"; }

fetch() {
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$1" -o "$2"
	elif command -v wget >/dev/null 2>&1; then
		wget -qO "$2" "$1"
	else
		fail "curl or wget is required"
	fi
}

latest_version() {
	url="https://api.github.com/repos/$REPO/releases/latest"
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL "$url"
	else
		wget -qO- "$url"
	fi | sed -n 's/.*"tag_name": *"\([^"]*\)".*/\1/p' | head -n 1
}

sha256() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | cut -d ' ' -f 1
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | cut -d ' ' -f 1
	else
		sha256 -q "$1"
	fi
}

need tar
need uname

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
linux | darwin | freebsd) ;;
*) fail "unsupported OS: $os (on Windows, use install.ps1 or scoop)" ;;
esac

arch=$(uname -m)
case "$arch" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) fail "unsupported architecture: $arch" ;;
esac

version="${GIGNORE_VERSION:-}"
if [ -z "$version" ]; then
	version=$(latest_version)
	[ -n "$version" ] || fail "could not find the latest release; set GIGNORE_VERSION"
fi
number="${version#v}"

archive="${BIN}_${number}_${os}_${arch}.tar.gz"
base="${GIGNORE_BASE_URL:-https://github.com/$REPO/releases/download/$version}"
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

say "Downloading $BIN $version for $os/$arch"
fetch "$base/$archive" "$tmp/$archive" || fail "download failed: $base/$archive"
fetch "$base/checksums.txt" "$tmp/checksums.txt" || fail "could not download checksums"

want=$(grep " $archive\$" "$tmp/checksums.txt" | cut -d ' ' -f 1)
[ -n "$want" ] || fail "$archive is not listed in checksums.txt"
got=$(sha256 "$tmp/$archive")
[ "$want" = "$got" ] || fail "checksum mismatch for $archive"

tar -xzf "$tmp/$archive" -C "$tmp"

dir="${GIGNORE_BIN_DIR:-}"
if [ -z "$dir" ]; then
	if [ -w /usr/local/bin ]; then
		dir=/usr/local/bin
	else
		dir="$HOME/.local/bin"
	fi
fi
mkdir -p "$dir"
install -m 0755 "$tmp/$BIN" "$dir/$BIN" 2>/dev/null || { cp "$tmp/$BIN" "$dir/$BIN" && chmod 0755 "$dir/$BIN"; }

say "Installed $("$dir/$BIN" version) to $dir/$BIN"
case ":$PATH:" in
*":$dir:"*) ;;
*) say "Add $dir to your PATH to run $BIN from anywhere." ;;
esac
