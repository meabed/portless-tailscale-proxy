# tailscale-proxy Redesign Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the portless dependency with automatic discovery of local dev servers by scanning listening TCP ports, route to them by project-name path through the existing proxy, expose privately (Tailscale Serve) or publicly (Funnel), and rename the project from `portless-tailscale-proxy`/`ptp` to `tailscale-proxy`/`tsp`.

**Architecture:** A platform discoverer lists listening TCP sockets in a port range with their PID/runtime/cwd. Shared logic classifies the runtime, derives a project-name slug from the project root, filters to web runtimes, and de-duplicates. A `RouteStore` (backed by an injectable `discover` func) refreshes on a ticker. The unchanged path-routing proxy forwards by first path segment. An `expose` layer wraps `tailscale serve` (private) or `tailscale funnel` (public).

**Tech Stack:** Go (stdlib only); external CLIs invoked at runtime: `tailscale`, plus `lsof`/`ps` (macOS/Linux) and `netstat`/`tasklist` (Windows).

**Module path:** `github.com/meabed/tailscale-proxy` · **Binary:** `tsp`

**Deviation from spec:** Unix discovery uses `lsof` (one parser for macOS + Linux) instead of pure `/proc` on Linux. Reliability/simplicity tradeoff; `lsof` is a runtime tool, not a Go dependency. `doctor` reports if `lsof` is missing.

