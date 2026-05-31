package main

import (
	"bufio"
	"context"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// splitFirstSegment splits "/seg/rest..." into ("seg", "/rest...").
// "/seg" and "/seg/" both yield ("seg", "/"); "/" and "" yield ("", "/").
func splitFirstSegment(p string) (seg, rest string) {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", "/"
	}
	if i := strings.IndexByte(p, '/'); i >= 0 {
		return p[:i], "/" + p[i+1:]
	}
	return p, "/"
}

// writeIndex writes a plain-text list of registered services with the given status.
func writeIndex(w http.ResponseWriter, store *RouteStore, status int) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	snap := store.snapshot()
	slugs := make([]string, 0, len(snap))
	for s := range snap {
		slugs = append(slugs, s)
	}
	sort.Strings(slugs)
	fmt.Fprintln(w, "tailscale-proxy — registered services:")
	if len(slugs) == 0 {
		fmt.Fprintln(w, "  (none discovered — start a dev server in range, or try --all / --ports, then `tsp doctor`)")
		return
	}
	for _, s := range slugs {
		svc := snap[s]
		rt := svc.Runtime
		if rt == "" {
			rt = "?"
		}
		fmt.Fprintf(w, "  /%s/  →  127.0.0.1:%d  (%s)\n", s, svc.Port, rt)
	}
}

type ctxKey int

const targetKey ctxKey = 0

// target is the resolved upstream for a single request.
type target struct {
	host string // "127.0.0.1:<port>"
	path string // rewritten path with the matched segment stripped
}

// newHandler returns an HTTP handler that routes by first path segment.
// When logRequests is true, each request is logged with method, status,
// target, and duration.
func newHandler(store *RouteStore, logRequests bool) http.Handler {
	// A dedicated, bounded transport so idle connections to dev servers that come
	// and go don't accumulate (capped pool + short idle timeout = steady memory).
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   8,
		IdleConnTimeout:       60 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: time.Second,
		ForceAttemptHTTP2:     true,
	}
	proxy := &httputil.ReverseProxy{
		Transport:     transport,
		FlushInterval: -1, // flush immediately: SSE / streaming / chunked
		Rewrite: func(pr *httputil.ProxyRequest) {
			tgt := pr.In.Context().Value(targetKey).(target)
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = tgt.host
			pr.Out.URL.Path = tgt.path
			pr.Out.URL.RawQuery = pr.In.URL.RawQuery
			pr.Out.Host = tgt.host
			pr.SetXForwarded()
			pr.Out.Header.Set("X-Forwarded-Proto", "https")
			if pr.In.Host != "" {
				pr.Out.Header.Set("X-Forwarded-Host", pr.In.Host)
			}
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusBadGateway)
			fmt.Fprintf(w, "tailscale-proxy: upstream error: %v\n", err)
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		port, path, ok := resolveRoute(store, r, w)
		if ok {
			tgt := target{host: "127.0.0.1:" + strconv.Itoa(port), path: path}
			ctx := context.WithValue(r.Context(), targetKey, tgt)
			proxy.ServeHTTP(rec, r.WithContext(ctx))
		} else {
			writeIndex(rec, store, http.StatusNotFound)
		}

		if logRequests {
			logRequest(r, rec.status, port, time.Since(start))
		}
	})
}

// routeCookie pins a browser to the last project it opened, so prefix-less
// asset/API requests (e.g. /_next/static/..., /api/...) — which lose the
// /<slug>/ prefix because the app assumes it lives at the root — still reach the
// right dev server. Without this, those requests 404 and the page renders broken.
const routeCookie = "tsp_route"

// resolveRoute determines the upstream port and rewritten path for a request.
// First path segment matching a slug wins (prefix stripped, affinity cookie set).
// Otherwise it falls back to the affinity cookie and forwards the full path.
// Returns (port, path, ok).
func resolveRoute(store *RouteStore, r *http.Request, w http.ResponseWriter) (int, string, bool) {
	seg, rest := splitFirstSegment(r.URL.Path)
	if seg != "" {
		if port, ok := store.lookup(seg); ok {
			// Remember this app for subsequent prefix-less requests.
			http.SetCookie(w, &http.Cookie{
				Name: routeCookie, Value: seg, Path: "/", SameSite: http.SameSiteLaxMode,
			})
			return port, rest, true
		}
	}
	// Prefix-less request (asset/API/HMR): follow the affinity cookie, forwarding
	// the full original path unchanged.
	if c, err := r.Cookie(routeCookie); err == nil && c.Value != "" {
		if port, ok := store.lookup(c.Value); ok {
			return port, r.URL.Path, true
		}
	}
	return 0, "", false
}

// statusRecorder captures the response status code while preserving streaming
// (Flush) and WebSocket (Hijack) support.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := s.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hj.Hijack()
}

func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// logRequest prints one nicely formatted request line.
func logRequest(r *http.Request, status, port int, dur time.Duration) {
	target := "—"
	if port > 0 {
		target = "127.0.0.1:" + strconv.Itoa(port)
	}
	log.Printf("%s %-6s %s → %s (%s)",
		colorStatus(status), r.Method, r.URL.Path, target, dur.Round(time.Millisecond))
}

// colorStatus renders an HTTP status code, colorized when writing to a terminal.
func colorStatus(code int) string {
	s := strconv.Itoa(code)
	if !useColor() {
		return s
	}
	var c string
	switch {
	case code >= 500:
		c = "\x1b[31m" // red
	case code >= 400:
		c = "\x1b[33m" // yellow
	case code >= 300:
		c = "\x1b[36m" // cyan
	default:
		c = "\x1b[32m" // green
	}
	return c + s + "\x1b[0m"
}

var (
	colorOnce    sync.Once
	colorEnabled bool
)

// useColor reports whether ANSI colors should be emitted (stderr is a TTY and
// NO_COLOR is unset). log output goes to stderr, so we probe stderr.
func useColor() bool {
	colorOnce.Do(func() {
		if os.Getenv("NO_COLOR") != "" {
			return
		}
		fi, err := os.Stderr.Stat()
		colorEnabled = err == nil && fi.Mode()&os.ModeCharDevice != 0
	})
	return colorEnabled
}
