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

// knownRuntimes maps a process's executable basename to a runtime label. These
// are the runtimes discovered by default; --all includes everything, --runtimes
// restricts to a subset. Server wrappers (uvicorn, puma, …) map to their language.
var knownRuntimes = map[string]string{
	// JavaScript / TypeScript
	"node": "node", "bun": "bun", "deno": "deno",
	// Python (interpreter + common app servers)
	"python": "python", "python2": "python", "python3": "python",
	"uvicorn": "python", "gunicorn": "python", "hypercorn": "python",
	"flask": "python", "waitress-serve": "python",
	// Ruby (interpreter + common app servers)
	"ruby": "ruby", "puma": "ruby", "unicorn": "ruby", "rackup": "ruby",
	"rails": "ruby", "thin": "ruby",
	// PHP
	"php": "php", "php-fpm": "php",
	// Go (compiled binaries are undetectable; `go run` is caught by heuristic below)
	"go": "go",
	// JVM
	"java": "java",
	// .NET
	"dotnet": "dotnet",
	// Elixir / Erlang
	"beam": "elixir", "beam.smp": "elixir",
	// Perl
	"perl": "perl",
	// Docker-published ports (the userland forwarder serves the host port)
	"docker-proxy": "docker", "com.docker.backend": "docker",
	"vpnkit-forwarder": "docker", "vpnkit": "docker",
}

// projectMarkers identify a project root when walking up from a process's cwd.
var projectMarkers = []string{
	".git",
	"package.json", "deno.json", "bun.lockb",
	"go.mod",
	"pyproject.toml", "requirements.txt", "Pipfile", "setup.py",
	"Gemfile",
	"composer.json",
	"Cargo.toml",
	"pom.xml", "build.gradle", "build.gradle.kts",
	"mix.exs",
}

// classifyRuntime maps an executable path to a runtime label, or "" if unknown.
func classifyRuntime(comm string) string {
	base := strings.ToLower(filepath.Base(comm))
	base = strings.TrimSuffix(base, ".exe")
	if rt, ok := knownRuntimes[base]; ok {
		return rt
	}
	switch {
	case strings.HasPrefix(base, "python"): // python3.12, python3.13, …
		return "python"
	case strings.Contains(comm, "go-build"): // `go run` temp binary
		return "go"
	case strings.Contains(base, "docker"): // docker-proxy variants
		return "docker"
	}
	return ""
}

// knownRuntimeLabels returns the distinct runtime labels, sorted — used in help
// and the startup banner so the default set is self-documenting.
func knownRuntimeLabels() []string {
	set := map[string]bool{}
	for _, label := range knownRuntimes {
		set[label] = true
	}
	out := make([]string, 0, len(set))
	for label := range set {
		out = append(out, label)
	}
	sort.Strings(out)
	return out
}

