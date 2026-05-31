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
	hosts := make([]string, 0, len(snap))
	for h := range snap {
		hosts = append(hosts, h)
	}
	sort.Strings(hosts)
	fmt.Fprintln(w, "portless-tailscale-proxy — registered services:")
	if len(hosts) == 0 {
		fmt.Fprintln(w, "  (none — is `portless` running? try `ptp doctor`)")
		return
	}
	for _, h := range hosts {
		fmt.Fprintf(w, "  /%s/  ->  127.0.0.1:%d\n", h, snap[h])
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
	proxy := &httputil.ReverseProxy{
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
			fmt.Fprintf(w, "portless-tailscale-proxy: upstream error: %v\n", err)
		},
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

		seg, rest := splitFirstSegment(r.URL.Path)
		port, ok := 0, false
		if seg != "" {
			port, ok = store.lookup(seg)
		}
		if ok {
			tgt := target{host: "127.0.0.1:" + strconv.Itoa(port), path: rest}
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
