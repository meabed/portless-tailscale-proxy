package core

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
	bind := o.bind
	if bind == "" {
		bind = "127.0.0.1"
	}
	fmt.Printf("  ports=%s  mode=%s  proxy=%s:%d  https=%d\n", ports, kind, bind, o.port, o.httpsPort)
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

// printDuplicateNotes explains projects that expose more than one service (the
// main process gets the clean slug; others are suffixed), so it's transparent.
func printDuplicateNotes(dups []Duplicate) {
	if len(dups) == 0 {
		return
	}
	fmt.Println("\nNote — these projects expose multiple services (main + suffixed):")
	for _, d := range dups {
		fmt.Printf("  %s:\n", d.Members[0].Dir)
		for i, m := range d.Members {
			tag := ""
			if i == 0 {
				tag = "  [main]"
			}
			fmt.Printf("    /%s/  →  :%d (%s, pid %d)%s\n", m.Slug, m.Port, runtimeOr(m.Runtime), m.PID, tag)
		}
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

// portList renders a project's services on one line: "/slug/ :port(runtime)".
func portList(d Duplicate) string {
	parts := make([]string, 0, len(d.Members))
	for _, m := range d.Members {
		parts = append(parts, "/"+m.Slug+"/ :"+strconv.Itoa(m.Port)+"("+runtimeOr(m.Runtime)+")")
	}
	return strings.Join(parts, ", ")
}

// dupKey is a stable fingerprint of the duplicate set, for change detection.
func dupKey(dups []Duplicate) string {
	var b strings.Builder
	for _, d := range dups {
		b.WriteString(d.Slug)
		b.WriteByte('=')
		for _, m := range d.Members {
			b.WriteString(m.Slug)
			b.WriteByte(':')
			b.WriteString(strconv.Itoa(m.Port))
			b.WriteByte(',')
		}
		b.WriteByte(';')
	}
	return b.String()
}
