package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Options configures a Controller run — the subset of `start` settings the
// desktop app exposes. Zero values fall back to the built-in defaults.
type Options struct {
	Ports            string
	All              bool
	Runtimes         string
	Private          bool // Serve (tailnet-only) instead of Funnel
	Bind             string
	Port             int
	Interval         int
	HTTPSPort        int
	DeregisterCycles int
	ForwardHost      bool
	LogRequests      bool
	ProxyOnly        bool // run the proxy only; skip the Serve/Funnel entry
}

// OptionsFromConfig builds run Options from a saved Config.
func OptionsFromConfig(c Config) Options {
	return Options{
		Ports: c.Ports, All: c.All, Runtimes: c.Runtimes, Private: c.Private,
		Bind: c.Bind, Port: c.Port, Interval: c.Interval, HTTPSPort: c.HTTPSPort,
		DeregisterCycles: c.DeregisterCycles, ForwardHost: c.ForwardHost,
		LogRequests: c.LogRequests,
	}
}

// ServiceURL pairs a discovered service with its public URL.
type ServiceURL struct {
	Service
	URL string
}

// Status is an immutable snapshot of the controller for the UI.
type Status struct {
	Running    bool
	Mode       string // human label, e.g. "Tailscale Funnel (public)"
	Private    bool
	Node       string // MagicDNS name, "" if unknown
	PublicBase string // https://node.ts.net[:port], "" if unknown
	Bind       string
	Port       int
	HTTPSPort  int
	Services   []ServiceURL
	Duplicates []Duplicate
	Err        string
}

// Controller runs the proxy, discovery loop, and Tailscale exposure in-process,
// exposing a small start/stop/status surface for embedding (the desktop app).
// Safe for concurrent use.
type Controller struct {
	mu       sync.Mutex
	runner   Runner
	onChange func()

	running bool
	opts    Options
	mode    Mode
	node    string
	lastErr string

	srv    *http.Server
	store  *RouteStore
	cancel context.CancelFunc
}

// NewController returns an idle controller backed by the real `tailscale`/`lsof`.
func NewController() *Controller { return newControllerWithRunner(execRunner{}) }

func newControllerWithRunner(r Runner) *Controller {
	return &Controller{runner: r, onChange: func() {}}
}

// OnChange registers a callback fired whenever the status changes (start, stop,
// or a discovery refresh). It runs on a background goroutine.
func (c *Controller) OnChange(f func()) {
	if f == nil {
		f = func() {}
	}
	c.mu.Lock()
	c.onChange = f
	c.mu.Unlock()
}

// Running reports whether the proxy is currently serving.
func (c *Controller) Running() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// Start brings up the proxy listener, the discovery loop, and (unless
// ProxyOnly) the Tailscale Serve/Funnel entry. Returns an error if already
// running, the port range is invalid, or the listen/expose fails.
func (c *Controller) Start(o Options) error {
	o = withDefaults(o)
	rng, err := parsePortRange(o.Ports)
	if err != nil {
		return err
	}

	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return fmt.Errorf("already running")
	}
	runner := c.runner
	c.mu.Unlock()

	mode := modeOf(o.Private)
	disc := newDiscoverer(runner)
	dcfg := discoverConfig{rng: rng, all: o.All, runtimes: parseRuntimes(o.Runtimes)}
	store := NewRouteStore(func() ([]Service, []Duplicate, error) { return disc.Discover(dcfg) }, o.DeregisterCycles)
	_, _, _, _ = store.refresh()

	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", o.Bind, o.Port),
		Handler: newHandler(store, o.LogRequests, o.ForwardHost),
	}
	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		return fmt.Errorf("cannot listen on %s: %v", srv.Addr, err)
	}
	if !o.ProxyOnly {
		if err := exposeStart(runner, mode, o.Port, o.HTTPSPort); err != nil {
			_ = ln.Close()
			return err
		}
	}
	node, _ := nodeDNSName(runner)

	ctx, cancel := context.WithCancel(context.Background())
	c.mu.Lock()
	c.running = true
	c.opts = o
	c.mode = mode
	c.node = node
	c.lastErr = ""
	c.srv = srv
	c.store = store
	c.cancel = cancel
	c.mu.Unlock()

	go func() { _ = srv.Serve(ln) }()
	go c.pollLoop(ctx, store, time.Duration(o.Interval)*time.Second)
	c.notify()
	return nil
}

// Stop shuts the proxy down and resets the Serve/Funnel entry. No-op if idle.
func (c *Controller) Stop() error {
	c.mu.Lock()
	if !c.running {
		c.mu.Unlock()
		return nil
	}
	srv, cancel, mode, runner, proxyOnly := c.srv, c.cancel, c.mode, c.runner, c.opts.ProxyOnly
	c.running = false
	c.srv, c.store, c.cancel = nil, nil, nil
	c.mu.Unlock()

	cancel()
	ctx, cancelT := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelT()
	_ = srv.Shutdown(ctx)
	var err error
	if !proxyOnly {
		err = exposeReset(runner, mode)
	}
	c.notify()
	return err
}

