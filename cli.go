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
	"strings"
	"syscall"
	"time"
)

func printHelp() {
	fmt.Print(`portless-tailscale-proxy (ptp)
Route a single Tailscale Funnel to all your portless dev servers, by URL path.

The first path segment of the public URL is the portless hostname; ptp strips it
and forwards the rest to the matching local dev server:

  https://<node>.ts.net/module-help-ai-agent-api.local/foo
                        └──────────────┬───────────────┘
                        ptp → 127.0.0.1:4434/foo

Usage:
  ptp <command> [flags]

Commands:
  start     Preflight, run the proxy, and start the Tailscale Funnel
  status    Print Funnel status and the current route map
  list      Print the live hostname→port map and public URLs
  reset     Stop the Funnel (tailscale funnel reset) and exit
  doctor    Check tailscale / Funnel / portless and print fix links

Examples:
  ptp doctor                 # verify your environment is ready
  ptp start                  # expose all portless servers via the Funnel
  ptp start --no-funnel      # just run the local proxy (print the funnel command)
  ptp start --bg             # run detached; logs to ./ptp.log
  ptp list                   # see what's currently routable
  ptp reset                  # take the Funnel down

Run "ptp <command> --help" for command-specific flags.
Global flags: -h/--help, -v/--version
Docs: https://github.com/meabed/portless-tailscale-proxy
`)
}

// startUsage prints help for the `start` command.
func startUsage(fs *flag.FlagSet) {
	fmt.Print(`ptp start — run the path-routing proxy and expose it via Tailscale Funnel

Usage:
  ptp start [flags]

Flags:
  --port <n>          Local proxy HTTP port              (default 8443)
  --interval <sec>    How often to re-read portless state (default 20)
  --state <path>      portless routes.json path          (default ~/.portless/routes.json)
  --funnel-port <n>   Public Funnel port: 443, 8443, or 10000 (default 443)
  --bg                Run ptp detached in the background (logs → ./ptp.log)
  --fg                Run in the foreground (default)
  --no-funnel         Run the proxy only; print the tailscale command to run yourself
  -h, --help          Show this help

Examples:
  ptp start
  ptp start --port 9000 --interval 10
  ptp start --funnel-port 8443
  ptp start --no-funnel --port 8799

Press Ctrl-C to stop — the Funnel is reset automatically on exit.
`)
}

// resolveStatePath returns the flag value or the default ~/.portless/routes.json.
func resolveStatePath(flagVal string) (string, error) {
	if flagVal != "" {
		return flagVal, nil
	}
	return defaultStatePath()
}

type startOpts struct {
	port       int
	interval   int
	state      string
	funnelPort int
	bg         bool
	noFunnel   bool
}

func cmdStart(argv []string) int {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.Usage = func() { startUsage(fs) }
	var o startOpts
	fs.IntVar(&o.port, "port", 8443, "local proxy HTTP port")
	fs.IntVar(&o.interval, "interval", 20, "route refresh period (seconds)")
	fs.StringVar(&o.state, "state", "", "routes.json path")
	fs.IntVar(&o.funnelPort, "funnel-port", 443, "public funnel port")
	fs.BoolVar(&o.bg, "bg", false, "run detached in background")
	var fg bool
	fs.BoolVar(&fg, "fg", false, "run in foreground (default)")
	fs.BoolVar(&o.noFunnel, "no-funnel", false, "proxy only; print funnel command")
	if err := fs.Parse(argv); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}

	if o.funnelPort != 443 && o.funnelPort != 8443 && o.funnelPort != 10000 {
		fmt.Fprintf(os.Stderr, "invalid --funnel-port %d: Tailscale Funnel allows only 443, 8443, or 10000\n", o.funnelPort)
		return 2
	}

	if o.bg {
		logPath := "ptp.log"
		pid, err := spawnDetached(logPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "failed to detach: %v\n", err)
			return 1
		}
		fmt.Printf("portless-tailscale-proxy running in background (pid %d), logs → %s\n", pid, logPath)
		return 0
	}

	statePath, err := resolveStatePath(o.state)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot resolve state path: %v\n", err)
		return 1
	}

	runner := execRunner{}

	// Preflight (non-fatal in --no-funnel mode).
	checks := runDoctor(runner, statePath)
	allOK := printChecks(checks)
	if !allOK && !o.noFunnel {
		fmt.Fprintln(os.Stderr, "\npreflight failed — fix the items above, or use --no-funnel to run the proxy alone")
		return 1
	}

	store := NewRouteStore(statePath)
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go poll(ctx, store, time.Duration(o.interval)*time.Second)

	srv := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", o.port),
		Handler: newHandler(store),
	}

	// Start listening first so the funnel never points at a dead port.
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cannot listen on %s: %v\n", srv.Addr, err)
		return 1
	}
	log.Printf("listening on http://%s", srv.Addr)

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()

	if o.noFunnel {
		fmt.Printf("proxy only — run this to expose it publicly:\n  tailscale %s\n",
			strings.Join(funnelArgs(o.port, o.funnelPort), " "))
	} else {
		if err := funnelStart(runner, o.port, o.funnelPort); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			_ = srv.Close()
			return 1
		}
		fmt.Printf("Tailscale Funnel → 127.0.0.1:%d (public port %d)\n", o.port, o.funnelPort)
	}

	// Block until a signal arrives or the server stops on its own.
	select {
	case err := <-serveErr:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			if !o.noFunnel {
				_ = funnelReset(runner)
			}
			return 1
		}
	case <-ctx.Done():
		fmt.Println("\nshutting down…")
	}

	// Graceful shutdown — run synchronously so the funnel is reset before we exit.
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
	if !o.noFunnel {
		if err := funnelReset(runner); err != nil {
			log.Printf("warn: %v", err)
		} else {
			fmt.Println("Tailscale Funnel reset.")
		}
	}
	return 0
}

func cmdReset(argv []string) int {
	if err := funnelReset(execRunner{}); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	fmt.Println("Tailscale Funnel reset.")
	return 0
}

func cmdStatus(argv []string) int {
	out, err := funnelStatus(execRunner{})
	if err != nil {
		fmt.Fprintf(os.Stderr, "funnel status: %v\n", err)
	} else {
		fmt.Println("Funnel status:")
		fmt.Println(out)
	}
	return cmdList(argv)
}

func cmdList(argv []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	state := fs.String("state", "", "routes.json path")
	_ = fs.Parse(argv)
	statePath, err := resolveStatePath(*state)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	m, err := loadRoutes(statePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if len(m) == 0 {
		fmt.Println("No portless routes found. Is `portless` running? Try `ptp doctor`.")
		return 0
	}
	fmt.Println("Registered services:")
	for h, p := range m {
		fmt.Printf("  /%s/  ->  127.0.0.1:%d\n", h, p)
	}
	return 0
}

func cmdDoctor(argv []string) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	state := fs.String("state", "", "routes.json path")
	_ = fs.Parse(argv)
	statePath, err := resolveStatePath(*state)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		return 1
	}
	if printChecks(runDoctor(execRunner{}, statePath)) {
		fmt.Println("\nAll checks passed — you're ready to `ptp start`.")
		return 0
	}
	return 1
}
