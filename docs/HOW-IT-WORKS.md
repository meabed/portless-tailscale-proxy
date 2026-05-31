# How it works

`tsp` discovers local dev servers by port and serves them all behind one Tailscale
entry, routed by the first URL path segment (the project name).

## The problem it solves

[Tailscale Funnel](https://tailscale.com/kb/1223/funnel) and
[Serve](https://tailscale.com/kb/1312/serve) expose your node's single MagicDNS
name. Funnel has no wildcard subdomains and is limited to ports 443/8443/10000. So
to reach many local dev servers through one entry, you route by **path**.

`tsp` does that automatically, with no per-app config. It finds whatever is
listening, names each by its project folder, and proxies to it.

## Request flow

```
                                     ┌──────────────────────────────────────┐
  caller (internet or tailnet)       │  your machine                        │
                                     │                                      │
  https://bigfoot.ts.net             │  tailscale funnel|serve (TLS, :443)  │
    /help-ai-web/foo                 │        │  plain HTTP                  │
                                     │        ▼                             │
                                     │  tsp proxy (net/http, :8443)         │
                                     │   lookup "help-ai-web" → 4983        │
                                     │   strip segment → /foo               │
                                     │   rewrite Host → 127.0.0.1:4983       │
                                     │        ▼                             │
                                     │  127.0.0.1:4983  (your dev server)    │
                                     └──────────────────────────────────────┘
                                              ▲ port scan every --interval s
                                   lsof/ps (mac, linux) · netstat/tasklist (win)
```

## Discovery pipeline (every `--interval` seconds)

1. **List listeners** — sockets in `LISTEN` within the port range, with PID:
   - macOS/Linux: `lsof -nP -iTCP -sTCP:LISTEN -Fpcn`, enriched with `ps -o comm`
     (full runtime path) and `lsof -d cwd` (working directory).
   - Windows: `netstat -ano` + `tasklist` (no working directory available).
2. **Classify runtime** from the executable basename: `node`, `bun`, `deno`. By
   default only these are kept; `--all` includes everything, `--runtimes a,b`
   overrides the set.
3. **Slug** = the nearest project-root folder name walking up from the working
   directory (markers: `package.json`, `.git`, `go.mod`, `pyproject.toml`,
   `Cargo.toml`, `deno.json`, `composer.json`, `Gemfile`). No cwd → `<runtime>-<port>`.
4. **De-duplicate** — colliding slugs get a `-<port>` suffix (and `-<pid>` as a
   final tie-break).

## Routing

For `/<segment>/<rest...>?<query>`:

- **Hit** → forward to `http://127.0.0.1:<port>/<rest...>?<query>` (segment stripped,
  `Host` rewritten, `X-Forwarded-*` set). Streaming flushes immediately
  (`FlushInterval = -1`); WebSocket upgrades are relayed by the stdlib.
- **Miss / empty** → `404` with the list of registered services.
- **Dead backend** → `502`.

The proxy uses a single bounded `http.Transport` (capped idle pool + 60s idle
timeout) so connections to dev servers that come and go don't accumulate.

## State, debounce, and lifecycle

- A `RouteStore` holds the current `slug → service` map, refreshed by a ticker.
- **De-register debounce:** a service missing from discovery is retained for
  `deregisterCycles` (default 5) consecutive scans before removal — so a dev-server
  restart doesn't drop its route. New services are logged on discovery; removals are
  logged after the debounce.
- On `SIGINT`/`SIGTERM` the server drains and `tailscale serve|funnel reset` runs
  synchronously before exit.

## Exposure modes

- **Public** (default) → `tailscale funnel --bg <proxy-port>` (ports 443/8443/10000).
- **Private** (`--private`) → `tailscale serve --bg <proxy-port>` (tailnet-only).

## Config & default command

`~/.tailscale-proxy/config.json` (written by `tsp configure`) provides the defaults;
CLI flags override them. Running `tsp` with no subcommand runs `start` with that
config — so once configured, a bare `tsp` is all you need.