**Run Go via:** `export GOROOT=/opt/homebrew/opt/go/libexec; export PATH="$GOROOT/bin:$PATH"` (this machine's shell still has a stale GOROOT in non-login shells).

---

## File structure (target)

| File | Responsibility |
| --- | --- |
| `main.go` | entry, version, command dispatch |
| `cli.go` | flags, help, signals, start/list/status/doctor/reset |
| `discover.go` | `Service`, `PortRange`, `discoverConfig`, runtime classification, project-root, slugify, filtering, `Discoverer` |
| `discover_unix.go` | `//go:build !windows` — `listeners()` via lsof/ps + pure parsers |
| `discover_windows.go` | `//go:build windows` — `listeners()` via netstat/tasklist + pure parsers |
| `store.go` | `RouteStore` over `map[slug]Service`, backed by an injectable `discover` func |
| `proxy.go` | path-routing reverse proxy + request logging (minor updates for `Service`) |
| `expose.go` | Serve/Funnel abstraction (`Mode`), `nodeDNSName`, `publicBase` |
| `update.go` | `tsp update` — latest-version check, install-method detection, self-replace |
| `poll.go` | ticker (unchanged logic) |
| `doctor.go` | preflight: tailscale / exposure / lsof / discovery |
| `detach_unix.go`, `detach_windows.go` | `--bg` (rename `ptp.log`→`tsp.log`) |
| `discover_test.go`, `discover_unix_test.go`, `discover_windows_test.go`, `store_test.go`, `proxy_test.go`, `expose_test.go`, `doctor_test.go`, `poll_test.go` | tests |

**Deleted:** `routes.go`, `routes_test.go`, `funnel.go`, `funnel_test.go`.

---

## Task 1: Rename module and binary to tailscale-proxy / tsp

**Files:** Modify `go.mod`, `main.go`

- [ ] **Step 1: Change the module path**

Edit `go.mod` line 1:
```
module github.com/meabed/tailscale-proxy
```

- [ ] **Step 2: Update the help-stub command name in `main.go`**

In `main.go`, the dispatch is unchanged; no portless strings live here. Confirm it still builds.

Run:
```bash
export GOROOT=/opt/homebrew/opt/go/libexec; export PATH="$GOROOT/bin:$PATH"
go build -o tsp . && ./tsp --version
```
Expected: prints `dev`. (`ptp` binary may remain from before; ignore — it's git-ignored.)

- [ ] **Step 3: Update .gitignore binary names**

Edit `.gitignore`: replace the `/ptp` and `/ptp.exe` lines with:
```gitignore
/tsp
/tsp.exe
```

- [ ] **Step 4: Commit**

```bash
git add go.mod .gitignore
git commit -m "refactor: rename module to github.com/meabed/tailscale-proxy, binary to tsp"
```

---

## Task 2: Shared discovery logic (`discover.go`)

**Files:** Create `discover.go`, `discover_test.go`

- [ ] **Step 1: Write the failing tests**

`discover_test.go`:
```go
package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyRuntime(t *testing.T) {
	cases := map[string]string{
		"/opt/homebrew/bin/node": "node",
		"bun":                    "bun",
		"/usr/bin/bun.exe":       "bun",
		"deno":                   "deno",
		"/usr/bin/python":        "", // not a default runtime anymore
		"ruby":                   "", // not a default runtime anymore
		"/opt/homebrew/opt/nats-server/bin/nats-server": "",
		"epmd": "",
	}
	for in, want := range cases {
		if got := classifyRuntime(in); got != want {
			t.Errorf("classifyRuntime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProjectRoot(t *testing.T) {
	root := t.TempDir()
	// root/proj/.git , root/proj/apps/web (cwd) -> "proj"
	web := filepath.Join(root, "proj", "apps", "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "proj", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := projectRoot(web); got != "proj" {
		t.Errorf("projectRoot = %q, want proj", got)
	}
	// no markers -> basename of cwd
	plain := filepath.Join(root, "loose", "thing")
	os.MkdirAll(plain, 0o755)
	if got := projectRoot(plain); got != "thing" {
		t.Errorf("projectRoot(no markers) = %q, want thing", got)
	}
	if got := projectRoot(""); got != "" {
		t.Errorf("projectRoot(empty) = %q, want empty", got)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Help AI Web":   "help-ai-web",
		"my_app.v2":     "my-app-v2",
		"  Spaced  ":    "spaced",
		"already-slug":  "already-slug",
		"@scope/pkg":    "scope-pkg",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildServices_filtersAndDisambiguates(t *testing.T) {
	dirA := t.TempDir() // -> basename used as slug source
	listeners := []listener{
		{Port: 4983, PID: 1, Comm: "/usr/bin/node", Cwd: dirA},
		{Port: 4222, PID: 2, Comm: "nats-server", Cwd: ""}, // unknown runtime -> dropped
		{Port: 3000, PID: 3, Comm: "bun", Cwd: ""},          // no cwd -> bun-3000
		{Port: 3001, PID: 4, Comm: "bun", Cwd: ""},          // no cwd -> bun-3001
	}
	svcs := buildServices(listeners, false, nil)
	got := map[string]Service{}
	for _, s := range svcs {
		got[s.Slug] = s
	}
	if _, ok := got["bun-3000"]; !ok {
		t.Errorf("expected bun-3000 slug, got %v", keysOf(got))
	}
	if _, ok := got["bun-3001"]; !ok {
		t.Errorf("expected bun-3001 slug, got %v", keysOf(got))
	}
	for _, s := range svcs {
		if s.Runtime == "" {
			t.Errorf("unknown-runtime service should have been filtered: %+v", s)
		}
	}
}

func TestBuildServices_collisionSuffix(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "proj", "api")
	b := filepath.Join(root, "proj", "api2")
	os.MkdirAll(filepath.Join(root, "proj", ".git"), 0o755)
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	// both resolve project root to "proj" -> collision -> -<port> suffix
	svcs := buildServices([]listener{
		{Port: 4001, PID: 1, Comm: "node", Cwd: a},
		{Port: 4002, PID: 2, Comm: "node", Cwd: b},
	}, false, nil)
	slugs := map[string]bool{}
	for _, s := range svcs {
		slugs[s.Slug] = true
	}
	if !slugs["proj-4001"] || !slugs["proj-4002"] {
		t.Errorf("expected proj-4001 and proj-4002, got %v", slugs)
	}
}

func TestParsePortRange(t *testing.T) {
	r, err := parsePortRange("3000-5000")
	if err != nil || r.Lo != 3000 || r.Hi != 5000 {
		t.Fatalf("got %+v err %v", r, err)
	}
	for _, bad := range []string{"5000-3000", "abc", "3000", "0-10", "1-70000"} {
		if _, err := parsePortRange(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func keysOf(m map[string]Service) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./... -run 'TestClassifyRuntime|TestProjectRoot|TestSlugify|TestBuildServices|TestParsePortRange'`
Expected: FAIL — undefined: classifyRuntime, etc.

- [ ] **Step 3: Implement `discover.go`**

```go
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Service is one discovered listening dev server.
type Service struct {
	Slug    string // URL path segment
	Port    int    // listening port (127.0.0.1)
	Runtime string // node|bun|deno or "" (unknown)
	Dir     string // working directory (may be "")
	PID     int
}

// PortRange is an inclusive TCP port range.
type PortRange struct{ Lo, Hi int }

func (r PortRange) contains(p int) bool { return p >= r.Lo && p <= r.Hi }

// discoverConfig bundles the discovery filters.
type discoverConfig struct {
	rng      PortRange
	all      bool
	runtimes map[string]bool // nil = all known web runtimes
}

// listener is a raw OS-level listening socket (pre-classification).
type listener struct {
	Port int
	PID  int
	Comm string
	Cwd  string
}

// Default known web runtimes (JS/TS). Others reachable via --runtimes or --all.
var knownRuntimes = map[string]string{
	"node": "node", "bun": "bun", "deno": "deno",
}

var projectMarkers = []string{
	"package.json", ".git", "go.mod", "pyproject.toml",
	"Cargo.toml", "deno.json", "composer.json", "Gemfile",
}

// classifyRuntime maps an executable path to a runtime label, or "".
func classifyRuntime(comm string) string {
	base := strings.ToLower(filepath.Base(comm))
	base = strings.TrimSuffix(base, ".exe")
	if rt, ok := knownRuntimes[base]; ok {
		return rt
	}
	return ""
}

// projectRoot walks up from dir to the nearest directory containing a project
// marker and returns its basename; falls back to dir's basename, or "".
func projectRoot(dir string) string {
	if dir == "" || dir == "/" {
		return ""
	}
	d := dir
	for {
		for _, m := range projectMarkers {
			if _, err := os.Stat(filepath.Join(d, m)); err == nil {
				return filepath.Base(d)
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return filepath.Base(dir)
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify normalizes a name into a URL path segment.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = slugUnsafe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// buildServices filters, slugs, and de-duplicates raw listeners.
func buildServices(listeners []listener, includeAll bool, runtimes map[string]bool) []Service {
	var out []Service
	for _, l := range listeners {
		rt := classifyRuntime(l.Comm)
		if !includeAll {
			if rt == "" {
				continue
			}
			if runtimes != nil && !runtimes[rt] {
				continue
			}
		}
		slug := slugify(projectRoot(l.Cwd))
		if slug == "" {
			if rt != "" {
				slug = rt + "-" + strconv.Itoa(l.Port)
			} else {
				slug = "port-" + strconv.Itoa(l.Port)
			}
		}
		out = append(out, Service{Slug: slug, Port: l.Port, Runtime: rt, Dir: l.Cwd, PID: l.PID})
	}
	disambiguate(out)
	sort.Slice(out, func(i, j int) bool { return out[i].Slug < out[j].Slug })
	return out
}

// disambiguate appends -<port> to any slug shared by more than one service.
func disambiguate(svcs []Service) {
	counts := map[string]int{}
	for _, s := range svcs {
		counts[s.Slug]++
	}
	for i := range svcs {
		if counts[svcs[i].Slug] > 1 {
			svcs[i].Slug = svcs[i].Slug + "-" + strconv.Itoa(svcs[i].Port)
		}
	}
}

// parsePortRange parses "lo-hi" into a validated PortRange.
func parsePortRange(s string) (PortRange, error) {
	parts := strings.SplitN(s, "-", 2)
	if len(parts) != 2 {
		return PortRange{}, fmt.Errorf("invalid port range %q (want lo-hi)", s)
	}
	lo, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	hi, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || lo < 1 || hi < lo || hi > 65535 {
		return PortRange{}, fmt.Errorf("invalid port range %q", s)
	}
	return PortRange{Lo: lo, Hi: hi}, nil
}

// parseRuntimes turns "node,bun" into a set; "" yields nil (all known).
func parseRuntimes(s string) map[string]bool {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	m := map[string]bool{}
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			m[p] = true
		}
	}
	return m
}

// Discoverer lists services using an injected command Runner (for exec parsers).
type Discoverer struct{ run Runner }

func newDiscoverer(r Runner) *Discoverer { return &Discoverer{run: r} }

// Discover returns the filtered services in the configured range.
func (d *Discoverer) Discover(cfg discoverConfig) ([]Service, error) {
	ls, err := d.listeners(cfg.rng)
	if err != nil {
		return nil, err
	}
	return buildServices(ls, cfg.all, cfg.runtimes), nil
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./... -run 'TestClassifyRuntime|TestProjectRoot|TestSlugify|TestBuildServices|TestParsePortRange' -v`
Expected: PASS. (Build will still fail overall until `d.listeners` exists — that's Task 3. Use `go vet` only after Task 3.)

- [ ] **Step 5: Commit**

```bash
git add discover.go discover_test.go
git commit -m "feat: service discovery core (classify, project-root, slugify, filter)"
```

---

## Task 3: Unix discoverer (`discover_unix.go`)

**Files:** Create `discover_unix.go`, `discover_unix_test.go`

- [ ] **Step 1: Write the failing tests (pure parsers)**

`discover_unix_test.go`:
```go
//go:build !windows

package main

import "testing"

func TestParseLsofListeners(t *testing.T) {
	// lsof -nP -iTCP -sTCP:LISTEN -Fpcn  (p=pid, c=command, n=name)
	out := "p4231\ncbun\nn*:4295\nn[::1]:4295\n" +
		"p630\ncControlCe\nn*:5000\n" +
		"p999\ncnode\nn127.0.0.1:8080\n" // 8080 out of range
	rng := PortRange{Lo: 3000, Hi: 5000}
	ls := parseLsofListeners(out, rng)
	// 4295 (deduped across v4/v6), 5000; not 8080
	if len(ls) != 2 {
		t.Fatalf("got %d listeners: %+v", len(ls), ls)
	}
	byPort := map[int]listener{}
	for _, l := range ls {
		byPort[l.Port] = l
	}
	if byPort[4295].PID != 4231 || byPort[4295].Comm != "bun" {
		t.Errorf("4295 wrong: %+v", byPort[4295])
	}
	if _, ok := byPort[5000]; !ok {
		t.Errorf("expected port 5000")
	}
}

func TestPortFromAddr(t *testing.T) {
	cases := map[string]int{"*:4764": 4764, "127.0.0.1:3000": 3000, "[::1]:3000": 3000, "[::]:5000": 5000, "bad": 0}
	for in, want := range cases {
		if got := portFromAddr(in); got != want {
			t.Errorf("portFromAddr(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParsePsComm(t *testing.T) {
	out := "  4231 /opt/homebrew/bin/bun\n  630 /System/.../ControlCenter\n"
	m := parsePsComm(out)
	if m[4231] != "/opt/homebrew/bin/bun" {
		t.Errorf("4231 = %q", m[4231])
	}
}

func TestParseLsofCwd(t *testing.T) {
	out := "p4231\nn/Users/me/work/help-ai/services/agent\np630\nn/\n"
	m := parseLsofCwd(out)
	if m[4231] != "/Users/me/work/help-ai/services/agent" {
		t.Errorf("4231 cwd = %q", m[4231])
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./... -run 'TestParseLsof|TestPortFromAddr|TestParsePsComm'`
Expected: FAIL — undefined: parseLsofListeners, etc.

- [ ] **Step 3: Implement `discover_unix.go`**

```go
//go:build !windows

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// listeners lists listening TCP sockets in range via lsof, enriching each with
// the full runtime (ps) and working directory (lsof -d cwd).
func (d *Discoverer) listeners(rng PortRange) ([]listener, error) {
	out, stderr, err := d.run.Run("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-Fpcn")
	if err != nil {
		return nil, fmt.Errorf("lsof failed (is lsof installed?): %v\n%s", err, stderr)
	}
	ls := parseLsofListeners(out, rng)
	if len(ls) == 0 {
		return ls, nil
	}

	pids := uniquePIDs(ls)
	if psOut, _, err := d.run.Run("ps", "-o", "pid=,comm=", "-p", strings.Join(pids, ",")); err == nil {
		comm := parsePsComm(psOut)
		for i := range ls {
			if c, ok := comm[ls[i].PID]; ok {
				ls[i].Comm = c
			}
		}
	}

	cwdArgs := append([]string{"-a", "-d", "cwd", "-Fpn", "-p", strings.Join(pids, ",")})
	if cwdOut, _, err := d.run.Run("lsof", cwdArgs...); err == nil {
		cwd := parseLsofCwd(cwdOut)
		for i := range ls {
			if c, ok := cwd[ls[i].PID]; ok {
				ls[i].Cwd = c
			}
		}
	}
	return ls, nil
}

// parseLsofListeners parses `lsof -Fpcn` output, deduping per (pid,port).
func parseLsofListeners(out string, rng PortRange) []listener {
	var res []listener
	seen := map[string]bool{}
	var pid int
	var comm string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
			comm = ""
		case 'c':
			comm = line[1:]
		case 'n':
			port := portFromAddr(line[1:])
			if port == 0 || !rng.contains(port) {
				continue
			}
			key := strconv.Itoa(pid) + ":" + strconv.Itoa(port)
			if seen[key] {
				continue
			}
			seen[key] = true
			res = append(res, listener{Port: port, PID: pid, Comm: comm})
		}
	}
	return res
}

// portFromAddr extracts the port from an lsof name like "*:4764" or "[::1]:3000".
func portFromAddr(addr string) int {
	i := strings.LastIndexByte(addr, ':')
	if i < 0 {
		return 0
	}
	p, err := strconv.Atoi(addr[i+1:])
	if err != nil {
		return 0
	}
	return p
}

// parsePsComm parses `ps -o pid=,comm=` output into pid -> executable path.
func parsePsComm(out string) map[int]string {
	m := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		m[pid] = strings.Join(f[1:], " ")
	}
	return m
}

// parseLsofCwd parses `lsof -d cwd -Fpn` output into pid -> cwd.
func parseLsofCwd(out string) map[int]string {
	m := map[int]string{}
	var pid int
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			if pid != 0 {
				m[pid] = line[1:]
			}
		}
	}
	return m
}

// uniquePIDs returns the distinct PIDs of a listener slice as strings.
func uniquePIDs(ls []listener) []string {
	seen := map[int]bool{}
	var out []string
	for _, l := range ls {
		if !seen[l.PID] {
			seen[l.PID] = true
			out = append(out, strconv.Itoa(l.PID))
		}
	}
	return out
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./... -run 'TestParseLsof|TestPortFromAddr|TestParsePsComm'` (and now `go build -o tsp .` should succeed)
Expected: PASS; build succeeds.

- [ ] **Step 5: Commit**

```bash
git add discover_unix.go discover_unix_test.go
git commit -m "feat: unix (lsof) listener discovery with tested parsers"
```

---

## Task 4: Windows discoverer (`discover_windows.go`)

**Files:** Create `discover_windows.go`, `discover_windows_test.go`

- [ ] **Step 1: Write the failing tests**

`discover_windows_test.go`:
```go
//go:build windows

package main

import "testing"

func TestParseNetstat(t *testing.T) {
	out := "  Proto  Local Address    Foreign Address   State        PID\n" +
		"  TCP    0.0.0.0:3000     0.0.0.0:0         LISTENING    1234\n" +
		"  TCP    [::]:4983        [::]:0            LISTENING    1234\n" +
		"  TCP    0.0.0.0:9000     0.0.0.0:0         LISTENING    5\n" + // out of range
		"  TCP    0.0.0.0:3000     1.2.3.4:55        ESTABLISHED  77\n"  // not listening
	ls := parseNetstat(out, PortRange{Lo: 3000, Hi: 5000})
	if len(ls) != 2 {
		t.Fatalf("got %d: %+v", len(ls), ls)
	}
}

func TestParseTasklist(t *testing.T) {
	out := "\"node.exe\",\"1234\",\"Console\",\"1\",\"50,000 K\"\n" +
		"\"bun.exe\",\"5678\",\"Console\",\"1\",\"60,000 K\"\n"
	m := parseTasklist(out)
	if m[1234] != "node.exe" || m[5678] != "bun.exe" {
		t.Errorf("got %v", m)
	}
}
```

- [ ] **Step 2: Run to verify it fails (on a Windows build)**

Run: `GOOS=windows go vet .` then build a windows test is not runnable on macOS; verify compilation:
```bash
GOOS=windows GOARCH=amd64 go build -o /dev/null .
```
Expected: FAIL — undefined: parseNetstat, parseTasklist.

- [ ] **Step 3: Implement `discover_windows.go`**

```go
//go:build windows

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// listeners lists listening TCP sockets via netstat, with process names from
// tasklist. Working directory is not available on Windows.
func (d *Discoverer) listeners(rng PortRange) ([]listener, error) {
	out, stderr, err := d.run.Run("netstat", "-ano", "-p", "TCP")
	if err != nil {
		return nil, fmt.Errorf("netstat failed: %v\n%s", err, stderr)
	}
	ls := parseNetstat(out, rng)
	if len(ls) == 0 {
		return ls, nil
	}
	if tlOut, _, err := d.run.Run("tasklist", "/FO", "CSV", "/NH"); err == nil {
		names := parseTasklist(tlOut)
		for i := range ls {
			if n, ok := names[ls[i].PID]; ok {
				ls[i].Comm = n
			}
		}
	}
	return ls, nil
}

// parseNetstat parses `netstat -ano -p TCP`, keeping LISTENING rows in range.
func parseNetstat(out string, rng PortRange) []listener {
	var res []listener
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "TCP" || f[3] != "LISTENING" {
			continue
		}
		port := portFromAddr(f[1])
		pid, err := strconv.Atoi(f[4])
		if port == 0 || err != nil || !rng.contains(port) {
			continue
		}
		key := f[4] + ":" + strconv.Itoa(port)
		if seen[key] {
			continue
		}
		seen[key] = true
		res = append(res, listener{Port: port, PID: pid})
	}
	return res
}

// portFromAddr extracts the port from "0.0.0.0:3000" or "[::]:4983".
func portFromAddr(addr string) int {
	i := strings.LastIndexByte(addr, ':')
	if i < 0 {
		return 0
	}
	p, err := strconv.Atoi(addr[i+1:])
	if err != nil {
		return 0
	}
	return p
}

// parseTasklist parses `tasklist /FO CSV /NH` into pid -> image name.
func parseTasklist(out string) map[int]string {
	m := map[int]string{}
	for _, line := range strings.Split(out, "\n") {
		cols := splitCSV(line)
		if len(cols) < 2 {
			continue
		}
		pid, err := strconv.Atoi(strings.TrimSpace(cols[1]))
		if err != nil {
			continue
		}
		m[pid] = strings.TrimSpace(cols[0])
	}
	return m
}

// splitCSV splits a simple quoted CSV line (no embedded quotes).
func splitCSV(line string) []string {
	var cols []string
	for _, c := range strings.Split(line, ",") {
		cols = append(cols, strings.Trim(strings.TrimSpace(c), "\""))
	}
	return cols
}
```

Note: `portFromAddr` is defined in `discover_unix.go` for non-Windows and here for Windows — each build includes exactly one. (They are identical; keeping one per build file avoids a shared-file split.)

- [ ] **Step 4: Run to verify it passes (windows build compiles)**

Run:
```bash
GOOS=windows GOARCH=amd64 go build -o /dev/null .
```
Expected: builds. (Windows unit tests run in CI on the windows runner.)

- [ ] **Step 5: Commit**

```bash
git add discover_windows.go discover_windows_test.go
git commit -m "feat: windows (netstat/tasklist) listener discovery with tested parsers"
```

---

## Task 5: RouteStore over discovery (`store.go`), delete portless `routes.go`

**Files:** Create `store.go`, `store_test.go`; Delete `routes.go`, `routes_test.go`

- [ ] **Step 1: Delete the portless route files**

```bash
git rm routes.go routes_test.go
```

- [ ] **Step 2: Write the failing test**

`store_test.go`:
```go
package main

import (
	"sort"
	"testing"
)

func TestRouteStore_refreshDiffAndLookup(t *testing.T) {
	svcs := []Service{{Slug: "a", Port: 1, Runtime: "node"}}
	store := NewRouteStore(func() ([]Service, error) { return svcs, nil })

	added, removed, err := store.refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "a" || len(removed) != 0 {
		t.Fatalf("first: added=%v removed=%v", added, removed)
	}
	if p, ok := store.lookup("a"); !ok || p != 1 {
		t.Fatalf("lookup a = %d %v", p, ok)
	}

	svcs = []Service{{Slug: "b", Port: 2, Runtime: "bun"}}
	added, removed, _ = store.refresh()
	sort.Strings(added)
	if len(added) != 1 || added[0] != "b" || len(removed) != 1 || removed[0] != "a" {
		t.Fatalf("second: added=%v removed=%v", added, removed)
	}
	if _, ok := store.lookup("a"); ok {
		t.Fatal("a should be gone")
	}
	snap := store.snapshot()
	if snap["b"].Runtime != "bun" {
		t.Fatalf("snapshot b = %+v", snap["b"])
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `go test ./... -run TestRouteStore`
Expected: FAIL — undefined: NewRouteStore.

- [ ] **Step 4: Implement `store.go`**

```go
package main

import (
	"sort"
	"sync"
)

// RouteStore holds the current slug→Service map behind a RWMutex. It is fed by
// an injectable discover function so it can be tested without the OS.
type RouteStore struct {
	mu       sync.RWMutex
	services map[string]Service
	discover func() ([]Service, error)
}

// NewRouteStore creates an empty store backed by a discover function.
func NewRouteStore(discover func() ([]Service, error)) *RouteStore {
	return &RouteStore{services: map[string]Service{}, discover: discover}
}

// lookup returns the port for a slug and whether it is registered.
func (s *RouteStore) lookup(slug string) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.services[slug]
	return svc.Port, ok
}

// snapshot returns a copy of the current slug→Service map.
func (s *RouteStore) snapshot() map[string]Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Service, len(s.services))
	for k, v := range s.services {
		out[k] = v
	}
	return out
}

