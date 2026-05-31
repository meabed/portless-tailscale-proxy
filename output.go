package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// printStartHeader shows which config is in effect and the resolved parameters.
func printStartHeader(o startOpts, mode Mode, rng PortRange, cfgPath string, existed bool) {
	if existed {
		fmt.Printf("Using config: %s\n", cfgPath)
	} else {
		fmt.Println("No config file (built-in defaults) — save one with `tsp configure`")
	}
	ports := fmt.Sprintf("%d-%d", rng.Lo, rng.Hi)
	if rng.Lo == rng.Hi {
		ports = strconv.Itoa(rng.Lo)
	}
	runtimes := "default (" + strings.Join(knownRuntimeLabels(), ", ") + ")"
	switch {
	case o.all:
		runtimes = "all (--all)"
	case strings.TrimSpace(o.runtimesRaw) != "":
		runtimes = o.runtimesRaw
	}
	kind := "public (Funnel)"
	if mode == ModeServe {
		kind = "private (Serve)"
	}
	hostMode := "local (apps see localhost)"
	if o.forwardHost {
		hostMode = "forwarded (public host via X-Forwarded-*)"
	}
	fmt.Printf("  ports=%s  mode=%s  proxy=127.0.0.1:%d  https=%d\n", ports, kind, o.port, o.httpsPort)
	fmt.Printf("  interval=%ds  runtimes=%s  deregister-after=%d scans  log-requests=%t\n",
		o.interval, runtimes, o.deregisterCycles, o.logRequests)
	fmt.Printf("  host=%s\n\n", hostMode)
}

// printServiceURLs prints each service's public URL and local target.
func printServiceURLs(snap map[string]Service, node string, httpsPort int) {
	base := publicBase(node, httpsPort)
	for _, slug := range sortedSlugs(snap) {
		fmt.Printf("  %s/%s/  →  127.0.0.1:%d\n", base, slug, snap[slug].Port)
	}
}

// printDiscovered lists discovered services with their public URLs (for
// `tsp list` / `tsp status`). Returns a process exit code.
func printDiscovered(dcfg discoverConfig, mode Mode, httpsPort int) int {
	svcs, dups, err := newDiscoverer(execRunner{}).Discover(dcfg)
	if err != nil {
		fmt.Println("discovery failed:", err)
		return 1
	}
	if len(svcs) == 0 {
		fmt.Printf("No services found in %d-%d. Start a dev server, widen --ports, or use --all.\n", dcfg.rng.Lo, dcfg.rng.Hi)
		return 0
	}
	kind := "public Funnel"
	if mode == ModeServe {
		kind = "private Serve"
	}
	fmt.Printf("Discovered services (ports %d-%d, %s):\n", dcfg.rng.Lo, dcfg.rng.Hi, kind)
	node, nerr := nodeDNSName(execRunner{})
	snap := make(map[string]Service, len(svcs))
	for _, s := range svcs {
		snap[s.Slug] = s
	}
	for _, slug := range sortedSlugs(snap) {
		s := snap[slug]
		fmt.Printf("  %-26s %-6s :%d  pid %d  %s\n", slug, runtimeOr(s.Runtime), s.Port, s.PID, dirOr(s.Dir))
		if nerr == nil {
			fmt.Printf("    %s/%s/\n", publicBase(node, httpsPort), slug)
		}
	}
	printDuplicateNotes(dups)
	return 0
}

// printDuplicateNotes warns about projects listening on multiple ports and which
// instance is being served (the most recent), so the choice is transparent.
func printDuplicateNotes(dups []Duplicate) {
	if len(dups) == 0 {
		return
	}
	fmt.Println("\nNote — these projects listen on multiple ports; serving the most recent:")
	for _, d := range dups {
		fmt.Printf("  %s: %s\n", d.Slug, portList(d))
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

func runtimeOr(rt string) string {
	if rt == "" {
		return "?"
	}
	return rt
}

func dirOr(dir string) string {
	if dir == "" {
		return "—"
	}
	return dir
}

// portList renders all instance ports for a duplicate, marking the served one.
func portList(d Duplicate) string {
	all := append([]Service{d.Chosen}, d.Others...)
	parts := make([]string, 0, len(all))
	for _, s := range all {
		p := ":" + strconv.Itoa(s.Port) + "(pid " + strconv.Itoa(s.PID) + ")"
		if s.Port == d.Chosen.Port && s.PID == d.Chosen.PID {
			p += "←used"
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ", ")
}

// dupKey is a stable fingerprint of the duplicate set, for change detection.
func dupKey(dups []Duplicate) string {
	var b strings.Builder
	for _, d := range dups {
		b.WriteString(d.Slug)
		b.WriteByte('=')
		b.WriteString(strconv.Itoa(d.Chosen.Port))
		for _, o := range d.Others {
			b.WriteByte(',')
			b.WriteString(strconv.Itoa(o.Port))
		}
		b.WriteByte(';')
	}
	return b.String()
}
