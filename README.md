# tailscale-proxy (`tsp`)

[![ci](https://github.com/meabed/tailscale-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/meabed/tailscale-proxy/actions/workflows/ci.yml)
[![release](https://github.com/meabed/tailscale-proxy/actions/workflows/release.yml/badge.svg)](https://github.com/meabed/tailscale-proxy/actions/workflows/release.yml)

Discover your local dev servers by **port**, and expose them through a **single
Tailscale entry** — privately (Serve, tailnet-only) or publicly (Funnel) — routed
by **project name**.

No per-app wiring: just run your servers (`node`, `bun`,
`deno`, …) and `tsp` finds the ones listening in a port range, derives a path slug
from each project's folder, and routes to them under one hostname:

```
https://<node>.ts.net/<project>/foo
                      └────┬────┘
   tsp strips the segment, forwards → 127.0.0.1:<port>/foo
```

It re-scans every few seconds (so servers that come and go are picked up), keeps a
service around for a few scans before de-registering (no flapping on restarts), and
streams SSE / proxies WebSocket upgrades. Zero runtime dependencies — one small Go
binary.

---

## Quick start

```bash
# Check your environment (tells you exactly what's missing, with links)
npx tailscale-proxy doctor

# Discover dev servers in :3000-5000 and expose them publicly (Funnel)
npx tailscale-proxy            # "start" is the default command

# ...or save your preferences once, then just run `tsp`
npx tailscale-proxy configure --ports 3000-9000 --private
npx tailscale-proxy            # uses the saved config
```

Open `https://<your-node>.ts.net/<project>/` from anywhere (for Funnel) or from
your tailnet (for Serve). <kbd>Ctrl-C</kbd> resets the Serve/Funnel entry on exit.

---

## Install

| Method | Command |
| --- | --- |
| **npx** (no install) | `npx tailscale-proxy <command>` |
| **npm** (global) | `npm i -g tailscale-proxy` |
| **Homebrew** | `brew install meabed/tap/tsp` |

Supported: **macOS, Linux, Windows, WSL** (amd64 + arm64).
(`go install github.com/meabed/tailscale-proxy@latest` also works if you have Go.)

Update later with **`tsp update`** — it self-updates a standalone binary, or prints
`brew upgrade tsp` / `npm i -g tailscale-proxy@latest` for managed installs.

---

## Commands

```
tsp [flags]         Default: run "start" with your saved config
tsp start           Discover services, run the proxy, and expose it
tsp status          Serve/Funnel status + the current service map
tsp list            Discovered services (slug → runtime, port, project, URL)
tsp reset           Remove the Serve/Funnel entry and exit
tsp doctor          Check tailscale, exposure readiness, and discovery
tsp configure       Save defaults to ~/.tailscale-proxy/config.json
tsp update          Update to the latest release
```

Run `tsp start --help` for all flags. Global: `-h/--help`, `-v/--version`.

### `start` flags (defaults come from your config)

| Flag | Default | Meaning |
| --- | --- | --- |
| `--ports <lo-hi\|port>` | `3000-5000` | Port range **or a single port** to scan |
| `--all` | off | Include all listeners, not just web runtimes |
| `--runtimes <list>` | `node,bun,deno` | Comma-separated runtimes to include |
| `--private` | off | Expose privately via Tailscale **Serve** (default: **Funnel**) |
| `--port <n>` | `8443` | Local proxy HTTP port |
| `--interval <sec>` | `20` | Re-scan period |
| `--https-port <n>` | `443` | Public/tailnet HTTPS port (Funnel: `443`/`8443`/`10000`) |
| `--deregister-cycles <n>` | `5` | Missing scans before a gone service is removed |
| `--forward-host` | off | Forward the public host to apps via `X-Forwarded-Host/Proto`. Default presents a **local** request so apps behave exactly like `localhost` |
| `--bg` | off | Run detached (logs → `./tsp.log`) |
| `--proxy-only` | off | Run the proxy only; print the `tailscale` command |
| `--log-requests` | on | Log each proxied request |
| `--quiet` | off | Disable per-request logging |

On startup `tsp` prints whether it loaded your config and the effective parameters,
then logs each discovered service and any de-registration:

```
Using config: /Users/me/.tailscale-proxy/config.json
  ports=3000-5000  mode=public (Funnel)  proxy=127.0.0.1:8443  https=443
  interval=20s  runtimes=node,bun,deno (default)  deregister-after=5 scans  log-requests=true

2026/05/31 02:05:48 discovered help-ai-web   node   :4983   ~/work/help-ai/apps/web
2026/05/31 02:05:49 200 GET    /help-ai-web/ → 127.0.0.1:4983 (6ms)
```

Request logs are colorized by status on a terminal (set `NO_COLOR` to disable).

---

## Configuration

`tsp configure [flags]` writes `~/.tailscale-proxy/config.json` (created on first
use). Flags override config at runtime; the file is the source of defaults.

```json
{
  "ports": "3000-5000", "all": false, "runtimes": "", "private": false,
  "port": 8443, "interval": 20, "httpsPort": 443,
  "logRequests": true, "deregisterCycles": 5
}
```

---

## Requirements

1. **[Tailscale](https://tailscale.com/download)**, logged in (`tailscale up`).
   For **public** exposure (Funnel), Funnel must be enabled for your tailnet:
   [HTTPS certificates](https://tailscale.com/kb/1153/enabling-https) + the `funnel`
   node attribute ([Funnel docs](https://tailscale.com/kb/1223/funnel)). Private
   exposure (Serve) needs no extra setup.
2. **`lsof`** on macOS/Linux (macOS ships it; Linux: `apt/dnf install lsof`).
   Windows uses `netstat`/`tasklist` (built in).

Run `tsp doctor` — it checks all of the above and prints the exact fix link.

---

## How it works

1. Every `--interval` seconds, `tsp` lists listening TCP sockets in the range
   (macOS/Linux via `lsof`+`ps`, Windows via `netstat`+`tasklist`), classifies the
   runtime, and derives a slug from the nearest project-root folder
   (`package.json`/`.git`/…), de-duplicating collisions.
2. A `net/http` reverse proxy matches the first path segment to a service, strips
   it, rewrites `Host`, and forwards to `127.0.0.1:<port>` (streaming + WebSocket
   preserved, bounded connection pool).
3. `tailscale serve|funnel --bg <proxy-port>` exposes the proxy. On exit the entry
   is reset.

More in [docs/HOW-IT-WORKS.md](docs/HOW-IT-WORKS.md).

---

## Troubleshooting

`tsp doctor` first. Common issues (full list in
[docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md)):

- **Works from my phone but not my Mac** — from the host, MagicDNS resolves
  `<node>.ts.net` to your tailnet IP, so requests may not traverse the public
  Funnel. Test from outside your tailnet (see the doc for how to force it locally).
- **No services found** — start a dev server in range, widen `--ports`, or `--all`.
- **`lsof` not found** — install it (`apt/dnf install lsof`).

---

## Development

```bash
go test ./...          # run the test suite
go vet ./...           # static checks
go build -o tsp .      # build the binary
goreleaser release --snapshot --clean --skip=publish   # full cross-platform build
```

CI builds, vets, and race-tests on Linux/macOS/Windows and cross-compiles all six
release targets on every push. Releases are tag-driven — see
[docs/RELEASING.md](docs/RELEASING.md).

---

## License

[MIT](LICENSE) © Mohamed Meabed
