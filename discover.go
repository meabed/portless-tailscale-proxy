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

// Duplicate records that several listeners mapped to one project slug: the one
// chosen to serve (most recent), plus the others that were dropped.
type Duplicate struct {
	Slug   string
	Chosen Service
	Others []Service // dropped instances (older), sorted by port
}

// serviceOf builds a Service from a raw listener.
func serviceOf(l listener, slug string) Service {
	return Service{Slug: slug, Port: l.Port, Runtime: classifyRuntime(l.Comm), Dir: l.Cwd, PID: l.PID}
}

// moreRecent reports whether listener a is "more recent" than b: a newer process
// (higher PID), tie-broken by the higher port. Used to pick which instance of a
// project to serve when the same project listens on several ports.
func moreRecent(a, b listener) bool {
	if a.PID != b.PID {
		return a.PID > b.PID
	}
	return a.Port > b.Port
}

// baseSlug derives a service slug from a listener: the project-root folder name,
// or "<runtime>-<port>" / "port-<port>" when there's no discoverable project.
func baseSlug(l listener) string {
	slug := slugify(projectRoot(l.Cwd))
	if slug != "" {
		return slug
	}
	if rt := classifyRuntime(l.Comm); rt != "" {
		return rt + "-" + strconv.Itoa(l.Port)
	}
	return "port-" + strconv.Itoa(l.Port)
}

// buildServices filters listeners to web runtimes and collapses every listener
// that belongs to the same project (same project-root directory) into ONE
// service — the most recent instance — so the URL path is just the project name
// with no port suffix. Genuinely-distinct projects that happen to share a folder
// name get a "-<port>" suffix to stay unique. Dropped instances are returned as
// Duplicates so the caller can surface them.
func buildServices(listeners []listener, includeAll bool, runtimes map[string]bool) ([]Service, []Duplicate) {
	// Group by project-root directory; listeners with no discoverable project are
	// each their own group (nothing to collapse on).
	groups := map[string][]listener{}
	order := []string{}
	for _, l := range listeners {
		rt := classifyRuntime(l.Comm)
		if !includeAll {
			if rt == "" || (runtimes != nil && !runtimes[rt]) {
				continue
			}
		}
		key := projectRootDir(l.Cwd)
		if key == "" {
			key = "\x00port:" + strconv.Itoa(l.Port) // ungroupable, unique per listener
		}
		if _, seen := groups[key]; !seen {
			order = append(order, key)
		}
		groups[key] = append(groups[key], l)
	}

	// Pick the most-recent instance per group and its base slug.
	type chosenGroup struct {
		ls   []listener
		best listener
		slug string
	}
	chosen := make([]chosenGroup, 0, len(order))
	slugCounts := map[string]int{}
	for _, key := range order {
		ls := groups[key]
		best := ls[0]
		for _, l := range ls[1:] {
			if moreRecent(l, best) {
				best = l
			}
		}
		slug := baseSlug(best)
		chosen = append(chosen, chosenGroup{ls: ls, best: best, slug: slug})
		slugCounts[slug]++
	}

	// Different projects sharing a folder name → suffix with port to disambiguate.
	var services []Service
	var dups []Duplicate
	for _, g := range chosen {
		slug := g.slug
		if slugCounts[slug] > 1 {
			slug = slug + "-" + strconv.Itoa(g.best.Port)
		}
		chosenSvc := serviceOf(g.best, slug)
		services = append(services, chosenSvc)
		if len(g.ls) > 1 {
			others := make([]Service, 0, len(g.ls)-1)
			for _, l := range g.ls {
				if l.PID == g.best.PID && l.Port == g.best.Port {
					continue
				}
				others = append(others, serviceOf(l, slug))
			}
			sort.Slice(others, func(i, j int) bool { return others[i].Port < others[j].Port })
			dups = append(dups, Duplicate{Slug: slug, Chosen: chosenSvc, Others: others})
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
