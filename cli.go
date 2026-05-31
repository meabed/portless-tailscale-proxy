package main

import (
	"context"
	"encoding/json"
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
  tsp [flags]              # default: run "start" with your saved config
  tsp <command> [flags]

Commands:
  start      Discover services, run the proxy, and expose it (Serve or Funnel)
  status     Print Serve/Funnel status and the current service map
  list       Print discovered services (slug → runtime, port, project, URL)
  reset      Remove the Serve/Funnel entry and exit
  doctor     Check tailscale, exposure readiness, and discovery
  configure  Save defaults to ~/.tailscale-proxy/config.json
  update     Update tsp to the latest release (or show the brew/npm command)

Examples:
  tsp                        # start using saved config (or built-in defaults)
  tsp --private              # start privately (Serve) this once
  tsp configure --ports 3000-9000 --private   # save, then just run "tsp"
  tsp list                   # see discovered services + URLs

Run "tsp start --help" for all flags. Global: -h/--help, -v/--version
Docs: https://github.com/meabed/tailscale-proxy
`)
}

func startUsage() {
	fmt.Print(`tsp start — discover services, run the proxy, and expose it

Usage:
  tsp start [flags]     (also the default: plain "tsp" runs this)

Flags (defaults come from ~/.tailscale-proxy/config.json if present):
  --ports <lo-hi|port>   Port range or single port to scan   (default 3000-5000)
  --all                  Include all listeners, not just web runtimes
  --runtimes <list>      Comma-separated runtimes (default node,bun,deno)
  --private              Expose privately via Tailscale Serve (default: Funnel)
  --port <n>             Local proxy HTTP port                (default 8443)
  --interval <sec>       Re-scan period in seconds            (default 20)
  --https-port <n>       Public/tailnet HTTPS port            (default 443)
  --deregister-cycles <n> Missing scans before a gone service is removed (default 5)
  --bg                   Run tsp detached in the background (logs → ./tsp.log)
  --proxy-only           Run the proxy only; print the tailscale command
  --log-requests         Log each proxied request             (default on)
  --quiet                Disable per-request logging
  -h, --help             Show this help

Press Ctrl-C to stop — the Serve/Funnel entry is reset automatically on exit.
`)
}

type startOpts struct {
	portsRaw         string
	all              bool
	runtimesRaw      string
	private          bool
	port             int
	interval         int
	httpsPort        int
	deregisterCycles int
	bg               bool
	proxyOnly        bool
	logRequests      bool
	quiet            bool
}

// modeOf returns the exposure mode for the private flag.
func modeOf(private bool) Mode {
	if private {
		return ModeServe
	}
	return ModeFunnel
}

func cmdStart(argv []string) int {
	cfg, cfgPath, existed, cfgErr := loadConfig()
	if cfgErr != nil {
		fmt.Fprintf(os.Stderr, "warning: reading %s: %v (using defaults)\n", cfgPath, cfgErr)
	}

	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.Usage = startUsage
	var o startOpts
	fs.StringVar(&o.portsRaw, "ports", cfg.Ports, "port range or single port to scan")
	fs.BoolVar(&o.all, "all", cfg.All, "include all listeners")
	fs.StringVar(&o.runtimesRaw, "runtimes", cfg.Runtimes, "comma-separated runtimes")
	fs.BoolVar(&o.private, "private", cfg.Private, "expose via Tailscale Serve (private)")
	fs.IntVar(&o.port, "port", cfg.Port, "local proxy HTTP port")
	fs.IntVar(&o.interval, "interval", cfg.Interval, "re-scan period (seconds)")
	fs.IntVar(&o.httpsPort, "https-port", cfg.HTTPSPort, "public/tailnet HTTPS port")
	fs.IntVar(&o.deregisterCycles, "deregister-cycles", cfg.DeregisterCycles, "missing scans before removal")
	fs.BoolVar(&o.logRequests, "log-requests", cfg.LogRequests, "log each proxied request")
	fs.BoolVar(&o.quiet, "quiet", false, "disable per-request logging")
	fs.BoolVar(&o.bg, "bg", false, "run detached in background")
	var fg bool
	fs.BoolVar(&fg, "fg", false, "run in foreground (default)")
	fs.BoolVar(&o.proxyOnly, "proxy-only", false, "proxy only; print tailscale command")
	if err := fs.Parse(argv); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	mode := modeOf(o.private)
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

	printStartHeader(o, mode, rng, cfgPath, existed)

	runner := execRunner{}
	dcfg := discoverConfig{rng: rng, all: o.all, runtimes: parseRuntimes(o.runtimesRaw)}
	disc := newDiscoverer(runner)

	if !printChecks(runDoctor(runner, disc, dcfg, mode)) && !o.proxyOnly {
		fmt.Fprintln(os.Stderr, "\npreflight failed — fix the items above, or use --proxy-only to run the proxy alone")
		return 1
	}

	// One Discoverer + one store, refreshed on a ticker. The store debounces
	// de-registration so brief restarts don't flap routes.
	store := NewRouteStore(func() ([]Service, []Duplicate, error) { return disc.Discover(dcfg) }, o.deregisterCycles)
	if _, _, _, err := store.refresh(); err != nil {
		log.Printf("warn: initial discovery failed: %v", err)
	}

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
		printDuplicateNotes(store.dupes())
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

// printStartHeader shows which config is in effect and the resolved parameters.
func printStartHeader(o startOpts, mode Mode, rng PortRange, cfgPath string, existed bool) {
	if existed {
		fmt.Printf("Using config: %s\n", cfgPath)
	} else {
		fmt.Printf("No config file (built-in defaults) — save one with `tsp configure`\n")
	}
	ports := fmt.Sprintf("%d-%d", rng.Lo, rng.Hi)
	if rng.Lo == rng.Hi {
		ports = fmt.Sprintf("%d", rng.Lo)
	}
	runtimes := "node,bun,deno (default)"
	if o.all {
		runtimes = "all (--all)"
	} else if strings.TrimSpace(o.runtimesRaw) != "" {
		runtimes = o.runtimesRaw
	}
	kind := "public (Funnel)"
	if mode == ModeServe {
		kind = "private (Serve)"
	}
	fmt.Printf("  ports=%s  mode=%s  proxy=127.0.0.1:%d  https=%d\n", ports, kind, o.port, o.httpsPort)
	fmt.Printf("  interval=%ds  runtimes=%s  deregister-after=%d scans  log-requests=%t\n\n",
		o.interval, runtimes, o.deregisterCycles, o.logRequests)
}

func cmdConfigure(argv []string) int {
	cfg, _, _, _ := loadConfig()
	fs := flag.NewFlagSet("configure", flag.ContinueOnError)
	fs.StringVar(&cfg.Ports, "ports", cfg.Ports, "port range or single port to scan")
	fs.BoolVar(&cfg.All, "all", cfg.All, "include all listeners")
	fs.StringVar(&cfg.Runtimes, "runtimes", cfg.Runtimes, "comma-separated runtimes")
	fs.BoolVar(&cfg.Private, "private", cfg.Private, "expose privately via Serve")
	fs.IntVar(&cfg.Port, "port", cfg.Port, "local proxy HTTP port")
	fs.IntVar(&cfg.Interval, "interval", cfg.Interval, "re-scan period (seconds)")
	fs.IntVar(&cfg.HTTPSPort, "https-port", cfg.HTTPSPort, "public/tailnet HTTPS port")
	fs.IntVar(&cfg.DeregisterCycles, "deregister-cycles", cfg.DeregisterCycles, "missing scans before removal")
	fs.BoolVar(&cfg.LogRequests, "log-requests", cfg.LogRequests, "log each proxied request")
	if err := fs.Parse(argv); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	// Validate before saving.
	if _, err := parsePortRange(cfg.Ports); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 2
	}
	path, err := saveConfig(cfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "could not save config: %v\n", err)
		return 1
	}
	out, _ := json.MarshalIndent(cfg, "", "  ")
	fmt.Printf("Saved %s:\n%s\n\nRun `tsp` to start with this config.\n", path, out)
	return 0
}

func cmdReset(argv []string) int {
	cfg, _, _, _ := loadConfig()
	fs := flag.NewFlagSet("reset", flag.ContinueOnError)
	private := fs.Bool("private", cfg.Private, "reset the Serve entry instead of Funnel")
	_ = fs.Parse(argv)
	mode := modeOf(*private)
	if err := exposeReset(execRunner{}, mode); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	fmt.Printf("%s reset.\n", mode.label())
	return 0
}

func cmdStatus(argv []string) int {
	mode, dcfg, httpsPort := queryConfig(argv)
	out, err := exposeStatus(execRunner{}, mode)
	if err != nil {
		fmt.Fprintf(os.Stderr, "status: %v\n", err)
	} else {
		fmt.Printf("%s status:\n%s\n", mode.label(), out)
	}
	return printDiscovered(dcfg, mode, httpsPort)
}

func cmdList(argv []string) int {
	mode, dcfg, httpsPort := queryConfig(argv)
	return printDiscovered(dcfg, mode, httpsPort)
}

func cmdDoctor(argv []string) int {
	mode, dcfg, _ := queryConfig(argv)
	if printChecks(runDoctor(execRunner{}, newDiscoverer(execRunner{}), dcfg, mode)) {
		fmt.Println("\nAll checks passed — you're ready to `tsp start`.")
		return 0
	}
	return 1
}

// queryConfig parses the shared discovery/mode flags (seeded from saved config)
// for list/status/doctor.
func queryConfig(argv []string) (Mode, discoverConfig, int) {
	cfg, _, _, _ := loadConfig()
	fs := flag.NewFlagSet("query", flag.ContinueOnError)
	portsRaw := fs.String("ports", cfg.Ports, "port range or single port")
	all := fs.Bool("all", cfg.All, "include all listeners")
	runtimesRaw := fs.String("runtimes", cfg.Runtimes, "comma-separated runtimes")
	private := fs.Bool("private", cfg.Private, "private (Serve) mode")
	httpsPort := fs.Int("https-port", cfg.HTTPSPort, "public/tailnet HTTPS port")
	_ = fs.Parse(argv)
	rng, err := parsePortRange(*portsRaw)
	if err != nil {
		rng = PortRange{Lo: 3000, Hi: 5000}
	}
	return modeOf(*private), discoverConfig{rng: rng, all: *all, runtimes: parseRuntimes(*runtimesRaw)}, *httpsPort
}

// printDiscovered lists discovered services with their public URLs.
func printDiscovered(dcfg discoverConfig, mode Mode, httpsPort int) int {
	svcs, dups, err := newDiscoverer(execRunner{}).Discover(dcfg)
	if err != nil {
		fmt.Fprintf(os.Stderr, "discovery failed: %v\n", err)
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

// printServiceURLs prints each service's public URL and local target.
func printServiceURLs(snap map[string]Service, node string, httpsPort int) {
	base := publicBase(node, httpsPort)
	for _, slug := range sortedSlugs(snap) {
		fmt.Printf("  %s/%s/  →  127.0.0.1:%d\n", base, slug, snap[slug].Port)
	}
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