// refresh re-discovers services, swaps the map, and reports slug diffs.
func (s *RouteStore) refresh() (added, removed []string, err error) {
	svcs, err := s.discover()
	if err != nil {
		return nil, nil, err
	}
	next := make(map[string]Service, len(svcs))
	for _, svc := range svcs {
		next[svc.Slug] = svc
	}
	s.mu.Lock()
	prev := s.services
	s.services = next
	s.mu.Unlock()
	for slug := range next {
		if _, ok := prev[slug]; !ok {
			added = append(added, slug)
		}
	}
	for slug := range prev {
		if _, ok := next[slug]; !ok {
			removed = append(removed, slug)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed, nil
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `go test ./... -run TestRouteStore -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add store.go store_test.go
git commit -m "feat: RouteStore over injectable discovery; remove portless routes.json loader"
```

---

## Task 6: Update the proxy for `Service` snapshots (`proxy.go`)

**Files:** Modify `proxy.go`, `proxy_test.go`

- [ ] **Step 1: Update `writeIndex` to iterate `Service`**

In `proxy.go`, replace the body of `writeIndex` with:
```go
// writeIndex writes a plain-text list of registered services with the given status.
func writeIndex(w http.ResponseWriter, store *RouteStore, status int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	snap := store.snapshot()
	slugs := make([]string, 0, len(snap))
	for s := range snap {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	fmt.Fprintln(w, "tailscale-proxy — registered services:")
	if len(slugs) == 0 {
		fmt.Fprintln(w, "  (none discovered — start a dev server in range, or try --all / --ports, then `tsp doctor`)")
		return
	}
	for _, s := range slugs {
		svc := snap[s]
		rt := svc.Runtime
		if rt == "" {
			rt = "?"
		}
		fmt.Fprintf(w, "  /%s/  →  127.0.0.1:%d  (%s)\n", s, svc.Port, rt)
	}
}
```

- [ ] **Step 2: Update the proxy tests for the new store constructor**

In `proxy_test.go`, every test currently does `store := NewRouteStore(""); store.routes = map[string]int{...}`. Replace each such pair with a helper. Add near the top of `proxy_test.go`:
```go
// storeWith builds a RouteStore whose discovery returns fixed services.
func storeWith(svcs ...Service) *RouteStore {
	s := NewRouteStore(func() ([]Service, error) { return svcs, nil })
	s.refresh()
	return s
}
```
Then replace, in each test:
- `store := NewRouteStore("")` + `store.routes = map[string]int{"svc.local": port}`
  → `store := storeWith(Service{Slug: "svc.local", Port: port, Runtime: "node"})`
- `store.routes = map[string]int{"known.local": 4000}`
  → `store := storeWith(Service{Slug: "known.local", Port: 4000, Runtime: "node"})`
- `store.routes = map[string]int{"dead.local": 1}`
  → `store := storeWith(Service{Slug: "dead.local", Port: 1, Runtime: "node"})`
- `store.routes = map[string]int{"ws.local": port}`
  → `store := storeWith(Service{Slug: "ws.local", Port: port, Runtime: "node"})`
- `store.routes = map[string]int{"svc.local": port}` (logging test)
  → `store := storeWith(Service{Slug: "svc.local", Port: port, Runtime: "node"})`
- `store.routes = map[string]int{"svc.local": 4000}` (loggingDisabled test)
  → `store := storeWith(Service{Slug: "svc.local", Port: 4000, Runtime: "node"})`

(The slugs keep the `.local` text only as arbitrary test strings; routing is slug-based and unaffected.)

- [ ] **Step 3: Run to verify it builds and passes**

Run:
```bash
go build -o tsp . && go test ./... -run 'TestSplitFirstSegment|TestHandler' -v
```
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add proxy.go proxy_test.go
git commit -m "refactor: proxy index over Service snapshots; tests use discovery-backed store"
```

---

## Task 7: Expose layer — Serve (private) or Funnel (public) (`expose.go`)

**Files:** Rename `funnel.go`→`expose.go`, `funnel_test.go`→`expose_test.go`; Modify both

- [ ] **Step 1: Rename the files**

```bash
git mv funnel.go expose.go
git mv funnel_test.go expose_test.go
```

- [ ] **Step 2: Rewrite the tests for `Mode`**

Replace the funnel-specific tests in `expose_test.go` with:
```go
package main

import (
	"strings"
	"testing"
)

type fakeRunner struct {
	calls  [][]string
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) Run(name string, args ...string) (string, string, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.stdout, f.stderr, f.err
}

func TestExposeArgs_funnelDefault(t *testing.T) {
	got := strings.Join(exposeArgs(ModeFunnel, 8443, 443), " ")
	if got != "funnel --bg 8443" {
		t.Fatalf("got %q", got)
	}
}

func TestExposeArgs_funnelCustomPort(t *testing.T) {
	got := strings.Join(exposeArgs(ModeFunnel, 8443, 8443), " ")
	if got != "funnel --bg --https 8443 8443" {
		t.Fatalf("got %q", got)
	}
}

func TestExposeArgs_serve(t *testing.T) {
	got := strings.Join(exposeArgs(ModeServe, 8443, 443), " ")
	if got != "serve --bg 8443" {
		t.Fatalf("got %q", got)
	}
}

func TestExposeStartAndReset(t *testing.T) {
	r := &fakeRunner{}
	if err := exposeStart(r, ModeServe, 8443, 443); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.calls[0], " ") != "tailscale serve --bg 8443" {
		t.Fatalf("start: %v", r.calls[0])
	}
	r2 := &fakeRunner{}
	if err := exposeReset(r2, ModeFunnel); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r2.calls[0], " ") != "tailscale funnel reset" {
		t.Fatalf("reset: %v", r2.calls[0])
	}
}

func TestNodeDNSName(t *testing.T) {
	r := &fakeRunner{stdout: `{"Self":{"DNSName":"bigfoot.quoll-adhara.ts.net."}}`}
	name, err := nodeDNSName(r)
	if err != nil {
		t.Fatal(err)
	}
	if name != "bigfoot.quoll-adhara.ts.net" {
		t.Fatalf("got %q", name)
	}
}

func TestPublicBase(t *testing.T) {
	if got := publicBase("n.ts.net", 443); got != "https://n.ts.net" {
		t.Errorf("443: %q", got)
	}
	if got := publicBase("n.ts.net", 8443); got != "https://n.ts.net:8443" {
		t.Errorf("8443: %q", got)
	}
}
```

- [ ] **Step 3: Rewrite `expose.go`**

Replace the funnel-specific functions (keep `Runner`, `execRunner`, `nodeDNSName`, `publicBase`) with the `Mode`-based API:
```go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
)