// projectRootDir walks up from dir to the nearest directory containing a project
// marker and returns that directory's path; falls back to dir itself, or "".
func projectRootDir(dir string) string {
	if dir == "" || dir == "/" {
		return ""
	}
	d := dir
	for {
		for _, m := range projectMarkers {
			if _, err := os.Stat(filepath.Join(d, m)); err == nil {
				return d
			}
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
		d = parent
	}
	return dir
}

// projectRoot returns the basename of the project root for dir (or "").
func projectRoot(dir string) string {
	rd := projectRootDir(dir)
	if rd == "" {
		return ""
	}
	return filepath.Base(rd)
}

var slugUnsafe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify normalizes a name into a URL path segment.
func slugify(name string) string {
	s := strings.ToLower(name)
	s = slugUnsafe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

// Duplicate records a project folder that exposes more than one service (more
// than one distinct process listening in it). Members holds them all, main first.
type Duplicate struct {
	Slug    string
	Members []Service // all services for this project, main (clean slug) first
}

// serviceOf builds a Service from a raw listener under a given slug.
func serviceOf(l listener, slug string) Service {
	return Service{Slug: slug, Port: l.Port, Runtime: classifyRuntime(l.Comm), Dir: l.Cwd, PID: l.PID}
}

// projectBaseSlug derives the clean project slug from a working directory, or
// "<runtime>-<port>" / "port-<port>" when there's no discoverable project.
func projectBaseSlug(l listener) string {
	slug := slugify(projectRoot(l.Cwd))
	if slug != "" {
		return slug
	}
	if rt := classifyRuntime(l.Comm); rt != "" {
		return rt + "-" + strconv.Itoa(l.Port)
	}
	return "port-" + strconv.Itoa(l.Port)
}

// buildServices turns raw listeners into routable services.
//
//   - Listeners are grouped by project-root directory.
//   - Multiple ports from the SAME process (e.g. a dev server + its HMR port)
//     collapse to that process's lowest port — one entry per process.
//   - Within a project, the process on the LOWEST port is the "main" service and
//     gets the clean project slug; every other distinct process is kept too, with
//     a "-<port>" suffix (so an auxiliary tool in the same folder stays reachable).
//   - Distinct projects that share a folder name get a "-<port>" suffix on their
//     main slug to stay unique.
//
// Projects exposing more than one service are returned as Duplicates for display.
func buildServices(listeners []listener, includeAll bool, runtimes map[string]bool) ([]Service, []Duplicate) {
	// Group surviving listeners by project-root dir (ungroupable → unique key).
	groups := map[string][]listener{}
	order := []string{}
	for _, l := range listeners {
		rt := classifyRuntime(l.Comm)
		if !includeAll && (rt == "" || (runtimes != nil && !runtimes[rt])) {
			continue
		}
		key := projectRootDir(l.Cwd)
		if key == "" {
			key = "\x00port:" + strconv.Itoa(l.Port)
		}
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], l)
	}

	// Per project: one entry per process (its lowest port), sorted by port.
	type project struct {
		procs []listener // distinct processes, lowest-port first; procs[0] is "main"
		slug  string     // base (clean) slug
	}
	projects := make([]project, 0, len(order))
	slugCounts := map[string]int{}
	for _, key := range order {
		byPID := map[int]listener{}
		pidOrder := []int{}
		for _, l := range groups[key] {
			if cur, ok := byPID[l.PID]; !ok {
				byPID[l.PID] = l
				pidOrder = append(pidOrder, l.PID)
			} else if l.Port < cur.Port {
				byPID[l.PID] = l
			}
		}
		procs := make([]listener, 0, len(pidOrder))
		for _, pid := range pidOrder {
			procs = append(procs, byPID[pid])
		}
		sort.Slice(procs, func(i, j int) bool { return procs[i].Port < procs[j].Port })
		slug := projectBaseSlug(procs[0])
		projects = append(projects, project{procs: procs, slug: slug})
		slugCounts[slug]++
	}

	var services []Service
	var dups []Duplicate
	for _, p := range projects {
		mainSlug := p.slug
		if slugCounts[mainSlug] > 1 { // distinct projects share a folder name
			mainSlug = mainSlug + "-" + strconv.Itoa(p.procs[0].Port)
		}
		members := make([]Service, 0, len(p.procs))
		for i, proc := range p.procs {
			slug := mainSlug
			if i > 0 { // not the main process → suffix to stay reachable
				slug = mainSlug + "-" + strconv.Itoa(proc.Port)
			}
			svc := serviceOf(proc, slug)
			members = append(members, svc)
			services = append(services, svc)
		}
		if len(members) > 1 {
			dups = append(dups, Duplicate{Slug: mainSlug, Members: members})
		}
	}
	sort.Slice(services, func(i, j int) bool { return services[i].Slug < services[j].Slug })
	sort.Slice(dups, func(i, j int) bool { return dups[i].Slug < dups[j].Slug })
	return services, dups
}

// parsePortRange parses "lo-hi" or a single "port" into a validated PortRange.
func parsePortRange(s string) (PortRange, error) {
	s = strings.TrimSpace(s)
	// Single port, e.g. "4000" -> {4000, 4000}.
	if !strings.Contains(s, "-") {
		p, err := strconv.Atoi(s)
		if err != nil || p < 1 || p > 65535 {
			return PortRange{}, fmt.Errorf("invalid port %q", s)
		}
		return PortRange{Lo: p, Hi: p}, nil
	}
	parts := strings.SplitN(s, "-", 2)
	lo, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	hi, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || lo < 1 || hi < lo || hi > 65535 {
		return PortRange{}, fmt.Errorf("invalid port range %q (want lo-hi or a single port)", s)
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

// Discover returns the filtered services in the configured range, plus any
// duplicate-instance info (same project on multiple ports).
func (d *Discoverer) Discover(cfg discoverConfig) ([]Service, []Duplicate, error) {
	ls, err := d.listeners(cfg.rng)
	if err != nil {
		return nil, nil, err
	}
	svcs, dups := buildServices(ls, cfg.all, cfg.runtimes)
	return svcs, dups, nil
}
