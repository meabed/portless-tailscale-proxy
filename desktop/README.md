# Tailscale Proxy — desktop app

A tray-first desktop wrapper around the `tsp` engine. It drives
[`core.Controller`](../core/controller.go) **in-process** (no sidecar), so the menu
bar can start/stop the proxy, switch Funnel/Serve, open service URLs, toggle
start-at-login, and edit the shared `~/.tailscale-proxy/config.json`.

> **Download a build:** [Releases](https://github.com/meabed/tailscale-proxy/releases)
> (newest `desktop-v…`) · [docs page](https://tailscaleproxy.vercel.app/desktop).

Built with [Wails v3](https://v3alpha.wails.io) (Go + native webview). Separate Go
module so the CLI module stays dependency-free; it imports `core` via a local
`replace` directive.

## What it does

Clicking the menu-bar item drops down a **webview panel** (a small dark UI served
on loopback, not a native menu):

- **Status** — a dot + your node name; **Start / Stop** button.
- **Public (Funnel) ↔ Private (Serve)** segmented toggle — persists to the config
  and re-exposes live.
- **Services list** — every discovered service with its runtime badge, port, and an
  open-in-browser button.
- **Start at login** switch — per-OS autostart (LaunchAgent / `.desktop` / HKCU Run).
- **Open config file**, **Documentation**, **Quit**.
- Auto-starts the proxy on launch; macOS shows no Dock icon (tray-first).

The app and the `tsp` CLI share the same config file, so changes in one show up in
the other.

### How the panel works

`main.go` starts a tiny HTTP server on `127.0.0.1:<random>` that serves
`assets/panel.html` and a token-gated JSON API (`/api/status`, `/api/toggle`,
`/api/mode`, `/api/autostart`, `/api/open`, `/api/quit`). A frameless Wails webview
window loads it and is attached to the tray. The per-session token (injected into
the HTML) blocks other local processes/browsers from driving it.

## Run it (dev)

Requires Go 1.25+ and a C toolchain (Xcode CLT on macOS; WebKitGTK + libgtk dev
packages on Linux). From this directory:

```bash
go build -o tsp-app .   # builds a native binary (CGO links the system webview)
./tsp-app               # launches the menu-bar app
```

`go run .` works too. The proxy needs Tailscale set up exactly like the CLI — run
`tsp doctor` (or the CLI) first if the menu shows it stopped with an error.

## Release builds

Installers are produced by the **`desktop-release`** GitHub workflow
([`.github/workflows/desktop-release.yml`](../.github/workflows/desktop-release.yml)).
Run it from the Actions tab with a version (e.g. `0.1.0`); it builds on macOS
(arm64 + Intel), Windows, and Linux runners, packages each
(`make-macos-dmg.sh` → `.dmg`, windowsgui `.exe` → `.zip`, Linux `.tar.gz`), and
publishes a `desktop-v<version>` GitHub release.

Locally on one platform you can also `go build` (above) or use the Wails toolchain
(`go install github.com/wailsapp/wails/v3/cmd/wails3@latest && wails3 build`).

Builds are currently **unsigned** — code-signing + notarization (macOS) and an
Authenticode cert (Windows) are the next step.

## Layout

| File | Responsibility |
| --- | --- |
| `main.go` | App setup, tray + webview window, wiring to `core.Controller` |
| `dashboard.go` | Loopback HTTP server: serves the panel + token-gated control API |
| `assets/panel.html` | The dropdown panel UI (HTML/CSS/JS, no build step) |
| `autostart_darwin.go` | Start-at-login via `~/Library/LaunchAgents` plist |
| `autostart_linux.go` | Start-at-login via `~/.config/autostart/*.desktop` |
| `autostart_windows.go` | Start-at-login via the `HKCU…\Run` registry key |