// Runner runs external commands. Abstracted so tests can fake `tailscale`/`lsof`.
type Runner interface {
	Run(name string, args ...string) (stdout, stderr string, err error)
}

type execRunner struct{}

func (execRunner) Run(name string, args ...string) (string, string, error) {
	cmd := exec.Command(name, args...)
	var out, errb bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errb
	err := cmd.Run()
	return out.String(), errb.String(), err
}

// Mode selects the Tailscale exposure backend.
type Mode int

const (
	ModeFunnel Mode = iota // public internet
	ModeServe              // private, tailnet-only
)

func (m Mode) subcommand() string {
	if m == ModeServe {
		return "serve"
	}
	return "funnel"
}

func (m Mode) label() string {
	if m == ModeServe {
		return "Tailscale Serve (private)"
	}
	return "Tailscale Funnel (public)"
}

// exposeArgs builds the `tailscale serve|funnel` argument list.
func exposeArgs(mode Mode, proxyPort, publicPort int) []string {
	args := []string{mode.subcommand(), "--bg"}
	if publicPort != 443 {
		args = append(args, "--https", strconv.Itoa(publicPort))
	}
	return append(args, strconv.Itoa(proxyPort))
}

// exposeStart registers the Serve/Funnel entry for the local proxy port.
func exposeStart(r Runner, mode Mode, proxyPort, publicPort int) error {
	_, stderr, err := r.Run("tailscale", exposeArgs(mode, proxyPort, publicPort)...)
	if err != nil {
		return fmt.Errorf("tailscale %s failed: %v\n%s", mode.subcommand(), err, stderr)
	}
	return nil
}

// exposeReset removes the Serve/Funnel configuration.
func exposeReset(r Runner, mode Mode) error {
	_, stderr, err := r.Run("tailscale", mode.subcommand(), "reset")
	if err != nil {
		return fmt.Errorf("tailscale %s reset failed: %v\n%s", mode.subcommand(), err, stderr)
	}
	return nil
}

// exposeStatus returns the human-readable serve/funnel status.
func exposeStatus(r Runner, mode Mode) (string, error) {
	out, stderr, err := r.Run("tailscale", mode.subcommand(), "status")
	if err != nil {
		return "", fmt.Errorf("%v\n%s", err, stderr)
	}
	return out, nil
}

// nodeDNSName returns this node's MagicDNS name (without trailing dot).
func nodeDNSName(r Runner) (string, error) {
	out, stderr, err := r.Run("tailscale", "status", "--json")
	if err != nil {
		return "", fmt.Errorf("tailscale status failed: %v\n%s", err, stderr)
	}
	var s struct {
		Self struct {
			DNSName string `json:"DNSName"`
		} `json:"Self"`
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		return "", err
	}
	name := strings.TrimSuffix(s.Self.DNSName, ".")
	if name == "" {
		return "", fmt.Errorf("this node has no MagicDNS name")
	}
	return name, nil
}

