// Command tailscale-proxy-app is a tray-first desktop wrapper around the tsp
// engine. It drives core.Controller in-process — no sidecar — so the menu bar
// can start/stop the proxy, switch Funnel/Serve, open service URLs, toggle
// start-at-login, and edit the shared config.
package main

import (
	"fmt"
	"log"
	"os/exec"
	"runtime"
	"sync"

	"github.com/meabed/tailscale-proxy/core"
	"github.com/wailsapp/wails/v3/pkg/application"
)

const docsURL = "https://tailscaleproxy.vercel.app"

type ui struct {
	app  *application.App
	tray *application.SystemTray
	ctl  *core.Controller

	mu  sync.Mutex
	cfg core.Config
}

func main() {
	cfg, _, _, err := core.LoadConfig()
	if err != nil {
		log.Printf("config: %v (using defaults)", err)
	}

	app := application.New(application.Options{
		Name:        "Tailscale Proxy",
		Description: "Discover local dev servers and expose them through one Tailscale entry.",
		// Tray-first: no Dock icon on macOS (menu-bar only).
		Mac: application.MacOptions{ActivationPolicy: application.ActivationPolicyAccessory},
	})

	u := &ui{app: app, ctl: core.NewController(), cfg: cfg}
	u.tray = app.SystemTray.New()
	u.tray.SetLabel("tsp")

	// Controller events arrive on a background goroutine; marshal UI work onto
	// the main thread.
	u.ctl.OnChange(func() { application.InvokeAsync(u.rebuild) })
	u.rebuild()

	// Auto-start the proxy on launch (best effort; status reflects failures).
	go func() {
		if err := u.ctl.Start(u.opts()); err != nil {
			log.Printf("auto-start: %v", err)
		}
		application.InvokeAsync(u.rebuild)
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

// rebuild reconstructs the tray menu from the current status. Must run on the
// main thread (call via application.InvokeAsync from background goroutines).
func (u *ui) rebuild() {
	st := u.ctl.Status()
	u.mu.Lock()
	private := u.cfg.Private
	u.mu.Unlock()

	m := u.app.NewMenu()

	header := "○  Stopped"
	if st.Running {
		header = "●  Running — " + st.Mode
	}
	m.Add(header).SetEnabled(false)
	if st.Running && st.PublicBase != "" {
		m.Add("   " + st.PublicBase).SetEnabled(false)
	}

	m.AddSeparator()
	toggle := "Start"
	if st.Running {
		toggle = "Stop"
	}
	m.Add(toggle).OnClick(func(*application.Context) { go u.toggle() })

	m.AddSeparator()
	m.AddRadio("Public  (Funnel)", !private).OnClick(func(*application.Context) { go u.setPrivate(false) })
	m.AddRadio("Private (Serve)", private).OnClick(func(*application.Context) { go u.setPrivate(true) })

	if len(st.Services) > 0 {
		m.AddSeparator()
		sub := m.AddSubmenu(fmt.Sprintf("Services (%d)", len(st.Services)))
		for _, s := range st.Services {
			label := fmt.Sprintf("%s  →  :%d", s.Slug, s.Port)
			url := s.URL
			item := sub.Add(label)
			if url != "" {
				item.OnClick(func(*application.Context) { openExternal(url) })
			} else {
				item.SetEnabled(false)
			}
		}
	}

	m.AddSeparator()
	m.AddCheckbox("Start at login", autostartEnabled()).
		OnClick(func(ctx *application.Context) { go u.setAutostart(ctx.IsChecked()) })
	m.Add("Open config file…").OnClick(func(*application.Context) {
		if p, err := core.ConfigPath(); err == nil {
			openExternal(p)
		}
	})
	m.Add("Open docs").OnClick(func(*application.Context) { openExternal(docsURL) })

	m.AddSeparator()
	m.Add("Quit").OnClick(func(*application.Context) {
		_ = u.ctl.Stop()
		u.app.Quit()
	})

	u.tray.SetMenu(m)
	if st.Running {
		u.tray.SetLabel("tsp ●")
	} else {
		u.tray.SetLabel("tsp")
	}
}

func (u *ui) toggle() {
	if err := u.ctl.Toggle(u.opts()); err != nil {
		log.Printf("toggle: %v", err)
	}
	application.InvokeAsync(u.rebuild)
}

func (u *ui) setPrivate(private bool) {
	u.mu.Lock()
	u.cfg.Private = private
	cfg := u.cfg
	u.mu.Unlock()
	if _, err := core.SaveConfig(cfg); err != nil {
		log.Printf("save config: %v", err)
	}
	// Re-expose under the new mode if we're running.
	if u.ctl.Running() {
		_ = u.ctl.Stop()
		if err := u.ctl.Start(u.opts()); err != nil {
			log.Printf("restart after mode change: %v", err)
		}
	}
	application.InvokeAsync(u.rebuild)
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
	application.InvokeAsync(u.rebuild)
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
