package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
)

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
	fs.BoolVar(&cfg.ForwardHost, "forward-host", cfg.ForwardHost, "forward the public host to apps")
	if err := fs.Parse(argv); err != nil {
		if err == flag.ErrHelp {
			return 0
		}
		return 2
	}
	if _, err := parsePortRange(cfg.Ports); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 2
	}
	path, err := saveConfig(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "could not save config:", err)
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
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Printf("%s reset.\n", mode.label())
	return 0
}

func cmdStatus(argv []string) int {
	mode, dcfg, httpsPort := queryConfig(argv)
	if out, err := exposeStatus(execRunner{}, mode); err != nil {
		fmt.Fprintln(os.Stderr, "status:", err)
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
// for list/status/doctor, returning the mode, discovery config, and HTTPS port.
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
