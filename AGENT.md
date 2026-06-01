# AGENT.md

Single source of truth for agents and contributors. **Rules, code style, patterns,
and workflow live here.** Product/usage/behavior specs live in the linked docs —
don't inline them here; keep them updated as features change.

## What this is

`tailscale-proxy` (binary **`tsp`**) — a single-binary Go CLI that discovers local
dev servers by listening **port** and exposes them through **one** Tailscale
Serve/Funnel entry, routed by URL path:

```
https://<node>.ts.net/<project>/foo   →   strip segment → 127.0.0.1:<port>/foo
```

Self-hosted ngrok alternative. Go **stdlib only**, zero runtime dependencies,
cross-platform (macOS, Linux, Windows, WSL). Module `github.com/meabed/tailscale-proxy`, Go 1.24.

## Spec & reference docs (keep updated; link, don't duplicate)

| Topic | File |
| --- | --- |
| Architecture & request flow | [docs/HOW-IT-WORKS.md](docs/HOW-IT-WORKS.md) |
| Usage & real examples | [docs/EXAMPLES.md](docs/EXAMPLES.md) |
| Troubleshooting | [docs/TROUBLESHOOTING.md](docs/TROUBLESHOOTING.md) |
| Testing approach & helpers | [docs/TESTING.md](docs/TESTING.md) |
| Release process | [docs/RELEASING.md](docs/RELEASING.md) |
| User overview | [README.md](README.md) |
| Published docs site | `website/` → https://tailscaleproxy.vercel.app |

When a flag or behavior changes, update the relevant doc above **and** the README
and `website/content/*.mdx` so all three stay in sync.

## Repo map

The engine + CLI live in **`core/`** (`package core`, importable). Root `main.go`
is a thin `package main` shell (`core.Run`). The desktop app is a **separate Go
module** under `desktop/` that imports `core` and drives it in-process.

| File | Responsibility |
| --- | --- |
| `main.go` (root) | Thin entry — `os.Exit(core.Run(os.Args[1:]))` |
| `core/dispatch.go` | Subcommand dispatch (bare `tsp` / leading flag → `start`); `Version` |
| `core/cli.go` | `start`: flags, server lifecycle, signal handling, header |
| `core/commands.go` | `list` / `status` / `reset` / `configure` + `queryConfig` |
| `core/controller.go` | **`Controller`** (Start/Stop/Status/OnChange) + exported embedder API (`LoadConfig`, `SaveConfig`, `ConfigPath`, `Doctor`, `OptionsFromConfig`) |
| `core/config.go` | `Config`, `defaultConfig()` overlay, load/save `~/.tailscale-proxy/config.json` |
| `core/discover.go` | `Service`/`Duplicate` model, runtime classification, slug from project root, `buildServices` |
| `core/discover_unix.go` | `//go:build !windows` — `lsof`/`ps` listeners + parsers |
| `core/discover_windows.go` | `//go:build windows` — `netstat`/`tasklist` + parsers |
| `core/store.go` | `RouteStore`: `refresh`, debounced de-registration |
| `core/proxy.go` | `newHandler`: reverse proxy, path routing, cookie affinity, Host rewrite |
| `core/expose.go` | `Runner` iface, `Mode` (Funnel/Serve), `tailscale serve\|funnel\|set`, `nodeDNSName`, accept-dns |
| `core/doctor.go` | `runDoctor` + `Check{}` + `printChecks` |
| `core/output.go` | Start header, service URLs, duplicate notes |
| `core/poll.go` | Periodic re-scan loop + logging (CLI) |
| `core/update.go` | Self-update (brew / npm / standalone) |
| `core/detach_unix.go` / `core/detach_windows.go` | Build-tagged `--bg` background spawn |
| `desktop/` | **Separate module** — Wails v3 tray app over `core.Controller`. See [desktop/README.md](desktop/README.md) |

Tests are `*_test.go` beside each file in `core/`. See [docs/TESTING.md](docs/TESTING.md).

## Code style

- **`core` is stdlib-only.** No third-party runtime dependencies in the engine/CLI
  — keep it that way. (The `desktop/` module is separate and may use Wails.)
- One clear responsibility per file; split when a file grows.
- Concise doc comments on exported identifiers; match the surrounding density. No
  noisy inline narration.
- `gofmt`-clean and `go vet`-clean before committing.
- Wrap errors with context (`fmt.Errorf("…: %v", err)`); surface `stderr` from
  external commands in the message.

## Patterns (follow these)

- **External commands go through the `Runner` interface** (`expose.go`). Real impl
  is `execRunner`; tests inject `fakeRunner` / `scriptRunner`. Don't call
  `exec.Command` for `tailscale`/`lsof` outside Runner-backed helpers.
- **Platform differences use build tags** (`//go:build !windows` / `windows`), not
  runtime `runtime.GOOS` checks — for discovery and detach.
- **Config:** always start from `defaultConfig()` then overlay the file; zero values
  are **not** defaults. Per-run flags override config.
- **Doctor `Check`:** `Fix` prints only when the check fails; `Note` is advisory and
  prints regardless. Advisories (e.g. the MagicDNS note) keep `OK: true` so they
  never fail `doctor`.
- **Modes:** `ModeFunnel` (public) vs `ModeServe` (`--private`).
- **Proxy:** first path segment = slug, stripped before forwarding; dial
  `127.0.0.1:<port>`; rewrite `Host: localhost:<port>`; cookie route-affinity
  (`tsp_route`) routes prefix-less asset/HMR requests; SSE streamed, WebSocket
  upgrades proxied.
- **De-registration is debounced** by `deregisterCycles` so brief restarts don't
  flap routes.
- Default presents a **local** request to apps (`X-Forwarded-*`, not PROXY
  protocol); `--forward-host` is opt-in for apps needing the public host.

## Conventions (behavior rules established across sessions)

- **System/global mutations are opt-in, default off, never silently reverted, and
  log how to undo.** Examples: `--accept-dns` (default unset = leave Tailscale DNS
  alone), `--bind` (default `127.0.0.1`; warn when binding beyond loopback).
- Validate user-facing flag values; reject bad input with **exit code 2** and a
  clear message.
- Reset the Serve/Funnel entry **synchronously** on exit (Ctrl-C); start listening
  before exposing.
- Prefer the smallest accurate change; no legacy/back-compat shims unless asked.

## Dev workflow

- **Go** is installed via Homebrew. If `go` is stale:
  `export GOROOT=/opt/homebrew/opt/go/libexec; export PATH="$GOROOT/bin:$PATH"`.
- Format / lint: `gofmt -w .` · `go vet ./...`  (or `bun run format` / `bun run lint`).
- Test: `go test -count=1 ./...` (CI uses `-race`). Always cross-check Windows:
  `GOOS=windows GOARCH=amd64 go build -o /dev/null .`.
- Build: `go build -o tsp .` · all release targets (snapshot): `bun run build:binaries`.
- Desktop app (separate module, needs CGO + system webview):
  `cd desktop && go build -o tsp-app .` (package with `wails3 build`). See
  [desktop/README.md](desktop/README.md).
- Website: `cd website && bun run build`.
- **Branching:** work on `master` (the default branch). Pushes to `master`/`main`
  trigger semantic-release. Branch first if asked to commit on the default branch;
  push/commit only when the user asks.
- **Conventional commits drive releases:** `feat:` → minor, `fix:` → patch,
  `docs:` / `chore:` / `test:` → no release. See [docs/RELEASING.md](docs/RELEASING.md).
