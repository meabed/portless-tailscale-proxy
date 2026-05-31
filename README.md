# portless-tailscale-proxy (`ptp`)

Route a **single Tailscale Funnel** to **all** your [portless](https://portless.sh)
dev servers by URL path. Funnel can only expose one hostname, so `ptp` puts a tiny
path-routing reverse proxy behind it: the first path segment is the portless
hostname and selects which local server to forward to.

```
https://<node>.ts.net/module-help-ai-agent-api.local/foo
        └────────────────────┬─────────────────────┘
        ptp strips the segment → 127.0.0.1:4434/foo
```

It polls `~/.portless/routes.json` every few seconds, so servers that come and go
are picked up automatically. Streaming responses (SSE) and WebSocket upgrades
(Vite/Next HMR) pass straight through.

## Install

**npm / npx** (no Go needed):

```bash
npx portless-tailscale-proxy doctor
npm i -g portless-tailscale-proxy
```

**Homebrew:**

```bash
brew install meabed/tap/ptp
```

**curl | sh:**

```bash
curl -fsSL https://raw.githubusercontent.com/meabed/portless-tailscale-proxy/main/install.sh | sh
```

**Go:**

```bash
go install github.com/meabed/portless-tailscale-proxy@latest
```

## Usage

```bash
ptp doctor     # check tailscale / Funnel / portless and print fix links
ptp start      # preflight, run the proxy, start the Funnel (Ctrl-C resets it)
ptp list       # show live hostname → port map
ptp status     # Funnel status + routes
ptp reset      # tailscale funnel reset
```

Flags for `start`:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--port <n>` | `8443` | Local proxy HTTP port |
| `--interval <sec>` | `20` | Route refresh period |
| `--state <path>` | `~/.portless/routes.json` | portless state file |
| `--funnel-port <n>` | `443` | Public funnel port (`443`, `8443`, or `10000`) |
| `--bg` | off | Run `ptp` detached in the background (logs → `ptp.log`) |
| `--no-funnel` | off | Run the proxy only; print the `tailscale` command to run manually |

## Requirements

- [Tailscale](https://tailscale.com/download) with **Funnel enabled** —
  [HTTPS certificates](https://tailscale.com/kb/1153/enabling-https) plus the
  `funnel` node attribute in your tailnet policy file. See the
  [Funnel docs](https://tailscale.com/kb/1223/funnel).
- [portless](https://portless.sh) running locally (`portless proxy start`).

Run `ptp doctor` — it tells you exactly what's missing, with links.

## How it works

1. A ticker reads `~/.portless/routes.json` into a `hostname → port` map.
2. A `net/http` reverse proxy matches the first path segment against that map,
   strips it, rewrites the `Host` header, and forwards to `127.0.0.1:<port>`.
3. `tailscale funnel --bg <proxy-port>` exposes the proxy publicly on your node.

Zero runtime dependencies — pure Go standard library.

## Platforms

macOS, Linux, Windows, and WSL (amd64 + arm64).

## License

MIT
