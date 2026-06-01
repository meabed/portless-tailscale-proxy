package main

import (
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/meabed/tailscale-proxy/core"
)

//go:embed assets/panel.html
var panelHTML string

//go:embed assets/settings.html
var settingsHTML string

type svcJSON struct {
	Slug    string `json:"slug"`
	Runtime string `json:"runtime"`
	Port    int    `json:"port"`
	PID     int    `json:"pid"`
	Dir     string `json:"dir"`
	URL     string `json:"url"`
}

type statusJSON struct {
	Running    bool      `json:"running"`
	Mode       string    `json:"mode"`
	Private    bool      `json:"private"`
	Node       string    `json:"node"`
	PublicBase string    `json:"publicBase"`
	HTTPSPort  int       `json:"httpsPort"`
	Err        string    `json:"err"`
	Services   []svcJSON `json:"services"`
}

type configJSON struct {
	Config    core.Config `json:"config"`
	Runtimes  []string    `json:"runtimes"`
	HideDock  bool        `json:"hideDock"`
	Autostart bool        `json:"autostart"`
	Name      string      `json:"name"`
	Version   string      `json:"version"`
}

func (u *ui) status() statusJSON {
	st := u.ctl.Status()
	u.mu.Lock()
	private := u.cfg.Private
	u.mu.Unlock()
	out := statusJSON{
		Running: st.Running, Mode: st.Mode, Private: private, Node: st.Node,
		PublicBase: st.PublicBase, HTTPSPort: st.HTTPSPort, Err: st.Err, Services: []svcJSON{},
	}
	for _, s := range st.Services {
		out.Services = append(out.Services, svcJSON{
			Slug: s.Slug, Runtime: s.Runtime, Port: s.Port, PID: s.PID, Dir: s.Dir, URL: s.URL,
		})
	}
	return out
}

func (u *ui) configState() configJSON {
	u.mu.Lock()
	cfg := u.cfg
	u.mu.Unlock()
	return configJSON{
		Config: cfg, Runtimes: core.KnownRuntimes(), HideDock: loadPrefs().HideDock,
		Autostart: autostartEnabled(), Name: appName, Version: core.Version,
	}
}

// startDashboard serves the panel + settings pages and a token-gated control API
// on a random loopback port. The token (injected into the HTML) stops other
// local processes/browsers from driving the app.
func (u *ui) startDashboard() (string, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	u.token = hex.EncodeToString(buf)
	page := func(html string) []byte { return []byte(strings.ReplaceAll(html, "__TOKEN__", u.token)) }
	panel, settings := page(panelHTML), page(settingsHTML)

	auth := func(h http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-TSP-Token") != u.token {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			h(w, r)
		}
	}
	html := func(body []byte) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write(body)
		}
	}
	writeJSON := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		html(panel)(w, r)
	})
	mux.HandleFunc("/settings", html(settings))

	mux.HandleFunc("/api/status", auth(func(w http.ResponseWriter, r *http.Request) { writeJSON(w, u.status()) }))
	mux.HandleFunc("/api/config", auth(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			var cfg core.Config
			if json.NewDecoder(r.Body).Decode(&cfg) == nil {
				go u.applyConfig(cfg)
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeJSON(w, u.configState())
	}))
	mux.HandleFunc("/api/toggle", auth(func(w http.ResponseWriter, r *http.Request) {
		go u.toggle()
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/api/mode", auth(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			Private bool `json:"private"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		go u.setPrivate(b.Private)
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/api/refresh", auth(func(w http.ResponseWriter, r *http.Request) {
		u.ctl.Refresh()
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/api/autostart", auth(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			On bool `json:"on"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		go u.setAutostart(b.On)
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/api/prefs", auth(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			HideDock bool `json:"hideDock"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		_ = savePrefs(prefs{HideDock: b.HideDock})
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/api/open", auth(func(w http.ResponseWriter, r *http.Request) {
		var b struct {
			URL string `json:"url"`
		}
		_ = json.NewDecoder(r.Body).Decode(&b)
		if p, err := url.Parse(b.URL); err == nil && (p.Scheme == "http" || p.Scheme == "https") {
			openExternal(b.URL)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/api/openconfig", auth(func(w http.ResponseWriter, r *http.Request) {
		if p, err := core.ConfigPath(); err == nil {
			openExternal(p)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/api/settings", auth(func(w http.ResponseWriter, r *http.Request) {
		u.showSettings()
		w.WriteHeader(http.StatusNoContent)
	}))
	mux.HandleFunc("/api/quit", auth(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
		go u.quit()
	}))

	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()
	return "http://" + ln.Addr().String(), nil
}
