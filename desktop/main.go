// Command tailscale-proxy-app is a tray-first desktop wrapper around the tsp
// engine. It drives core.Controller in-process — no sidecar — and presents a
// webview panel (served on loopback) from the menu bar, plus a settings window.
package main

import (
	"log"
	"os/exec"
	"runtime"
	"sync"
	"time"

	"github.com/meabed/tailscale-proxy/core"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const appName = "Tailscale Proxy"

var (
	bgColour    = application.RGBA{Red: 18, Green: 20, Blue: 24, Alpha: 255}
	transparent = application.RGBA{Red: 0, Green: 0, Blue: 0, Alpha: 0}
)

type ui struct {
	app      *application.App
	tray     *application.SystemTray
	panel    *application.WebviewWindow
	settings *application.WebviewWindow
	ctl      *core.Controller

	mu          sync.Mutex
	cfg         core.Config
	token       string
	settingsURL string

	dmu      sync.Mutex // guards the status caches below
	seen     map[string]time.Time
	health   core.TailscaleHealth
	healthAt time.Time
	stats    map[int]procStat
	statsAt  time.Time
}

func main() {
	cfg, _, _, err := core.LoadConfig()
	if err != nil {
		log.Printf("config: %v (using defaults)", err)
	}

	policy := application.ActivationPolicyAccessory // tray-first: no Dock icon
	if !loadPrefs().HideDock {
		policy = application.ActivationPolicyRegular
	}
	app := application.New(application.Options{
		Name:        appName,
		Description: "Discover local dev servers and expose them through one Tailscale entry.",
		Icon:        iconRunning,
		Mac:         application.MacOptions{ActivationPolicy: policy},
	})

	u := &ui{app: app, ctl: core.NewController(), cfg: cfg}
	u.tray = app.SystemTray.New()
	u.tray.SetTemplateIcon(iconIdle)

	base, err := u.startDashboard()
	if err != nil {
		log.Fatalf("dashboard: %v", err)
	}
	u.panel = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name: "panel", URL: base, Width: 384, Height: 600,
		Frameless: true, DisableResize: true, AlwaysOnTop: true, Hidden: true,
		BackgroundColour: transparent,
		Mac:              application.MacWindow{Backdrop: application.MacBackdropTranslucent},
	})
	u.tray.AttachWindow(u.panel).WindowDebounce(200 * time.Millisecond)
	// Dismiss the panel when the user clicks away from it.
	u.panel.OnWindowEvent(events.Common.WindowLostFocus, func(*application.WindowEvent) { u.hidePanel() })

	// The settings window is created on demand (see showSettings) so it always
	// reopens cleanly after the user closes it.
	u.settingsURL = base + "/settings"

	u.ctl.OnChange(func() { application.InvokeAsync(u.updateIcon) })
	u.updateIcon()

	// Auto-start the proxy on launch (best effort; the panel reflects failures).
	go func() {
		if err := u.ctl.Start(u.opts()); err != nil {
			log.Printf("auto-start: %v", err)
		}
		application.InvokeAsync(u.updateIcon)
	}()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func (u *ui) opts() core.Options {
	u.mu.Lock()
	defer u.mu.Unlock()
	return core.OptionsFromConfig(u.cfg)
}

func (u *ui) updateIcon() {
	if u.ctl.Running() {
		u.tray.SetIcon(iconRunning)
	} else {
		u.tray.SetTemplateIcon(iconIdle)
	}
}

// showSettings opens the settings window, (re)creating it if it was closed.
// Closing the window destroys it in Wails, so we clear the reference on close and
// build a fresh one next time — this is what makes "open → close → open" reliable.
func (u *ui) showSettings() {
	application.InvokeAsync(func() {
		if u.settings == nil {
			u.settings = u.app.Window.NewWithOptions(application.WebviewWindowOptions{
				Name: "settings", Title: appName + " Settings", URL: u.settingsURL,
				Width: 560, Height: 660, Hidden: true, BackgroundColour: bgColour,
			})
			u.settings.OnWindowEvent(events.Common.WindowClosing, func(*application.WindowEvent) {
				u.settings = nil
			})
		}
		u.settings.Show()
		u.settings.Focus()
	})
}

func (u *ui) hidePanel() { application.InvokeAsync(func() { u.panel.Hide() }) }

func (u *ui) toggle() {
	if err := u.ctl.Toggle(u.opts()); err != nil {
		log.Printf("toggle: %v", err)
	}
	application.InvokeAsync(u.updateIcon)
}

func (u *ui) setPrivate(private bool) {
	u.mu.Lock()
	u.cfg.Private = private
	cfg := u.cfg
	u.mu.Unlock()
	if _, err := core.SaveConfig(cfg); err != nil {
		log.Printf("save config: %v", err)
	}
	u.restartIfRunning()
}

// applyConfig persists a new config from the settings window and re-exposes if running.
func (u *ui) applyConfig(cfg core.Config) {
	u.mu.Lock()
	u.cfg = cfg
	u.mu.Unlock()
	if _, err := core.SaveConfig(cfg); err != nil {
		log.Printf("save config: %v", err)
	}
	u.restartIfRunning()
}

func (u *ui) restartIfRunning() {
	if u.ctl.Running() {
		_ = u.ctl.Stop()
		if err := u.ctl.Start(u.opts()); err != nil {
			log.Printf("restart: %v", err)
		}
	}
	application.InvokeAsync(u.updateIcon)
}

func (u *ui) setAutostart(on bool) {
	var err error
	if on {
		err = enableAutostart()
	} else {
		err = disableAutostart()
	}
	if err != nil {
		log.Printf("autostart: %v", err)
	}
}

func (u *ui) quit() {
	_ = u.ctl.Stop()
	application.InvokeAsync(u.app.Quit)
}

// openExternal opens a URL or file path with the OS default handler.
func openExternal(target string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", target)
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", target)
	default:
		cmd = exec.Command("xdg-open", target)
	}
	if err := cmd.Start(); err != nil {
		log.Printf("open %q: %v", target, err)
	}
}