// publicBase returns the exposure base URL, e.g. https://node.ts.net[:port].
func publicBase(node string, publicPort int) string {
	if publicPort == 443 {
		return "https://" + node
	}
	return fmt.Sprintf("https://%s:%d", node, publicPort)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./... -run 'TestExpose|TestNodeDNSName|TestPublicBase' -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add expose.go expose_test.go
git commit -m "feat: expose layer with Serve (private) and Funnel (public) modes"
```

---

## Task 8: Doctor rewrite (`doctor.go`)

**Files:** Modify `doctor.go`, `doctor_test.go`

- [ ] **Step 1: Rewrite the doctor tests**

Replace `doctor_test.go` with:
```go
package main

import (
	"strings"
	"testing"
)

type scriptRunner struct {
	responses map[string][3]string
}

func (s scriptRunner) Run(name string, args ...string) (string, string, error) {
	key := name + " " + strings.Join(args, " ")
	r, ok := s.responses[key]
	if !ok {
		return "", "not stubbed", errString("stub")
	}
	var err error
	if r[2] != "" {
		err = errString(r[2])
	}
	return r[0], r[1], err
}

type errString string

func (e errString) Error() string { return string(e) }

func TestDoctor_tailscaleMissing(t *testing.T) {
	r := scriptRunner{responses: map[string][3]string{}}
	disc := newDiscoverer(r)
	cfg := discoverConfig{rng: PortRange{3000, 5000}}
	checks := runDoctor(r, disc, cfg, ModeFunnel)
	c := findCheck(t, checks, "tailscale installed")
	if c.OK || !strings.Contains(c.Fix, "tailscale.com/download") {
		t.Fatalf("expected failing tailscale check with link, got %+v", c)
	}
}

func TestDoctor_serveModeSkipsFunnelCheck(t *testing.T) {
	r := scriptRunner{responses: map[string][3]string{
		"tailscale version":  {"1.98.2", "", ""},
		"tailscale status":   {"100.1.1.1 node user macOS -", "", ""},
		"lsof -nP -iTCP -sTCP:LISTEN -Fpcn": {"", "", ""},
	}}
	disc := newDiscoverer(r)
	cfg := discoverConfig{rng: PortRange{3000, 5000}}
	checks := runDoctor(r, disc, cfg, ModeServe)
	for _, c := range checks {
		if c.Name == "funnel enabled" {
			t.Fatal("serve mode should not check funnel")
		}
	}
}

func findCheck(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found", name)
	return Check{}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./... -run TestDoctor`
Expected: FAIL — runDoctor signature mismatch / undefined.

- [ ] **Step 3: Rewrite `doctor.go`**

```go
package main

import (
	"fmt"
	"strings"
)

// Check is one preflight result with a remediation hint.
type Check struct {
	Name   string
	OK     bool
	Detail string
	Fix    string
}

const (
	linkTailscaleInstall = "https://tailscale.com/download"
	linkFunnelKB         = "https://tailscale.com/kb/1223/funnel"
	linkHTTPSKB          = "https://tailscale.com/kb/1153/enabling-https"
)

// runDoctor probes tailscale, exposure readiness, lsof, and discovery.
func runDoctor(r Runner, disc *Discoverer, cfg discoverConfig, mode Mode) []Check {
	var checks []Check

	verOut, _, verErr := r.Run("tailscale", "version")
	if verErr != nil {
		checks = append(checks, Check{
			Name: "tailscale installed", OK: false,
			Detail: "`tailscale` not found on PATH",
			Fix:    "Install Tailscale: " + linkTailscaleInstall,
		})
	} else {
		checks = append(checks, Check{"tailscale installed", true, firstLine(verOut), ""})

		statusOut, _, statusErr := r.Run("tailscale", "status")
		if statusErr != nil || strings.Contains(statusOut, "Logged out") {
			checks = append(checks, Check{
				Name: "tailscale up", OK: false, Detail: "node is not logged in",
				Fix: "Run: tailscale up   (https://tailscale.com/kb/1080/cli#up)",
			})
		} else {
			checks = append(checks, Check{"tailscale up", true, "", ""})
		}

		if mode == ModeFunnel {
			_, fStderr, fErr := r.Run("tailscale", "funnel", "status")
			if fErr != nil {
				checks = append(checks, Check{
					Name: "funnel enabled", OK: false, Detail: strings.TrimSpace(fStderr),
					Fix: "Enable Funnel for your tailnet:\n" +
						"  - Overview: " + linkFunnelKB + "\n" +
						"  - Enable HTTPS certs: " + linkHTTPSKB + "\n" +
						"  - Grant the `funnel` node attribute in your tailnet policy file (admin console)",
				})
			} else {
				checks = append(checks, Check{"funnel enabled", true, "", ""})
			}
		}
	}

	// Discovery readiness.
	svcs, derr := disc.Discover(cfg)
	if derr != nil {
		checks = append(checks, Check{
			Name: "service discovery", OK: false, Detail: derr.Error(),
			Fix: "Ensure `lsof` is installed (macOS has it; Linux: `apt install lsof` / `dnf install lsof`)",
		})
	} else {
		detail := fmt.Sprintf("%d service(s) in %d-%d", len(svcs), cfg.rng.Lo, cfg.rng.Hi)
		fix := ""
		ok := true
		if len(svcs) == 0 {
			ok = false
			detail = fmt.Sprintf("no services found in %d-%d", cfg.rng.Lo, cfg.rng.Hi)
			fix = "Start a dev server in range, widen --ports, or pass --all to include non-web processes"
		}
		checks = append(checks, Check{"service discovery", ok, detail, fix})
	}

	return checks
}

// firstLine returns the first line of s, trimmed.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return strings.TrimSpace(s)
}

// printChecks writes a ✓/✗ summary and returns true if every check passed.
func printChecks(checks []Check) bool {
	allOK := true
	for _, c := range checks {
		mark := "✓"
		if !c.OK {
			mark = "✗"
			allOK = false
		}
		line := fmt.Sprintf("%s %s", mark, c.Name)
		if c.Detail != "" {
			line += "  (" + c.Detail + ")"
		}
		fmt.Println(line)
		if !c.OK && c.Fix != "" {
			for _, fl := range strings.Split(c.Fix, "\n") {
				fmt.Println("    " + fl)
			}
		}
	}
	return allOK
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `go test ./... -run TestDoctor -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add doctor.go doctor_test.go
git commit -m "feat: doctor checks tailscale, exposure mode, and discovery (no portless)"
```

---

## Task 9: CLI rewrite (`cli.go`), poll + detach renames

**Files:** Modify `cli.go`, `detach_unix.go`, `detach_windows.go`; verify `poll.go`/`poll_test.go`

- [ ] **Step 1: Rewrite `poll_test.go` for the discovery-backed store**

Replace `poll_test.go` with:
```go
package main

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoll_picksUpChanges(t *testing.T) {
	var stage atomic.Int32
	store := NewRouteStore(func() ([]Service, error) {
		if stage.Load() == 0 {
			return nil, nil
		}
		return []Service{{Slug: "x", Port: 9, Runtime: "node"}}, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go poll(ctx, store, 10*time.Millisecond)

	stage.Store(1)
	deadline := time.After(2 * time.Second)
	for {
		if p, ok := store.lookup("x"); ok && p == 9 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("poll did not pick up the new service")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
```
(`poll.go` itself is unchanged — it only calls `store.refresh()`.)

- [ ] **Step 2: Rename the background log file**

In `detach_unix.go` and `detach_windows.go`, no `ptp.log` literal exists (the name is passed in from `cli.go`). No change needed here. Confirm with:
```bash
grep -rn "ptp.log" . || echo "no ptp.log literals outside cli.go"
```

- [ ] **Step 3: Rewrite `cli.go`**

Replace the entire `cli.go` with:
```go
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strings"
	"syscall"
	"time"
)

func printHelp() {
	fmt.Print(`tailscale-proxy (tsp)
Discover local dev servers by port and expose them through one Tailscale entry,
routed by project name.

  https://<node>.ts.net/<project>/foo   →   127.0.0.1:<port>/foo

Usage:
  tsp <command> [flags]

Commands:
  start     Discover services, run the proxy, and expose it (Serve or Funnel)
  status    Print Serve/Funnel status and the current service map
  list      Print discovered services (slug → runtime, port, project, URL)
  reset     Remove the Serve/Funnel entry and exit
  doctor    Check tailscale, exposure readiness, and discovery
  update    Update tsp to the latest release (or show the brew/npm command)

Examples:
  tsp doctor                 # verify your environment
  tsp start                  # discover :3000-5000 and expose publicly (Funnel)
  tsp start --private        # expose privately (Serve, tailnet-only)
  tsp start --ports 3000-9000 --all
  tsp list                   # see discovered services + URLs

Run "tsp start --help" for all flags. Global: -h/--help, -v/--version
Docs: https://github.com/meabed/tailscale-proxy
`)
}

func startUsage() {
	fmt.Print(`tsp start — discover services, run the proxy, and expose it

Usage:
  tsp start [flags]

Flags:
  --ports <lo-hi>     Port range to scan                 (default 3000-5000)
  --all               Include all listeners, not just known web runtimes
  --runtimes <list>   Comma-separated runtimes to include (e.g. node,bun)
  --private           Expose privately via Tailscale Serve (default: public Funnel)
  --port <n>          Local proxy HTTP port              (default 8443)
  --interval <sec>    Re-scan period in seconds          (default 20)
  --https-port <n>    Public/tailnet HTTPS port          (default 443)
  --bg                Run tsp detached in the background (logs → ./tsp.log)
  --proxy-only        Run the proxy only; print the tailscale command to run yourself
  --log-requests      Log each proxied request           (default on)
  --quiet             Disable per-request logging
  -h, --help          Show this help

Press Ctrl-C to stop — the Serve/Funnel entry is reset automatically on exit.
`)
}

type startOpts struct {
	portsRaw    string
	all         bool
	runtimesRaw string
	private     bool
	port        int
	interval    int
	httpsPort   int
	bg          bool
	proxyOnly   bool
	logRequests bool
	quiet       bool
}

func cmdStart(argv []string) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.Usage = startUsage
	var o startOpts
	fs.StringVar(&o.portsRaw, "ports", "3000-5000", "port range to scan")
	fs.BoolVar(&o.all, "all", false, "include all listeners")
	fs.StringVar(&o.runtimesRaw, "runtimes", "", "comma-separated runtimes to include")
	fs.BoolVar(&o.private, "private", false, "expose via Tailscale Serve (private)")
	fs.IntVar(&o.port, "port", 8443, "local proxy HTTP port")
	fs.IntVar(&o.interval, "interval", 20, "re-scan period (seconds)")
	fs.IntVar(&o.httpsPort, "https-port", 443, "public/tailnet HTTPS port")
	fs.BoolVar(&o.bg, "bg", false, "run detached in background")
	var fg bool
	fs.BoolVar(&fg, "fg", false, "run in foreground (default)")
	fs.BoolVar(&o.proxyOnly, "proxy-only", false, "proxy only; print tailscale command")
	fs.BoolVar(&o.logRequests, "log-requests", true, "log each proxied request")
	fs.BoolVar(&o.quiet, "quiet", false, "disable per-request logging")
	if err := fs.Parse(argv); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	mode := ModeFunnel
	if o.private {
		mode = ModeServe
	}
	if mode == ModeFunnel && o.httpsPort != 443 && o.httpsPort != 8443 && o.httpsPort != 10000 {
		fmt.Fprintf(os.Stderr, "invalid --https-port %d: Funnel allows only 443, 8443, or 10000\n", o.httpsPort)
		return 2
	}
	rng, err := parsePortRange(o.portsRaw)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	if o.quiet {
		o.logRequests = false
	}

	if o.bg {
		pid, err := spawnDetached("tsp.log")
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to detach: %v\n", err)
			return 1
		}
		fmt.Printf("tailscale-proxy running in background (pid %d), logs → tsp.log\n", pid)
		return 0
	}

	runner := execRunner{}
	cfg := discoverConfig{rng: rng, all: o.all, runtimes: parseRuntimes(o.runtimesRaw)}
	disc := newDiscoverer(runner)

	// Preflight (non-fatal in --proxy-only mode).
	if !printChecks(runDoctor(runner, disc, cfg, mode)) && !o.proxyOnly {
		fmt.Fprintln(os.Stderr, "\npreflight failed — fix the items above, or use --proxy-only to run the proxy alone")
		return 1
	}

	store := NewRouteStore(func() ([]Service, error) { return disc.Discover(cfg) })
	_, _, _ = store.refresh()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	go poll(ctx, store, time.Duration(o.interval)*time.Second)

	srv := &http.Server{Addr: fmt.Sprintf("127.0.0.1:%d", o.port), Handler: newHandler(store, o.logRequests)}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot listen on %s: %v\n", srv.Addr, err)
		return 1
	}
	log.Printf("listening on http://%s", srv.Addr)
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	if o.proxyOnly {
		fmt.Printf("proxy only — run this to expose it:\n  tailscale %s\n",
			strings.Join(exposeArgs(mode, o.port, o.httpsPort), " "))
	} else {
		if err := exposeStart(runner, mode, o.port, o.httpsPort); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			_ = srv.Close()
			return 1
		}
		fmt.Printf("%s → 127.0.0.1:%d (port %d)\n", mode.label(), o.port, o.httpsPort)
	}

	if node, nerr := nodeDNSName(runner); nerr == nil {
		fmt.Println("\nServices:")
		printServiceURLs(store.snapshot(), node, o.httpsPort)
		fmt.Println()
	}

	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			if !o.proxyOnly {
				_ = exposeReset(runner, mode)
			}
			return 1
		}
	case <-ctx.Done():
		fmt.Println("\nshutting down…")
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	if !o.proxyOnly {
		if err := exposeReset(runner, mode); err != nil {
			log.Printf("warn: %v", err)
		} else {
			fmt.Printf("%s reset.\n", mode.label())
		}
	}
	return 0
}

func cmdReset(argv []string) int {
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	private := fs.Bool("private", false, "reset the Serve entry instead of Funnel")
	_ = fs.Parse(argv)
	mode := ModeFunnel
	if *private {
		mode = ModeServe
	}
	if err := exposeReset(execRunner{}, mode); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	fmt.Printf("%s reset.\n", mode.label())
	return 0
}

func cmdStatus(argv []string) int {
	mode, cfg := modeAndConfig(argv)
	out, err := exposeStatus(execRunner{}, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "status: %v\n", err)
	} else {
		fmt.Printf("%s status:\n%s\n", mode.label(), out)
	}
	return printDiscovered(cfg, mode)
}

func cmdList(argv []string) int {
	mode, cfg := modeAndConfig(argv)
	return printDiscovered(cfg, mode)
}

func cmdDoctor(argv []string) int {
	mode, cfg := modeAndConfig(argv)
	if printChecks(runDoctor(execRunner{}, newDiscoverer(execRunner{}), cfg, mode)) {
		fmt.Println("\nAll checks passed — you're ready to `tsp start`.")
		return 0
	}
	return 1
}

// modeAndConfig parses the shared discovery/mode flags for list/status/doctor.
func modeAndConfig(argv []string) (Mode, discoverConfig) {
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	portsRaw := fs.String("ports", "3000-5000", "port range to scan")
	all := fs.Bool("all", false, "include all listeners")
	runtimesRaw := fs.String("runtimes", "", "comma-separated runtimes")
	private := fs.Bool("private", false, "private (Serve) mode")
	httpsPort := fs.Int("https-port", 443, "public/tailnet HTTPS port")
	_ = fs.Parse(argv)
	rng, err := parsePortRange(*portsRaw)
	if err != nil {
		rng = PortRange{Lo: 3000, Hi: 5000}
	}
	mode := ModeFunnel
	if *private {
		mode = ModeServe
	}
	cfg := discoverConfig{rng: rng, all: *all, runtimes: parseRuntimes(*runtimesRaw)}
	// stash httpsPort via a package-global is avoided; status/list read 443 unless set.
	queryHTTPSPort = *httpsPort
	return mode, cfg
}

// queryHTTPSPort carries --https-port from modeAndConfig to printDiscovered.
var queryHTTPSPort = 443

// printDiscovered lists discovered services with their public URLs.
func printDiscovered(cfg discoverConfig, mode Mode) int {
	svcs, err := newDiscoverer(execRunner{}).Discover(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discovery failed: %v\n", err)
		return 1
	}
	if len(svcs) == 0 {
		fmt.Printf("No services found in %d-%d. Start a dev server, widen --ports, or use --all.\n", cfg.rng.Lo, cfg.rng.Hi)
		return 0
	}
	kind := "public Funnel"
	if mode == ModeServe {
		kind = "private Serve"
	}
	fmt.Printf("Discovered services (ports %d-%d, %s):\n", cfg.rng.Lo, cfg.rng.Hi, kind)
	node, nerr := nodeDNSName(execRunner{})
	snap := map[string]Service{}
	for _, s := range svcs {
		snap[s.Slug] = s
	}
	for _, slug := range sortedSlugs(snap) {
		s := snap[slug]
		rt := s.Runtime
		if rt == "" {
			rt = "?"
		}
		dir := s.Dir
		if dir == "" {
			dir = "—"
		}
		fmt.Printf("  %-22s %-7s :%d   %s\n", slug, rt, s.Port, dir)
		if nerr == nil {
			fmt.Printf("    %s/%s/\n", publicBase(node, queryHTTPSPort), slug)
		}
	}
	return 0
}

// printServiceURLs prints each service's public URL and local target.
func printServiceURLs(snap map[string]Service, node string, httpsPort int) {
	base := publicBase(node, httpsPort)
	for _, slug := range sortedSlugs(snap) {
		fmt.Printf("  %s/%s/  →  127.0.0.1:%d\n", base, slug, snap[slug].Port)
	}
}

func sortedSlugs(snap map[string]Service) []string {
	out := make([]string, 0, len(snap))
	for s := range snap {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}
```

- [ ] **Step 4: Format, vet, build, test**

Run:
```bash
export GOROOT=/opt/homebrew/opt/go/libexec; export PATH="$GOROOT/bin:$PATH"
gofmt -w . && go vet ./... && go build -o tsp . && go test -count=1 ./...
GOOS=windows GOARCH=amd64 go build -o /dev/null .
```
Expected: vet clean, build ok, tests PASS, windows builds.

- [ ] **Step 5: Commit**

```bash
git add cli.go poll_test.go
git commit -m "feat: CLI for discovery + serve/funnel modes; tsp.log; new flags"
```

---

## Task 9.5: Self-update (`tsp update`)

**Files:** Create `update.go`, `update_test.go`; Modify `main.go` (dispatch)

- [ ] **Step 1: Write the failing tests (pure helpers)**

`update_test.go`:
```go
package main

import "testing"

func TestInstallMethod(t *testing.T) {
	cases := map[string]string{
		"/opt/homebrew/Cellar/tsp/0.1.0/bin/tsp":        "brew",
		"/opt/homebrew/Caskroom/tsp/0.1.0/tsp":          "brew",
		"/usr/local/Homebrew/bin/tsp":                   "brew",
		"/Users/me/.npm/_npx/abc/node_modules/.bin/tsp": "npm",
		"/Users/me/proj/node_modules/tailscale-proxy-darwin-arm64/bin/tsp": "npm",
		"/Users/me/bin/tsp":                             "standalone",
		"/usr/local/bin/tsp":                            "standalone",
	}
	for path, want := range cases {
		if got := installMethod(path); got != want {
			t.Errorf("installMethod(%q) = %q, want %q", path, got, want)
		}
	}
}

func TestNormalizeVer(t *testing.T) {
	if normalizeVer("v0.1.0") != normalizeVer("0.1.0") {
		t.Error("v-prefix should be ignored")
	}
	if normalizeVer(" 0.2.0 ") != "0.2.0" {
		t.Error("whitespace should be trimmed")
	}
}

func TestReleaseArchiveURL(t *testing.T) {
	got := releaseArchiveURL("v0.2.0", "darwin", "arm64")
	want := "https://github.com/meabed/tailscale-proxy/releases/download/v0.2.0/tsp_darwin_arm64.tar.gz"
	if got != want {
		t.Errorf("got %q", got)
	}
	if got := releaseArchiveURL("v0.2.0", "windows", "amd64"); !strings.HasSuffix(got, "tsp_windows_amd64.zip") {
		t.Errorf("windows should be .zip, got %q", got)
	}
}
```
Add `import "strings"` to the test file's imports (the last case uses it):
```go
import (
	"strings"
	"testing"
)
```

- [ ] **Step 2: Run to verify it fails**

Run: `go test ./... -run 'TestInstallMethod|TestNormalizeVer|TestReleaseArchiveURL'`
Expected: FAIL — undefined: installMethod, normalizeVer, releaseArchiveURL.

- [ ] **Step 3: Implement `update.go`**

```go
package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const repoSlug = "meabed/tailscale-proxy"

// cmdUpdate updates tsp to the latest release, or prints the right command for
// Homebrew/npm installs.
func cmdUpdate(argv []string) int {
	latest, err := latestVersion()
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not check latest version: %v\n", err)
		return 1
	}
	fmt.Printf("current: %s\nlatest:  %s\n", version, latest)
	if version != "dev" && normalizeVer(latest) == normalizeVer(version) {
		fmt.Println("already up to date.")
		return 0
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot locate executable: %v\n", err)
		return 1
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}

	switch installMethod(exe) {
	case "brew":
		fmt.Println("\nInstalled via Homebrew — update with:\n  brew upgrade tsp")
		return 0
	case "npm":
		fmt.Println("\nInstalled via npm — update with:\n  npm i -g tailscale-proxy@latest")
		return 0
	default:
		fmt.Printf("\nDownloading %s …\n", latest)
		if err := selfReplace(exe, latest); err != nil {
			fmt.Fprintf(os.Stderr, "self-update failed: %v\n", err)
			return 1
		}
		fmt.Printf("updated to %s\n", latest)
		return 0
	}
}

// installMethod classifies how the binary at path was installed.
func installMethod(path string) string {
	p := strings.ToLower(path)
	if strings.Contains(p, "/cellar/") || strings.Contains(p, "/caskroom/") || strings.Contains(p, "/homebrew/") {
		return "brew"
	}
	if strings.Contains(p, "/node_modules/") || strings.Contains(p, "/_npx/") {
		return "npm"
	}
	return "standalone"
}

// normalizeVer strips a leading v and surrounding whitespace.
func normalizeVer(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// releaseArchiveURL builds the GitHub release archive URL for an os/arch.
func releaseArchiveURL(tag, goos, goarch string) string {
	ext := "tar.gz"
	if goos == "windows" {
		ext = "zip"
	}
	return fmt.Sprintf("https://github.com/%s/releases/download/%s/tsp_%s_%s.%s",
		repoSlug, tag, goos, goarch, ext)
}

// latestVersion returns the latest release tag from the GitHub API.
func latestVersion() (string, error) {
	url := "https://api.github.com/repos/" + repoSlug + "/releases/latest"
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "tailscale-proxy-updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	var rel struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("no releases found")
	}
	return rel.TagName, nil
}

// selfReplace downloads the release binary and atomically replaces exe.
func selfReplace(exe, tag string) error {
	url := releaseArchiveURL(tag, runtime.GOOS, runtime.GOARCH)
	binName := "tsp"
	if runtime.GOOS == "windows" {
		binName = "tsp.exe"
	}

	tmp, err := os.CreateTemp(filepath.Dir(exe), ".tsp-dl-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)

	if err := downloadBinary(url, binName, tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o755); err != nil {
		return err
	}

	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		return os.Rename(tmpName, exe)
	}
	return os.Rename(tmpName, exe) // replaces the running binary on Unix
}

// downloadBinary fetches url and writes the named binary entry into out.
func downloadBinary(url, binName string, out *os.File) error {
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download %s returned %s", url, resp.Status)
	}
	if strings.HasSuffix(url, ".zip") {
		return extractZipEntry(resp.Body, binName, out)
	}
	return extractTarGzEntry(resp.Body, binName, out)
}

// extractTarGzEntry copies the binName file out of a .tar.gz stream.
func extractTarGzEntry(r io.Reader, binName string, out io.Writer) error {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return err
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return fmt.Errorf("%s not found in archive", binName)
		}
		if err != nil {
			return err
		}
		if filepath.Base(h.Name) == binName {
			_, err := io.Copy(out, tr)
			return err
		}
	}
}

// extractZipEntry copies the binName file out of a .zip stream (buffered to temp).
func extractZipEntry(r io.Reader, binName string, out io.Writer) error {
	tmp, err := os.CreateTemp("", "tsp-zip-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	n, err := io.Copy(tmp, r)
	if err != nil {
		return err
	}
	zr, err := zip.NewReader(tmp, n)
	if err != nil {
		return err
	}
	for _, f := range zr.File {
		if filepath.Base(f.Name) == binName {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			_, err = io.Copy(out, rc)
			return err
		}
	}
	return fmt.Errorf("%s not found in archive", binName)
}
```

- [ ] **Step 4: Add the `update` dispatch to `main.go`**

In `main.go`'s `run` switch, add a case alongside the others:
```go
	case "update":
		return cmdUpdate(argv[1:])
```

- [ ] **Step 5: Format, vet, test, build (all platforms)**

Run:
```bash
export GOROOT=/opt/homebrew/opt/go/libexec; export PATH="$GOROOT/bin:$PATH"
gofmt -w . && go vet ./... && go test -count=1 ./... 2>&1 | tail -3
go build -o tsp . && ./tsp update 2>&1 | head -3   # will report current/latest (or API error pre-release)
GOOS=windows GOARCH=amd64 go build -o /dev/null .
```
Expected: vet clean, tests PASS, builds; `tsp update` prints current/latest (or a clear "no releases found" before the first release).

- [ ] **Step 6: Commit**

```bash
git add update.go update_test.go main.go
git commit -m "feat: tsp update — self-update or brew/npm upgrade hint"
```

---

## Task 10: Manual end-to-end verification

**Files:** none

- [ ] **Step 1: Doctor and list against real listeners**

Run:
```bash
./tsp doctor
./tsp list
./tsp list --all
```
Expected: `doctor` shows ✓ for tailscale/up/funnel and a discovery count; `list` shows discovered web servers (the running bun/node dev servers) with project folders and `https://<node>.ts.net/<slug>/` URLs; `--all` additionally shows non-web listeners.

- [ ] **Step 2: Proxy-only routing smoke**

Run:
```bash
./tsp start --proxy-only --port 8830 >| /tmp/tsp.log 2>&1 &
sleep 2
SLUG=$(./tsp list | awk '/:[0-9]+/ {print $1; exit}')
echo "slug=$SLUG"
curl -s -o /dev/null -w "known -> %{http_code}\n" "http://127.0.0.1:8830/$SLUG/"
curl -s -o /dev/null -w "unknown -> %{http_code}\n" "http://127.0.0.1:8830/nope/"
kill %1 2>/dev/null; wait 2>/dev/null; true
```
Expected: known slug → 200 (or the app's status); unknown → 404.

- [ ] **Step 3: Commit any fixes**

```bash
git add -A && git commit -m "fix: e2e adjustments" || echo "no changes"
```

---

## Task 11: npm packaging rename (tailscale-proxy / tsp)

**Files:** `npm/portless-tailscale-proxy/`→`npm/tailscale-proxy/`, `npm/build-platform-packages.mjs`

- [ ] **Step 1: Rename the launcher package directory**

```bash
git mv npm/portless-tailscale-proxy npm/tailscale-proxy
```

- [ ] **Step 2: Rewrite `npm/tailscale-proxy/package.json`**

```json
{
  "name": "tailscale-proxy",
  "version": "0.0.0",
  "description": "Discover local dev servers by port and expose them through one Tailscale Serve/Funnel entry, routed by project name.",
  "bin": {
    "tailscale-proxy": "bin/launcher.js",
    "tsp": "bin/launcher.js"
  },
  "files": ["bin/launcher.js", "README.md"],
  "keywords": ["tailscale", "funnel", "serve", "proxy", "reverse-proxy", "dev", "discovery"],
  "license": "MIT",
  "repository": { "type": "git", "url": "git+https://github.com/meabed/tailscale-proxy.git" },
  "optionalDependencies": {
    "tailscale-proxy-darwin-arm64": "0.0.0",
    "tailscale-proxy-darwin-x64": "0.0.0",
    "tailscale-proxy-linux-x64": "0.0.0",
    "tailscale-proxy-linux-arm64": "0.0.0",
    "tailscale-proxy-win32-x64": "0.0.0",
    "tailscale-proxy-win32-arm64": "0.0.0"
  }
}
```

- [ ] **Step 3: Rewrite `npm/tailscale-proxy/bin/launcher.js`**

```js
#!/usr/bin/env node
"use strict";

const { spawnSync } = require("node:child_process");

function resolveBinary() {
  const platform = process.platform;
  const arch = process.arch;
  const pkg = `tailscale-proxy-${platform}-${arch}`;
  const exe = platform === "win32" ? "tsp.exe" : "tsp";
  try {
    return require.resolve(`${pkg}/bin/${exe}`);
  } catch {
    return null;
  }
}

const bin = resolveBinary();
if (!bin) {
  console.error(
    `tailscale-proxy: no prebuilt binary for ${process.platform}-${process.arch}.\n` +
      `Install from source: go install github.com/meabed/tailscale-proxy@latest\n` +
      `or download a release: https://github.com/meabed/tailscale-proxy/releases`
  );
  process.exit(1);
}

const res = spawnSync(bin, process.argv.slice(2), { stdio: "inherit" });
if (res.error) {
  console.error(res.error.message);
  process.exit(1);
}
process.exit(res.status === null ? 1 : res.status);
```

- [ ] **Step 4: Update `npm/build-platform-packages.mjs`**

Replace every `portless-tailscale-proxy` with `tailscale-proxy`, every `ptp` archive name with `tsp`, and the binary names. The `targets` array and archive references become:
```js
const targets = [
  ["darwin", "arm64", "tsp_darwin_arm64.tar.gz", "tsp", false],
  ["darwin", "x64", "tsp_darwin_amd64.tar.gz", "tsp", false],
  ["linux", "x64", "tsp_linux_amd64.tar.gz", "tsp", false],
  ["linux", "arm64", "tsp_linux_arm64.tar.gz", "tsp", false],
  ["win32", "x64", "tsp_windows_amd64.zip", "tsp.exe", true],
  ["win32", "arm64", "tsp_windows_arm64.zip", "tsp.exe", true],
];
```
And the package name/description:
```js
  const pkgName = `tailscale-proxy-${os}-${arch}`;
  ...
    name: pkgName,
    version,
    description: `Prebuilt tailscale-proxy binary for ${os}-${arch}.`,
    ...
    repository: { type: "git", url: "git+https://github.com/meabed/tailscale-proxy.git" },
```

- [ ] **Step 5: Validate JS syntax**

Run:
```bash
node --check npm/tailscale-proxy/bin/launcher.js
node --check npm/build-platform-packages.mjs
node -e "console.log('tailscale-proxy-'+process.platform+'-'+process.arch)"
```
Expected: no output from `--check`; prints this machine's platform package.

- [ ] **Step 6: Commit**

```bash
git add npm/
git commit -m "refactor: rename npm packages to tailscale-proxy, binary tsp"
```

---

## Task 12: Release config + CI rename (goreleaser, workflows); remove curl installer

**Files:** `.goreleaser.yaml`, `.github/workflows/release.yml`; Delete `install.sh`

Distribution is **npx + Homebrew** only. GitHub Releases remain (the artifact
source for both). The `curl | sh` installer is removed.

- [ ] **Step 1: Update `.goreleaser.yaml`**

Change `project_name`, binary, archive names, cask, and release repo:
```yaml
version: 2
project_name: tsp
before:
  hooks:
    - go mod tidy
builds:
  - id: tsp
    main: .
    binary: tsp
    env: [CGO_ENABLED=0]
    ldflags:
      - -s -w -X main.version={{.Version}}
    goos: [darwin, linux, windows]
    goarch: [amd64, arm64]
archives:
  - id: tsp
    name_template: "tsp_{{ .Os }}_{{ .Arch }}"
    formats: [tar.gz]
    format_overrides:
      - goos: windows
        formats: [zip]
checksum:
  name_template: "checksums.txt"
homebrew_casks:
  - name: tsp
    repository:
      owner: meabed
      name: homebrew-tap
    homepage: "https://github.com/meabed/tailscale-proxy"
    description: "Discover local dev servers and expose them through Tailscale Serve/Funnel"
    license: "MIT"
    binaries: [tsp]
    hooks:
      post:
        install: |
          if OS.mac?
            system_command "/usr/bin/xattr", args: ["-dr", "com.apple.quarantine", "#{staged_path}/tsp"]
          end
release:
  github:
    owner: meabed
    name: tailscale-proxy
```

- [ ] **Step 2: Update `.github/workflows/release.yml`**

In the "Publish launcher package" step, change the directory:
```yaml
          cd npm/tailscale-proxy
```
(The action versions and the rest stay as-is.)

- [ ] **Step 3: Remove the curl installer**

```bash
git rm install.sh
```

- [ ] **Step 4: Validate**

Run:
```bash
export PATH="/opt/homebrew/bin:$PATH"
goreleaser check
```
Expected: `1 configuration file(s) validated`.

- [ ] **Step 5: Commit**

```bash
git add .goreleaser.yaml .github/workflows/release.yml
git commit -m "ci: rename release artifacts to tsp/tailscale-proxy; drop curl installer"
```

---

## Task 13: Docs rewrite (README + docs/), remove all portless

**Files:** `README.md`, `npm/tailscale-proxy/README.md`, `docs/HOW-IT-WORKS.md`, `docs/TROUBLESHOOTING.md`, `docs/RELEASING.md`

- [ ] **Step 1: Rewrite `README.md`**

Replace the entire file with a version that:
- Titles it `# tailscale-proxy (\`tsp\`)`, badges point to `meabed/tailscale-proxy`.
- Explains port discovery (default 3000-5000, runtimes node/bun/deno), project-name path slugs, and the private (Serve) vs public (Funnel) modes.
- **Install = npx + Homebrew only:**
  ```bash
  npx tailscale-proxy doctor          # or: npm i -g tailscale-proxy
  brew install meabed/tap/tsp
  ```
  (No `curl | sh`; `go install` may be mentioned once as a footnote, not in the table.)
- Commands section: `start/status/list/reset/doctor/update` with the new flags (`--ports`, `--all`, `--runtimes`, `--private`, `--https-port`, `--proxy-only`, `--quiet`, `--log-requests`).
- An **Updating** section: `tsp update` (self-updates a standalone binary, or prints `brew upgrade tsp` / `npm i -g tailscale-proxy@latest` for managed installs).
- Requirements: Tailscale (Funnel enabled only needed for public mode) + `lsof` on macOS/Linux; no portless.
- Keeps the request-logging and per-service-URL sections.
- Contains zero "portless" strings.

- [ ] **Step 2: Rewrite the docs guides**

- `docs/HOW-IT-WORKS.md`: replace the portless/routes.json description with the discovery pipeline (listeners → classify → project-root slug → filter → de-dup), the unchanged proxy, and the Serve/Funnel `expose` layer. Update the diagram so the bottom feeds from "port scan (lsof/netstat) every --interval".
- `docs/TROUBLESHOOTING.md`: keep the MagicDNS gotcha; replace portless-specific items with: "no services found" (start a server, `--all`, widen `--ports`), "`lsof` not found", "wrong project name / slug collision (`-<port>` suffix)", and the Serve-vs-Funnel note. Replace `ptp`→`tsp`.
- `docs/RELEASING.md`: replace `ptp`→`tsp`, repo `portless-tailscale-proxy`→`tailscale-proxy`, npm dir `npm/tailscale-proxy`.

- [ ] **Step 3: Sync npm README and scan for stragglers**

Run:
```bash
/bin/cp -f README.md npm/tailscale-proxy/README.md
grep -rni "portless" . --include='*.go' --include='*.md' --include='*.json' --include='*.mjs' --include='*.yaml' --include='*.yml' --include='*.sh' \
  --exclude-dir=docs/superpowers | grep -v 'docs/superpowers' || echo "no portless references remain (outside historical specs)"
```
Expected: prints "no portless references remain…".

- [ ] **Step 4: Commit**

```bash
git add README.md npm/tailscale-proxy/README.md docs/HOW-IT-WORKS.md docs/TROUBLESHOOTING.md docs/RELEASING.md
git commit -m "docs: rewrite for tailscale-proxy (discovery, serve/funnel); remove all portless references"
```

---

## Task 14: Rename the GitHub repo and finalize

**Files:** none (remote operations) + final verification

- [ ] **Step 1: Full local verification**

Run:
```bash
export GOROOT=/opt/homebrew/opt/go/libexec; export PATH="$GOROOT/bin:/opt/homebrew/bin:$PATH"
gofmt -l . ; go vet ./... && go test -count=1 ./... && go build -o tsp . && ./tsp --version
GOOS=windows GOARCH=amd64 go build -o /dev/null . && echo "windows ok"
```
Expected: gofmt clean, vet clean, tests PASS, builds, windows builds.

- [ ] **Step 2: Rename the GitHub repository**

Run:
```bash
export PATH="/opt/homebrew/bin:$PATH"
gh repo rename tailscale-proxy --repo meabed/portless-tailscale-proxy --yes
git remote set-url origin git@github.com:meabed/tailscale-proxy.git
git remote -v
```
Expected: repo renamed; origin points at the new URL. (GitHub redirects the old name.)

- [ ] **Step 3: Push and confirm CI**

```bash
git push origin main
RID=$(gh run list --workflow=ci.yml --limit 1 --json databaseId -q '.[0].databaseId')
gh run watch "$RID" --exit-status
```
Expected: CI green on Linux/macOS/Windows + cross-compile.

- [ ] **Step 4: Final commit (if anything pending)**

```bash
git add -A && git commit -m "chore: finalize tailscale-proxy rename" || echo "clean"
git push origin main
```

---

## Self-review notes

- **Spec coverage:** discovery (Tasks 2–5), runtime filter node/bun/deno + `--all`/`--runtimes` (Task 2/9), project-root slug + collisions (Task 2), proxy unchanged + index (Task 6), Serve/Funnel modes + public default (Task 7/9), doctor (Task 8), CLI flags incl. `--ports`/`--private`/`--https-port`/`--proxy-only` (Task 9), self-update (Task 9.5), Windows best-effort (Task 4), rename across module/binary/npm/goreleaser/docs/repo (Tasks 1, 11–14), request logging retained (Task 6/9).
- **Amendment coverage:** runtimes trimmed to node/bun/deno (Task 2); distribution npx + brew only, `install.sh` removed (Task 12); `tsp update` (Task 9.5, dispatch in `main.go`, docs in Task 13).
- **Deviation:** Unix discovery uses `lsof` (not `/proc`); flagged in the header and `doctor`. macOS + Linux share `discover_unix.go`.
- **Type consistency:** `Service{Slug,Port,Runtime,Dir,PID}`, `PortRange{Lo,Hi}`, `discoverConfig{rng,all,runtimes}`, `Discoverer.Discover(cfg)`, `RouteStore` via `NewRouteStore(func() ([]Service, error))` with `lookup/snapshot/refresh`, `Mode{ModeFunnel,ModeServe}` with `subcommand/label`, `exposeArgs/Start/Reset/Status`, `newHandler(store, logRequests)`, `runDoctor(r, disc, cfg, mode)` — used consistently across tasks.
- **`portFromAddr`** is defined once per build (unix file and windows file), never both in one build.
- **Removed:** `routes.go`, `routes_test.go`, `funnel.go`, `funnel_test.go`; `ptp.log`→`tsp.log`.
