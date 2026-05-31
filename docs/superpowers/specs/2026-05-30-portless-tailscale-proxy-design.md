# portless-tailscale-proxy — Design

**Date:** 2026-05-30
**Status:** Approved (pending spec review)
**Package name:** `portless-tailscale-proxy` (bin alias: `ptp`)

## Purpose

Expose every locally-running [portless](https://portless.sh) dev server to the
public internet through a single [Tailscale Funnel](https://tailscale.com/kb/1223/funnel),
routed by URL path.

Tailscale Funnel can only expose **one** hostname (the node's MagicDNS name,
e.g. `bigfoot.quoll-adhara.ts.net`) on a fixed set of ports (443/8443/10000).
It cannot do wildcard subdomains. So instead of one public hostname per service,
we put a small path-routing proxy behind the Funnel: the first path segment of
the URL selects which portless-registered dev server to forward to.

```
                              ┌─────────────────────────────────────┐
  public internet             │  this machine                       │
                              │                                     │
  https://bigfoot.ts.net  ──► │  tailscale funnel (TLS, :443)       │
    /module-...-api.local/foo │        │  plain HTTP                 │
                              │        ▼                             │
                              │  ptp proxy (Bun.serve, :8443)        │
                              │    map[module-...-api.local]         │
                              │        │ strip segment, → /foo       │
                              │        ▼                             │
                              │  127.0.0.1:4434  (real dev server)   │
                              └─────────────────────────────────────┘
                                        ▲ polled every 20s
                                  ~/.portless/routes.json
```

## State source

Portless persists active routes at `~/.portless/routes.json`:

```json
[
  { "hostname": "www-web-help-ai.local", "port": 4764, "pid": 4154 },
  { "hostname": "module-help-ai-workspace-api.local", "port": 4295, "pid": 4219 },
  { "hostname": "module-help-ai-agent-api.local", "port": 4434, "pid": 4315 },
  { "hostname": "module-help-ai-crawlee-api.local", "port": 4600, "pid": 4388 }
]
```

We read and parse this file. The path is overridable with `--state` (default
`$HOME/.portless/routes.json`). We do **not** shell out to `portless list` —
the JSON file is the stable, machine-readable source of truth.

## Routing rules

For an incoming request with path `/<segment>/<rest...>?<query>`:

1. Take the **first** path segment verbatim (e.g. `module-help-ai-agent-api.local`).
   No slugification — the segment is the exact portless hostname.
2. Look it up in the live route map.
   - **Hit** → forward to `http://127.0.0.1:<port>/<rest...>?<query>`. The matched
     segment is **stripped**; the dev server sees its normal root path
     (`/module-help-ai-agent-api.local/foo?x=1` → backend gets `/foo?x=1`;
     `/module-help-ai-agent-api.local` with no trailing rest → backend gets `/`).
   - **Miss / empty path** → `404` with a plain-text body listing the currently
     registered services and their public URLs.

### Forwarding behavior

- **Methods/headers/body:** all forwarded as-is, except the `Host` header, which
  is rewritten to `127.0.0.1:<port>` so dev servers that validate Host accept it.
  `X-Forwarded-Host` / `X-Forwarded-Proto: https` are added for app awareness.
- **Streaming:** request and response bodies are streamed (not buffered), so SSE,
  chunked responses, and large uploads/downloads work.
- **WebSockets:** `Upgrade` requests are proxied. The proxy accepts the client
  WS, opens a client WebSocket to the backend, and relays frames bidirectionally
  (covers Vite/Next HMR and socket APIs).

## Dependencies — zero at runtime

The package ships with **no runtime dependencies**. Everything is built on Bun's
native primitives — no proxy/router/CLI libraries:

- HTTP server: `Bun.serve`
- HTTP forwarding: global `fetch`
- WebSocket proxying: Bun server `websocket` handler + native `WebSocket` client
- Subprocess (`tailscale`): `Bun.spawn`
- File read: `Bun.file`
- Arg parsing, route matching, help text: hand-written (custom), not a dep.

`package.json` has an empty `dependencies` block. Dev-only deps are limited to the
toolchain: `oxlint`, `oxfmt`, `husky`, `lint-staged`. This keeps install fast and
the published tarball tiny (`index.ts` + README + LICENSE).

## Runtime & architecture

- **Runtime:** Bun-native. The HTTP server is `Bun.serve({ fetch, websocket })`.
  HTTP proxying uses `fetch()` (streams bodies natively); WS proxying uses Bun's
  server `websocket` handler plus a client `new WebSocket(target)`.
- **Single file:** all logic lives in `index.ts` (with a `#!/usr/bin/env bun`
  shebang) — small and readable, as requested.
- **Funnel target port:** the proxy listens on plain HTTP (default `8443`);
  Tailscale Funnel terminates TLS and forwards to it. The funnel public port is
  selectable (`--funnel-port`, default `443`).

### Module shape inside `index.ts`

| Unit | Responsibility | Depends on |
| --- | --- | --- |
| `loadRoutes(statePath)` | read + parse `routes.json` → `Map<hostname, port>` | `fs` |
| `RouteStore` | holds current map; `refresh()` swaps it atomically; logs add/remove diffs | `loadRoutes` |
| `startProxy(opts, store)` | `Bun.serve` with HTTP + WS proxy `fetch`/`websocket` handlers | `RouteStore` |
| `funnel.start/reset/status` | wrap `tailscale funnel` via `Bun.spawn` | `tailscale` CLI |
| `cli(argv)` | parse subcommand + flags, wire the above together | all |

## CLI

```
ptp <command> [flags]

Commands:
  start            Poll portless, run the proxy, and start the Tailscale Funnel
  reset            Stop the Funnel (tailscale funnel reset) and exit
  status           Print Funnel status + the current route map
  list             Print the live hostname→port map and public URLs

Flags (start):
  --port <n>          Local proxy HTTP port            (default 8443)
  --interval <sec>    Route refresh period in seconds  (default 20)
  --state <path>      routes.json path                 (default ~/.portless/routes.json)
  --funnel-port <n>   Public funnel port 443|8443|10000 (default 443)
  --bg                Run funnel in background (tailscale funnel --bg); proxy still foreground
  --fg                Run funnel in foreground (default)
  --no-funnel         Run the proxy only; print the tailscale command to run manually
  -h, --help          Show help
  -v, --version       Show version
```

- `start` registers a SIGINT/SIGTERM handler that resets the funnel (unless `--bg`)
  and shuts the proxy down cleanly.
- `--no-funnel` is the escape hatch: prints the exact
  `tailscale funnel --bg <port>` command instead of executing it.

## Error handling

- **Missing/invalid `routes.json`:** treat as empty map, warn once, keep polling
  (portless may not be running yet). Never crash the proxy on a bad read.
- **Backend connection refused** (dev server died between polls): respond `502`
  with a short plain-text message naming the host:port that failed.
- **`tailscale` not found / funnel error:** print the stderr, exit non-zero on
  `start`; the proxy is torn down so we don't leave an orphan listener.
- **Port already in use:** clear error naming the port and the `--port` flag.

## Tooling & packaging

- **Package manager / runtime:** Bun (`bun install`, `bun run`).
- **Runtime deps:** none (see "Dependencies — zero at runtime").
- **Lint:** `oxlint` (`bun run lint` / `lint:fix`).
- **Format:** `oxfmt` (`bun run format` / `format:check`).
- **Pre-commit:** `husky` + `lint-staged` running `oxfmt` + `oxlint --fix` on staged files.
- **npx/bunx:** `bin: { "portless-tailscale-proxy": "index.ts", "ptp": "index.ts" }`
  with the bun shebang. `bunx portless-tailscale-proxy start` works directly;
  `npx portless-tailscale-proxy start` works when `bun` is on PATH (documented).
- **Publish:** public npm package. `files` limited to `index.ts`, `README.md`,
  `LICENSE`. A `release` script bumps the version, creates a git tag, and runs
  `npm publish --access public`; a GitHub Actions workflow publishes on tag push
  using `NPM_TOKEN`.

## Testing

- `loadRoutes` / routing-decision logic (segment → target, strip, miss → 404)
  unit-tested with `bun test` against fixture JSON and synthetic `Request`s.
- An integration test spins up a throwaway backend `Bun.serve`, points a route at
  it, and asserts: path strip, header rewrite, streamed body passthrough, and a
  WebSocket echo round-trip. Funnel calls are not exercised in tests (external CLI).

## Out of scope (YAGNI)

- Wildcard-subdomain / host-based routing (Funnel can't do it).
- Auth / access control beyond what Tailscale Funnel already enforces.
- A config file — flags + the portless state file are enough.
- Hot-reloading the proxy's own code; restart on upgrade.
