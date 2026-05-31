#!/bin/sh
# portless-tailscale-proxy installer — downloads the right release binary.
set -eu
REPO="meabed/portless-tailscale-proxy"
BIN="ptp"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"

os=$(uname -s | tr '[:upper:]' '[:lower:]')
arch=$(uname -m)
case "$arch" in
  x86_64|amd64) arch="amd64" ;;
  arm64|aarch64) arch="arm64" ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac
case "$os" in
  darwin|linux) ;;
  *) echo "unsupported OS: $os (Windows: use npm or a GitHub release)" >&2; exit 1 ;;
esac

tag=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name"' | head -1 | cut -d'"' -f4)
[ -n "$tag" ] || { echo "could not resolve latest release tag" >&2; exit 1; }

url="https://github.com/${REPO}/releases/download/${tag}/ptp_${os}_${arch}.tar.gz"
tmp=$(mktemp -d)
echo "Downloading $url"
curl -fsSL "$url" | tar -xz -C "$tmp"
chmod +x "$tmp/$BIN"

if [ -w "$INSTALL_DIR" ]; then
  mv "$tmp/$BIN" "$INSTALL_DIR/$BIN"
else
  echo "Installing to $INSTALL_DIR (sudo)"
  sudo mv "$tmp/$BIN" "$INSTALL_DIR/$BIN"
fi
rm -rf "$tmp"
echo "Installed $BIN to $INSTALL_DIR. Run: $BIN doctor"
