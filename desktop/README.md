# Tailscale Proxy — desktop app

A tray-first desktop wrapper around the `tsp` engine. It drives
[`core.Controller`](../core/controller.go) **in-process** (no sidecar), so the menu
bar can start/stop the proxy, switch Funnel/Serve, open service URLs, toggle
start-at-login, and edit the shared `~/.tailscale-proxy/config.json`.

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

## Package it (.app / .dmg / .msi / .deb)

For a signed, bundled app, use the Wails v3 toolchain:

```bash
go install github.com/wailsapp/wails/v3/cmd/wails3@latest
wails3 build            # see https://v3alpha.wails.io for packaging + signing
```

Packaging config (icons, bundle identifier, signing, notarization) and CI wiring
are the next step — see the repo's `AGENT.md` for status.

## Layout

| File | Responsibility |
| --- | --- |
| `main.go` | App setup, tray + webview window, wiring to `core.Controller` |
| `dashboard.go` | Loopback HTTP server: serves the panel + token-gated control API |
| `assets/panel.html` | The dropdown panel UI (HTML/CSS/JS, no build step) |
| `autostart_darwin.go` | Start-at-login via `~/Library/LaunchAgents` plist |
| `autostart_linux.go` | Start-at-login via `~/.config/autostart/*.desktop` |
| `autostart_windows.go` | Start-at-login via the `HKCU…\Run` registry key |
