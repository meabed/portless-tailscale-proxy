# tailscale-proxy — Redesign (drop portless, discover by port) — Design

**Date:** 2026-05-31
**Status:** Approved (pending spec review)
**Project / package / repo:** `tailscale-proxy` (was `portless-tailscale-proxy`)
**Binary:** `tsp`
**Module path:** `github.com/meabed/tailscale-proxy`

## Purpose

Expose your locally-running dev servers through a single Tailscale entry — either
**privately** (Tailscale Serve, tailnet-only) or **publicly** (Tailscale Funnel) —
routed by URL path. Services are **discovered automatically** by scanning listening
TCP ports in a range (default `3000–5000`); there is no portless dependency.

The first path segment of the URL is the service's **project name** (derived from
the process's project folder), and it selects which local server to forward to:

```
https://<node>.ts.net/<project-name>/foo   →   127.0.0.1:<port>/foo
```

This replaces the previous design, which read `~/.portless/routes.json`. **All
portless code, references, docs, and naming are removed.**

## What changes vs. the previous version

| Area | Before (portless) | After (this redesign) |
| --- | --- | --- |
| Service source | `~/.portless/routes.json` | Live scan of listening TCP ports in a range |
| Path slug | portless `.local` hostname | Project-folder name (nearest `package.json`/`.git`) |
| Exposure | Funnel only | **Serve (private)** or **Funnel (public)**, `--private` flag |
| Naming | `portless-tailscale-proxy` / `ptp` | `tailscale-proxy` / `tsp` |
| Doctor | checks portless | checks discovery + serve/funnel readiness |

The **path-routing proxy itself is kept unchanged** (request logging, WebSocket,
streaming, index page, 404/502). Only the *source of the route map* and the
*exposure backend* change.

## Service discovery

Discover listening TCP services in a port range and describe each one.

```go
type Service struct {
    Slug    string // path segment, e.g. "help-ai-web"
    Port    int    // listening port on 127.0.0.1
    Runtime string // "node" | "bun" | "deno" | "python" | "ruby" | "php" | "go" | "" (unknown)
    Dir     string // project root (may be "")
    PID     int
}

type Discoverer interface {
    Discover(r PortRange, includeAll bool) ([]Service, error)
}

type PortRange struct{ Lo, Hi int } // default {3000, 5000}
```

Platform implementations (behind build tags), each returning `(port, pid, comm, cwd)` tuples:

- **Linux** (`discover_linux.go`): pure `/proc` — parse `/proc/net/tcp` +
  `/proc/net/tcp6` for `LISTEN` sockets and their inodes, map inode → pid via
  `/proc/<pid>/fd/*`, read `comm` from `/proc/<pid>/comm`, `cwd` from
  `readlink /proc/<pid>/cwd`. No external tools.
- **macOS / BSD** (`discover_darwin.go`): `lsof -nP -iTCP -sTCP:LISTEN -FpcnP`
  for port+pid+command; `ps -p <pids> -o pid=,comm=` for full runtime; `lsof -a
  -p <pid> -d cwd -Fn` for the working directory.
- **Windows** (`discover_windows.go`): `netstat -ano` for port+pid (LISTENING);
  process name via `tasklist /FI "PID eq <pid>" /FO CSV /NH`. **No cwd available**
  → slug falls back to `<runtime>-<port>`. Best-effort.

Shared logic (`discover.go`, platform-independent, fully unit-tested):

- **Runtime classification** — basename of the executable → runtime label.
  Known web runtimes: `node`, `bun`, `deno`, `python`/`python3`, `ruby`, `php`,
  plus `next`/`vite`/`rails` wrappers if seen. Unknown binaries → `""`.
- **Filtering** — by default keep only services whose runtime is a known web
  runtime. `--all` keeps every listener in range. `--runtimes node,bun` overrides
  the known set.
- **Project root** — from `cwd`, walk **up** to the nearest directory containing a
  project marker (`package.json`, `.git`, `go.mod`, `pyproject.toml`, `Cargo.toml`,
  `deno.json`, `composer.json`, `Gemfile`); use that directory's basename. If none
  found, use the `cwd` basename. If no `cwd` (Windows / permission denied), use
  `<runtime>-<port>` (or `port-<port>` if runtime unknown).
