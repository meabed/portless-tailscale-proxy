package core

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
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
  --runtimes <list>      Comma-separated runtimes to keep (default: all known)
  --private              Expose privately via Tailscale Serve (default: Funnel)
  --bind <addr>          Proxy listen address (default 127.0.0.1; use 0.0.0.0 to
                         reach the proxy from containers / the LAN — no MagicDNS)
  --port <n>             Local proxy HTTP port                (default 8443)
  --interval <sec>       Re-scan period in seconds            (default 20)
  --https-port <n>       Public/tailnet HTTPS port            (default 443)
  --deregister-cycles <n> Missing scans before a gone service is removed (default 5)
  --bg                   Run tsp detached in the background (logs → ./tsp.log)
  --proxy-only           Run the proxy only; print the tailscale command
  --forward-host         Forward the public host to apps (X-Forwarded-Host/Proto);
                         default presents a local request (apps behave like localhost)
  --accept-dns <bool>    Optionally set Tailscale MagicDNS (true|false) on start;
                         default unset = leave it alone. accept-dns=false lets a
                         tailnet host resolve the public funnel name (persists)
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
	bind             string
	port             int
	interval         int
	httpsPort        int
	deregisterCycles int
	bg               bool
	proxyOnly        bool
	logRequests      bool
	forwardHost      bool
	quiet            bool
	acceptDNS        string
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
	fs.StringVar(&o.bind, "bind", cfg.Bind, "proxy listen address (0.0.0.0 to reach it from containers/LAN)")
	fs.IntVar(&o.port, "port", cfg.Port, "local proxy HTTP port")
	fs.IntVar(&o.interval, "interval", cfg.Interval, "re-scan period (seconds)")
	fs.IntVar(&o.httpsPort, "https-port", cfg.HTTPSPort, "public/tailnet HTTPS port")
	fs.IntVar(&o.deregisterCycles, "deregister-cycles", cfg.DeregisterCycles, "missing scans before removal")
	fs.BoolVar(&o.logRequests, "log-requests", cfg.LogRequests, "log each proxied request")
	fs.BoolVar(&o.forwardHost, "forward-host", cfg.ForwardHost, "forward the public host to apps (X-Forwarded-Host/Proto); default presents a local request")
	fs.StringVar(&o.acceptDNS, "accept-dns", cfg.AcceptDNS, "optionally set Tailscale MagicDNS (true|false) on start; default unset = leave it alone")
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
	if o.bind == "" {
		o.bind = "127.0.0.1"
	}
	if o.acceptDNS != "" && o.acceptDNS != "true" && o.acceptDNS != "false" {
		fmt.Fprintf(os.Stderr, "invalid --accept-dns %q: use true or false\n", o.acceptDNS)
		return 2
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

	// Opt-in: set MagicDNS (accept-dns) before exposing. Off by default — this is a
	// global, persistent Tailscale setting, so tsp only touches it when asked, and
	// does not revert it on exit (the user chose it deliberately).
	if o.acceptDNS != "" {
		if err := setAcceptDNS(runner, o.acceptDNS); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			return 1
		}
		revert := "true"
		if o.acceptDNS == "true" {
			revert = "false"
		}
		fmt.Printf("set tailscale accept-dns=%s (persists after exit; revert with: tailscale set --accept-dns=%s)\n", o.acceptDNS, revert)
	}

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

	if o.bind != "127.0.0.1" && o.bind != "localhost" {
		fmt.Printf("⚠ proxy bound to %s:%d — reachable beyond this host (containers/LAN). Anyone who can reach it can hit your dev servers.\n", o.bind, o.port)
	}
	srv := &http.Server{Addr: fmt.Sprintf("%s:%d", o.bind, o.port), Handler: newHandler(store, o.logRequests, o.forwardHost)}
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
