package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httputil"
	"sort"
	"strconv"
	"strings"
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
func newHandler(store *RouteStore) http.Handler {
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
		seg, rest := splitFirstSegment(r.URL.Path)
		if seg == "" {
			writeIndex(w, store, http.StatusNotFound)
			return
		}
		port, ok := store.lookup(seg)
		if !ok {
			writeIndex(w, store, http.StatusNotFound)
			return
		}
		tgt := target{host: "127.0.0.1:" + strconv.Itoa(port), path: rest}
		ctx := context.WithValue(r.Context(), targetKey, tgt)
		proxy.ServeHTTP(w, r.WithContext(ctx))
	})
}