- **Slugify** — lowercase; spaces/underscores/dots → `-`; drop characters outside
  `[a-z0-9-]`; collapse repeats; trim `-`.
- **Collision handling** — if two services slugify to the same value, **both** get
  a `-<port>` suffix so every slug is unique and stable.

## Routing (unchanged proxy)

A `RouteStore` holds the discovered services, refreshed every `--interval` seconds.
For a request `/<segment>/<rest...>?<query>`:

1. The first segment is looked up against the slug map.
2. **Hit** → forward to `http://127.0.0.1:<port>/<rest...>?<query>` (segment
   stripped, `Host` rewritten, streaming + WebSocket preserved).
3. **Miss / empty** → `404` listing the registered services.
4. **Dead backend** → `502`.

Per-request logging stays on by default (`--quiet` / `--log-requests=false`).

## Exposure: private (Serve) vs public (Funnel)

A single `expose` abstraction wraps the two Tailscale backends:

```go
type Mode int
const ( ModeFunnel Mode = iota; ModeServe )

func exposeArgs(mode Mode, proxyPort, publicPort int) []string
func exposeStart(r Runner, mode Mode, proxyPort, publicPort int) error
func exposeReset(r Runner, mode Mode) error
func exposeStatus(r Runner, mode Mode) (string, error)
```

- **Public (default)** → `tailscale funnel --bg [--https <p>] <proxyPort>`;
  public port must be `443`, `8443`, or `10000`.
- **Private (`--private`)** → `tailscale serve --bg [--https <p>] <proxyPort>`;
  any HTTPS port allowed (default `443`).
- Reset uses `tailscale funnel reset` / `tailscale serve reset` respectively, run
  **synchronously** on shutdown (existing behavior).

> Tailscale note: the same port can't be Serve and Funnel simultaneously — whichever
> ran last wins. `tsp` only manages its own `--port` proxy entry.

## CLI

```
tsp <command> [flags]

Commands:
  start     Discover services, run the proxy, and expose it (Serve or Funnel)
  status    Print Serve/Funnel status and the current service map
  list      Print discovered services (slug → runtime, port, project, URL)
  reset     Remove the Serve/Funnel entry and exit
  doctor    Check tailscale, Serve/Funnel readiness, and discovery

start flags:
  --ports <lo-hi>     Port range to scan                 (default 3000-5000)
  --all               Include all listeners, not just known web runtimes
  --runtimes <list>   Comma-separated runtimes to include (default known set)
  --private           Expose privately via Tailscale Serve (default: public Funnel)
  --port <n>          Local proxy HTTP port              (default 8443)
  --interval <sec>    Re-scan period in seconds          (default 20)
  --https-port <n>    Public/tailnet HTTPS port          (default 443)
  --bg                Run tsp detached in the background (logs → ./tsp.log)
  --proxy-only        Run the proxy only; print the tailscale command to run yourself
  --log-requests      Log each proxied request           (default on)
  --quiet             Disable per-request logging
  -h, --help / -v, --version
```

- `--https-port` replaces the old `--funnel-port` (applies to both modes;
  validated as `443|8443|10000` only when the mode is Funnel).
- `--proxy-only` replaces the old `--no-funnel`.
- `list` and `status` accept `--ports`, `--all`, `--runtimes`, `--private`,
  `--https-port` so the printed URLs match what `start` would expose.

### Example `list` output

```
Discovered services (ports 3000-5000, public Funnel):
  help-ai-web          node    :4983   ~/work/help-ai/apps/web
    https://bigfoot.quoll-adhara.ts.net/help-ai-web/
  agent-api            bun     :4434   ~/work/help-ai/services/agent
    https://bigfoot.quoll-adhara.ts.net/agent-api/
```

## Doctor

