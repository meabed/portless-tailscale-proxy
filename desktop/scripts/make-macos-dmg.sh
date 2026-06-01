#!/usr/bin/env bash
# Wrap the built binary in a Tailscale Proxy.app bundle and produce a .dmg.
# Usage: make-macos-dmg.sh <version> <binary> <out.dmg>
set -euo pipefail

VERSION="${1:?version required}"
BIN="${2:?binary path required}"
OUT="${3:?output dmg path required}"

APP="Tailscale Proxy.app"
rm -rf "$APP" dmgroot
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources"
cp "$BIN" "$APP/Contents/MacOS/tailscale-proxy"
chmod +x "$APP/Contents/MacOS/tailscale-proxy"

cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key><string>Tailscale Proxy</string>
  <key>CFBundleDisplayName</key><string>Tailscale Proxy</string>
  <key>CFBundleIdentifier</key><string>com.meabed.tailscale-proxy</string>
  <key>CFBundleExecutable</key><string>tailscale-proxy</string>
  <key>CFBundleVersion</key><string>${VERSION}</string>
  <key>CFBundleShortVersionString</key><string>${VERSION}</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>LSMinimumSystemVersion</key><string>10.15</string>
  <key>LSUIElement</key><true/>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

# Stage the .app next to an /Applications symlink so the dmg offers drag-install.
mkdir -p dmgroot
cp -R "$APP" dmgroot/
ln -s /Applications dmgroot/Applications

hdiutil create -volname "Tailscale Proxy" -srcfolder dmgroot -ov -format UDZO "$OUT"
echo "built $OUT"
