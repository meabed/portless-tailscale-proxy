# How it works

`ptp` is a path-routing reverse proxy that sits between a single Tailscale Funnel
and your many portless dev servers.

## The problem it solves

[Tailscale Funnel](https://tailscale.com/kb/1223/funnel) exposes exactly **one**
hostname — your node's MagicDNS name, e.g. `bigfoot.quoll-adhara.ts.net` — on a
fixed set of public ports (`443`, `8443`, `10000`). It has no wildcard-subdomain
support, so you cannot give each dev server its own public hostname.

[portless](https://portless.sh) gives every local dev server a stable
`.local` URL and records the mapping in `~/.portless/routes.json`.

`ptp` bridges the two: it serves all portless servers under the single Funnel
hostname, keyed by the **first path segment**.

## Request flow

```
                                     ┌──────────────────────────────────────┐
  public internet                    │  your machine                        │
                                     │                                      │
  https://bigfoot.ts.net             │  tailscale funnel (TLS, public :443) │
    /module-help-ai-agent-api.local  │        │  plain HTTP                  │
    /foo?x=1                         │        ▼                             │
                                     │  ptp proxy  (net/http, :8443)         │
                                     │   lookup "module-help-ai-agent-api    │
                                     │           .local" → 4434              │
                                     │   strip segment → /foo?x=1            │
                                     │   rewrite Host → 127.0.0.1:4434       │
                                     │        │                             │
                                     │        ▼                             │
                                     │  127.0.0.1:4434  (your dev server)    │
                                     └──────────────────────────────────────┘
                                              ▲ re-read every --interval s
                                        ~/.portless/routes.json
```

## Components

All in one small Go module (standard library only):

| Piece | Responsibility |
| --- | --- |
| `loadRoutes` | Parse `routes.json` → `map[hostname]port`. Missing file = empty map (no crash). |
| `RouteStore` | Thread-safe map; `refresh()` swaps it atomically and logs added/removed hosts. |
| poller | Ticker that calls `refresh()` every `--interval` seconds. |
| proxy handler | `httputil.ReverseProxy` — picks the target per request, strips the matched segment, rewrites `Host`, streams bodies, and relays WebSocket upgrades. |
| funnel manager | Wraps `tailscale funnel` (`start`/`reset`/`status`) via `os/exec`. |
| doctor | Preflight checks for tailscale / Funnel / portless with fix links. |

## Routing rules

For a request path `/<segment>/<rest...>?<query>`:

- The **first segment** is looked up verbatim (it is the exact portless hostname,
  including `.local` — no slugging).
- **Hit** → forward to `http://127.0.0.1:<port>/<rest...>?<query>` (segment stripped).
- **Miss / empty path** → `404` with a plain-text list of registered services.
- **Dead backend** → `502` naming the failed `host:port`.

Forwarded requests get `Host` rewritten to the target, plus
`X-Forwarded-Host` and `X-Forwarded-Proto: https`. Responses flush immediately
(`FlushInterval = -1`) so SSE and chunked streaming work.

## Funnel lifecycle

`ptp start` runs `tailscale funnel --bg <proxy-port>` (foreground `tailscale
funnel` blocks, so registration always uses `--bg`). The proxy listens first, then
the Funnel is pointed at it. On `SIGINT`/`SIGTERM` the server drains and
`tailscale funnel reset` runs **synchronously** before the process exits, so you
never leave a Funnel pointing at a dead port.

`--bg` (on `ptp` itself) re-execs `ptp` detached and returns, logging to
`./ptp.log`.