// Refresh forces an immediate discovery refresh and fires OnChange. No-op if idle.
func (c *Controller) Refresh() {
	c.mu.Lock()
	store := c.store
	c.mu.Unlock()
	if store != nil {
		_, _, _, _ = store.refresh()
		c.notify()
	}
}

// Toggle starts if idle, stops if running.
func (c *Controller) Toggle(o Options) error {
	if c.Running() {
		return c.Stop()
	}
	return c.Start(o)
}

// Status returns the current snapshot for the UI.
func (c *Controller) Status() Status {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := Status{
		Running: c.running, Private: c.opts.Private, Node: c.node, Err: c.lastErr,
		Bind: c.opts.Bind, Port: c.opts.Port, HTTPSPort: c.opts.HTTPSPort,
	}
	if !c.running {
		return st
	}
	st.Mode = c.mode.label()
	if c.node != "" {
		st.PublicBase = publicBase(c.node, c.opts.HTTPSPort)
	}
	if c.store != nil {
		snap := c.store.snapshot()
		for _, slug := range sortedSlugs(snap) {
			url := ""
			if st.PublicBase != "" {
				url = st.PublicBase + "/" + slug + "/"
			}
			st.Services = append(st.Services, ServiceURL{Service: snap[slug], URL: url})
		}
		st.Duplicates = c.store.dupes()
	}
	return st
}

func (c *Controller) pollLoop(ctx context.Context, store *RouteStore, every time.Duration) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			_, _, _, _ = store.refresh()
			c.notify()
		}
	}
}

func (c *Controller) notify() {
	c.mu.Lock()
	f := c.onChange
	c.mu.Unlock()
	go f()
}

func withDefaults(o Options) Options {
	if o.Ports == "" {
		o.Ports = "3000-5000"
	}
	if o.Bind == "" {
		o.Bind = "127.0.0.1"
	}
	if o.Port == 0 {
		o.Port = 8443
	}
	if o.Interval < 1 {
		o.Interval = 20
	}
	if o.HTTPSPort == 0 {
		o.HTTPSPort = 443
	}
	if o.DeregisterCycles < 1 {
		o.DeregisterCycles = 5
	}
	return o
}

// --- Exported helpers for embedders (the desktop app) -----------------------

// LoadConfig loads the saved config (or built-in defaults if none).
// Returns (config, path, existed, error).
func LoadConfig() (Config, string, bool, error) { return loadConfig() }

// SaveConfig persists cfg to the default path and returns the path written.
func SaveConfig(c Config) (string, error) { return saveConfig(c) }

// ConfigPath returns the config file path (~/.tailscale-proxy/config.json).
func ConfigPath() (string, error) { return configPath() }

// DefaultConfig returns the built-in defaults.
func DefaultConfig() Config { return defaultConfig() }

// KnownRuntimes returns the labels of the web runtimes discovery recognizes.
func KnownRuntimes() []string { return knownRuntimeLabels() }

// UpdateInfo reports the running version vs the latest GitHub release.
type UpdateInfo struct {
	Current   string `json:"current"`
	Latest    string `json:"latest"`
	HasUpdate bool   `json:"hasUpdate"`
	Err       string `json:"err"`
}

// CheckUpdate queries the latest GitHub release and compares it to Version.
func CheckUpdate() UpdateInfo {
	u := UpdateInfo{Current: Version}
	latest, err := latestVersion()
	if err != nil {
		u.Err = err.Error()
		return u
	}
	u.Latest = latest
	u.HasUpdate = Version != "dev" && normalizeVer(latest) != normalizeVer(Version)
	return u
}

// TailscaleHealth reports whether the `tailscale` CLI is present and logged in.
type TailscaleHealth struct {
	Installed bool   `json:"installed"`
	Up        bool   `json:"up"`
	Detail    string `json:"detail"`
}

// CheckTailscale probes for the tailscale CLI and login state (best effort).
func CheckTailscale() TailscaleHealth {
	r := execRunner{}
	if _, _, err := r.Run("tailscale", "version"); err != nil {
		return TailscaleHealth{Detail: "the tailscale CLI was not found on your PATH"}
	}
	out, _, err := r.Run("tailscale", "status")
	if err != nil || strings.Contains(out, "Logged out") {
		return TailscaleHealth{Installed: true, Detail: "Tailscale is installed but not logged in — run `tailscale up`"}
	}
	return TailscaleHealth{Installed: true, Up: true}
}

// Doctor runs the preflight checks for the given options.
func Doctor(o Options) []Check {
	o = withDefaults(o)
	rng, err := parsePortRange(o.Ports)
	if err != nil {
		rng = PortRange{Lo: 3000, Hi: 5000}
	}
	r := execRunner{}
	dcfg := discoverConfig{rng: rng, all: o.All, runtimes: parseRuntimes(o.Runtimes)}
	return runDoctor(r, newDiscoverer(r), dcfg, modeOf(o.Private))
}
