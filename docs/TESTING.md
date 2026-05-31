# Testing

Conventions for the Go test suite. Rules and workflow live in
[AGENT.md](../AGENT.md); this is the how-to for tests.

## Running

```bash
go test -count=1 ./...                         # full suite
go test -race -count=1 ./...                   # what CI runs
go test -run TestDoctor -v ./...               # one group
GOOS=windows GOARCH=amd64 go build -o /dev/null .   # always cross-check Windows
gofmt -l . && go vet ./...                     # must be clean
```

Tests live in `*_test.go` beside the file they cover (`package main`), one test
file per source file (`doctor_test.go`, `proxy_test.go`, `discover_unix_test.go`, …).

## Faking external commands

`tsp` shells out to `tailscale` and `lsof` only through the `Runner` interface
(`expose.go`). Tests inject a fake instead of touching the real system — never call
the real binaries from a test. Two fakes exist:

- **`fakeRunner`** (`expose_test.go`) — one canned `stdout`/`stderr`/`err` for every
  call; records `calls` so you can assert the exact argv. Use for single-command
  helpers (`exposeStart`, `setAcceptDNS`, `acceptDNSEnabled`).
- **`scriptRunner`** (`doctor_test.go`) — maps `"<name> <args…>"` → `{stdout, stderr,
  err}`; unstubbed commands return an error. Use when one code path runs several
  commands (e.g. `runDoctor` calls `tailscale version`, `status`, `funnel status`,
  `debug prefs`, `lsof …`).
- **`errString`** (`doctor_test.go`) — a string error for the `err` slot.

## What to cover when adding a feature

- **Flags:** valid values apply; invalid values are rejected (exit 2). Assert the
  exact `tailscale …` argv the Runner received.
- **Doctor checks:** a failure sets `OK:false` + a `Fix`; an advisory keeps
  `OK:true` + a `Note` (and must not appear when its condition is absent).
- **Parsers:** `lsof`/`ps` (unix) and `netstat`/`tasklist` (windows) parsers get
  table tests with real sample output — including the empty/no-match case. Keep
  unix/windows parser tests in their build-tagged `_test.go` files.
- **Discovery:** project-root slugging, duplicate collapsing, and `-<port>`
  suffixing belong in `discover_test.go`.
- **Proxy:** path-segment routing, the `tsp_route` cookie affinity, and Host
  rewriting belong in `proxy_test.go`.

Keep tests deterministic and offline — no real network, no real `tailscale`/`lsof`.
