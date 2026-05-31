# portless-tailscale-proxy (`ptp`)

[![ci](https://github.com/meabed/portless-tailscale-proxy/actions/workflows/ci.yml/badge.svg)](https://github.com/meabed/portless-tailscale-proxy/actions/workflows/ci.yml)
[![release](https://github.com/meabed/portless-tailscale-proxy/actions/workflows/release.yml/badge.svg)](https://github.com/meabed/portless-tailscale-proxy/actions/workflows/release.yml)

Route a **single Tailscale Funnel** to **all** your [portless](https://portless.sh)
dev servers, by URL path.

Tailscale Funnel can only expose **one** hostname (your node's MagicDNS name) on a
fixed set of ports. It can't do wildcard subdomains. So `ptp` puts a tiny
path-routing reverse proxy behind the Funnel: the **first path segment** of the
URL is the portless hostname, and it selects which local dev server to forward to.

```
https://<node>.ts.net/module-help-ai-agent-api.local/foo
                      └──────────────┬───────────────┘
       ptp strips the segment, forwards → 127.0.0.1:4434/foo
```

It re-reads `~/.portless/routes.json` every few seconds, so servers that come and
go are picked up automatically. Streaming responses (SSE) and WebSocket upgrades
(Vite/Next HMR) pass straight through. Zero runtime dependencies — one small Go
binary.

---

## Quick start

```bash
# 1. Check your environment (tells you exactly what's missing, with links)
npx portless-tailscale-proxy doctor

# 2. Expose every portless server through your Tailscale Funnel
npx portless-tailscale-proxy start
```

Then open `https://<your-node>.ts.net/<portless-hostname>.local/` from anywhere.
Press <kbd>Ctrl-C</kbd> to stop — the Funnel is reset automatically on exit.

> Find your hostnames any time with `ptp list`.

---

## Install

| Method | Command |
| --- | --- |
| **npx** (no install) | `npx portless-tailscale-proxy <command>` |
| **npm** (global) | `npm i -g portless-tailscale-proxy` |
| **Homebrew** | `brew install meabed/tap/ptp` |
| **curl \| sh** | `curl -fsSL https://raw.githubusercontent.com/meabed/portless-tailscale-proxy/main/install.sh \| sh` |
| **Go** | `go install github.com/meabed/portless-tailscale-proxy@latest` |
| **Binaries** | [GitHub Releases](https://github.com/meabed/portless-tailscale-proxy/releases) |

Supported: **macOS, Linux, Windows, WSL** (amd64 + arm64).

---

## Commands

```
ptp start     Preflight, run the proxy, and start the Tailscale Funnel
ptp status    Print Funnel status and the current route map
ptp list      Print the live hostname → port map and public URLs
ptp reset     Stop the Funnel (tailscale funnel reset) and exit
ptp doctor    Check tailscale / Funnel / portless and print fix links
```

Run `ptp <command> --help` for command-specific flags. Global: `-h/--help`, `-v/--version`.

### `ptp start` flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--port <n>` | `8443` | Local proxy HTTP port |
| `--interval <sec>` | `20` | How often to re-read portless state |
| `--state <path>` | `~/.portless/routes.json` | portless state file |
| `--funnel-port <n>` | `443` | Public Funnel port — must be `443`, `8443`, or `10000` |
| `--bg` | off | Run `ptp` detached in the background (logs → `./ptp.log`) |
| `--fg` | on | Run in the foreground |
| `--no-funnel` | off | Run the proxy only; print the `tailscale` command to run yourself |

Examples:

```bash
ptp start                      # default: proxy on :8443, Funnel public on :443
ptp start --port 9000 --interval 10
ptp start --funnel-port 8443   # serve the Funnel on the 8443 public port
ptp start --no-funnel          # local proxy only (CI, debugging, custom funnels)
ptp start --bg                 # detach; check ./ptp.log; stop with `ptp reset` + kill
```

---

## Requirements

1. **[Tailscale](https://tailscale.com/download)** with **Funnel enabled** for your tailnet:
   - [HTTPS certificates](https://tailscale.com/kb/1153/enabling-https) enabled, and
   - the `funnel` node attribute granted in your tailnet policy file.
   - See the [Funnel docs](https://tailscale.com/kb/1223/funnel).
2. **[portless](https://portless.sh)** running locally:
   ```bash
   npm install -g portless
   portless proxy start
   ```

Not sure? Just run **`ptp doctor`** — it checks all of the above and prints the
exact fix link for anything missing:

```
✓ tailscale installed  (1.98.2)
✓ tailscale up
✓ funnel enabled
✓ portless routes  (4 route(s))

All checks passed — you're ready to `ptp start`.
```

---

## How it works

1. A ticker reads `~/.portless/routes.json` into a `hostname → port` map every
   `--interval` seconds.
2. A `net/http` reverse proxy matches the first path segment against that map,
   strips it, rewrites the `Host` header, and forwards to `127.0.0.1:<port>`.
   Streaming and WebSocket upgrades are handled by the Go standard library.
3. `tailscale funnel --bg <proxy-port>` exposes the proxy publicly on your node.
   On exit (`Ctrl-C`), the Funnel is reset.

See [docs/HOW-IT-WORKS.md](docs/HOW-IT-WORKS.md) for the full design.

---

## Troubleshooting

**Testing from the same Mac shows `x-portless` and a 404 — but it works from my phone.**
That's expected. From the host machine, MagicDNS resolves `<node>.ts.net` to your
*tailnet* IP, and portless's local proxy (which binds `:443` on all interfaces)
answers directly — the request never reaches the public Funnel. Test from outside
your tailnet, or see [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) for how to
hit the public Funnel ingress from the host.

More in [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md).

---

## Development

```bash
go test ./...          # run the test suite
go vet ./...           # static checks
go build -o ptp .      # build the binary
goreleaser release --snapshot --clean --skip=publish   # full cross-platform build
```

CI builds, vets, and race-tests on Linux/macOS/Windows and cross-compiles all six
release targets on every push. See [docs/RELEASING.md](docs/RELEASING.md) for how
releases are cut and published.

---

## License

[MIT](LICENSE) © Mohamed Meabed
