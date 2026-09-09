#!/bin/sh
# GPM installer — downloads the latest release binary for your platform.
#
#   curl -fsSL https://raw.githubusercontent.com/tonmoydeb404/gpm/main/install.sh | sh
#
# Override the install location with: GPM_INSTALL_DIR=/some/bin sh install.sh

set -e

REPO="tonmoydeb404/gpm"

say() { printf '%s\n' "$1"; }
die() { printf 'error: %s\n' "$1" >&2; exit 1; }

# --- detect platform -------------------------------------------------------
os=$(uname -s)
case "$os" in
    Darwin) os=darwin ;;
    Linux)  os=linux ;;
    *) die "unsupported OS: $os (gpm ships for macOS and Linux)" ;;
esac

arch=$(uname -m)
case "$arch" in
    x86_64|amd64)  arch=amd64 ;;
    arm64|aarch64) arch=arm64 ;;
    *) die "unsupported architecture: $arch" ;;
esac

url="https://github.com/$REPO/releases/latest/download/gpm_${os}_${arch}.tar.gz"

# --- fetch ------------------------------------------------------------------
tmp=$(mktemp -d) || die "cannot create a temp directory"
trap 'rm -rf "$tmp"' EXIT

say "Downloading gpm ($os/$arch)..."
fetch() {
    if command -v curl >/dev/null 2>&1; then
        curl -fsSL -o "$tmp/gpm.tar.gz" "$url"
    elif command -v wget >/dev/null 2>&1; then
        wget -qO "$tmp/gpm.tar.gz" "$url"
    else
        die "need curl or wget to download"
    fi
}
fetch || die "download failed - is there a published release?
    https://github.com/$REPO/releases"

tar xzf "$tmp/gpm.tar.gz" -C "$tmp"
[ -f "$tmp/gpm" ] || die "archive did not contain a gpm binary"

# --- install ----------------------------------------------------------------
install_binary() {
    mkdir -p "$1"
    mv "$tmp/gpm" "$1/gpm"
    chmod 0755 "$1/gpm"
}

dest="${GPM_INSTALL_DIR:-/usr/local/bin}"
if [ -n "${GPM_INSTALL_DIR:-}" ] || [ -w "$dest" ] || [ "$(id -u)" -eq 0 ]; then
    install_binary "$dest"
elif command -v sudo >/dev/null 2>&1; then
    say "Need permission to write to $dest (sudo)..."
    sudo mkdir -p "$dest"
    sudo mv "$tmp/gpm" "$dest/gpm"
    sudo chmod 0755 "$dest/gpm"
else
    dest="$HOME/.local/bin"
    say "No sudo available - installing to $dest instead."
    install_binary "$dest"
fi

case ":$PATH:" in
    *":$dest:"*) ;;
    *) say "NOTE: $dest is not in your PATH — add it to your shell profile." ;;
esac

say "Installed: $dest/gpm"
say "Run 'gpm --help' to get started."