`tsp doctor` checks:
1. **tailscale installed** → install link if missing.
2. **tailscale up** (logged in) → `tailscale up` hint.
3. **exposure ready** — for Funnel: Funnel enabled (HTTPS certs + `funnel` node
   attribute) with the existing remediation links; for Serve: always available.
4. **discovery** — counts services found in the range; if zero, hints to start a
   dev server or widen `--ports`/`--all`.

## Error handling

- **Discovery tool missing / errors** (e.g. no `lsof`) → warn once, treat as empty,
  keep polling; `doctor` surfaces it.
- **No services found** → proxy still runs; `404` index explains how to get
  discovered (start a server in range, `--all`, widen `--ports`).
- **Backend refused** → `502` naming `host:port`.
- **Funnel disabled / tailscale missing** → guidance + non-zero exit (unless
  `--proxy-only`).

## File structure (after rename)

| File | Responsibility |
| --- | --- |
| `main.go` | entry, version, dispatch |
| `cli.go` | flags, help, signals, start orchestration, list/status/doctor/reset |
| `discover.go` | `Service`, `PortRange`, runtime classification, project-root, slugify, collisions, filtering |
| `discover_linux.go` / `discover_darwin.go` / `discover_windows.go` | per-OS raw listener+pid+comm+cwd |
| `store.go` | `RouteStore` over `map[slug]Service`; `refresh()` re-discovers; diff logging |
| `proxy.go` | path-routing reverse proxy + request logging (mostly unchanged) |
| `expose.go` | Serve/Funnel abstraction (was `funnel.go`), `nodeDNSName`, `publicBase` |
| `poll.go` | ticker |
| `doctor.go` | preflight checks |
| `detach_unix.go` / `detach_windows.go` | `--bg` |
| `*_test.go` | discovery parsing, slug/project-root, runtime, expose args, proxy |
| `npm/…` | launcher + per-platform packages renamed to `tailscale-proxy*`, bin `tsp` |
| `.goreleaser.yaml`, workflows, `install.sh`, `README.md`, `docs/*` | renamed + updated |

## Rename checklist (portless → tailscale-proxy)

- `go.mod` module → `github.com/meabed/tailscale-proxy`; update all imports (none are
  self-imports today, single package `main`).
- Binary `ptp` → `tsp` everywhere (goreleaser `binary`, archive names, bin entries,
  install.sh, docs).
- npm: main package `tailscale-proxy`; platform packages
  `tailscale-proxy-<os>-<arch>`; launcher bin `tsp` (+ `tailscale-proxy` alias);
  `optionalDependencies` updated; generator target dirs updated.
- GitHub repo renamed `portless-tailscale-proxy` → `tailscale-proxy` (GitHub keeps a
  redirect); update `remote.origin` URL, goreleaser `release`/`brews` owner/name,
  Homebrew cask name `tsp`, README badges/links.
- Remove every "portless" string from code, tests, docs (including the old design
  spec references where they describe current behavior — historical specs under
  `docs/superpowers/specs/` are left as-is, this new spec supersedes them).
- Old log/state filename `ptp.log` → `tsp.log`.

## Testing

- **Discovery parsing** — feed canned `lsof`/`netstat` output and `/proc` fixtures
  into the platform parsers (parsers take an injected command runner / fs root) and
  assert `(port, pid, comm, cwd)` extraction.
- **Shared logic** — runtime classification table; project-root walk against a temp
  dir tree with markers; slugify cases; collision suffixing; runtime filtering and
  `--all`.
- **Expose args** — `exposeArgs` for Funnel vs Serve, default vs custom port.
- **Proxy** — unchanged tests (routing, strip, 404/502, WebSocket, logging).
- Real `tailscale`/`lsof` are not invoked in CI (runners injected/faked).

## Out of scope (YAGNI)

- Tailscale Services (per-service VIP hostnames) — heavier setup (tags + admin
  console + v1.86); revisit later if per-host names are wanted.
- Native `tailscale ... --set-path` per service — would bypass our proxy and lose
  request logging, which we want to keep.
- UDP / non-HTTP services.
- Auth beyond what Serve/Funnel already enforce.
