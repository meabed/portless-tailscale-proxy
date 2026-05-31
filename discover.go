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

// disambiguate appends -<port> to any slug shared by more than one service, then
// -<pid> as a final tie-break if the -<port> suffix still collides (e.g. a slug
// that already had the -<port> form). PIDs are unique, so this always converges.
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
	counts2 := map[string]int{}
	for _, s := range svcs {
		counts2[s.Slug]++
	}
	for i := range svcs {
		if counts2[svcs[i].Slug] > 1 {
			svcs[i].Slug = svcs[i].Slug + "-" + strconv.Itoa(svcs[i].PID)
		}
	}
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

// Discover returns the filtered services in the configured range.
func (d *Discoverer) Discover(cfg discoverConfig) ([]Service, error) {
	ls, err := d.listeners(cfg.rng)
	if err != nil {
		return nil, err
	}
	return buildServices(ls, cfg.all, cfg.runtimes), nil
}
