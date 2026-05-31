package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
)

// storeWith builds a RouteStore whose discovery returns fixed services.
func storeWith(svcs ...Service) *RouteStore {
	s := NewRouteStore(func() ([]Service, error) { return svcs, nil }, 5)
	s.refresh()
	return s
}

func TestSplitFirstSegment(t *testing.T) {
	cases := []struct {
		in, seg, rest string
	}{
		{"/module-help-ai-agent-api.local/foo", "module-help-ai-agent-api.local", "/foo"},
		{"/module-help-ai-agent-api.local/foo/bar", "module-help-ai-agent-api.local", "/foo/bar"},
		{"/module-help-ai-agent-api.local", "module-help-ai-agent-api.local", "/"},
		{"/module-help-ai-agent-api.local/", "module-help-ai-agent-api.local", "/"},
		{"/", "", "/"},
		{"", "", "/"},
	}
	for _, c := range cases {
		seg, rest := splitFirstSegment(c.in)
		if seg != c.seg || rest != c.rest {
			t.Errorf("splitFirstSegment(%q) = (%q,%q), want (%q,%q)", c.in, seg, rest, c.seg, c.rest)
		}
	}
}

func TestHandler_routesAndStripsPrefix(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Echo-Path", r.URL.Path)
		w.Header().Set("X-Echo-Host", r.Host)
		w.Header().Set("X-Echo-Query", r.URL.RawQuery)
		io.WriteString(w, "ok")
	}))
	defer backend.Close()

	port := mustPort(t, backend.URL)
	store := storeWith(Service{Slug: "svc.local", Port: port, Runtime: "node"})

	h := newHandler(store, false)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/svc.local/foo?x=1", nil)
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if got := rec.Result().Header.Get("X-Echo-Path"); got != "/foo" {
		t.Errorf("backend path = %q, want /foo", got)
	}
	if got := rec.Result().Header.Get("X-Echo-Query"); got != "x=1" {
		t.Errorf("backend query = %q, want x=1", got)
	}
	if got := rec.Result().Header.Get("X-Echo-Host"); got == "" {
		t.Error("backend Host header empty; expected rewrite to 127.0.0.1:port")
	}
}

func TestHandler_unknownHostReturns404Index(t *testing.T) {
	store := storeWith(Service{Slug: "known.local", Port: 4000, Runtime: "node"})
	h := newHandler(store, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/nope.local/x", nil))
	if rec.Code != 404 {
		t.Fatalf("status = %d", rec.Code)
	}
	if body := rec.Body.String(); !strings.Contains(body, "known.local") {
		t.Errorf("404 body should list known services, got: %s", body)
	}
}

func TestHandler_deadBackendReturns502(t *testing.T) {
	store := storeWith(Service{Slug: "dead.local", Port: 1, Runtime: "node"}) // nothing listens on :1
	h := newHandler(store, false)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/dead.local/x", nil))
	if rec.Code != 502 {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
}

// TestHandler_proxiesUpgrade verifies Connection: Upgrade is passed through and
// bytes are relayed bidirectionally. We use a minimal raw upgrade, not full WS,
// because ReverseProxy's switch-protocols path is protocol-agnostic.
func TestHandler_proxiesUpgrade(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Upgrade") != "echo" {
			http.Error(w, "no upgrade", 400)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Error("backend cannot hijack")
			return
		}
		conn, brw, err := hj.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		brw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: echo\r\nConnection: Upgrade\r\n\r\n")
		brw.Flush()
		line, _ := brw.ReadString('\n')
		brw.WriteString("echo:" + line)
		brw.Flush()
	}))
	defer backend.Close()

	port := mustPort(t, backend.URL)
	store := storeWith(Service{Slug: "ws.local", Port: port, Runtime: "node"})

	front := httptest.NewServer(newHandler(store, false))
	defer front.Close()

	u, _ := url.Parse(front.URL)
	conn, err := net.Dial("tcp", u.Host)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	fmt.Fprintf(conn, "GET /ws.local/ HTTP/1.1\r\nHost: x\r\nUpgrade: echo\r\nConnection: Upgrade\r\n\r\n")
	br := bufio.NewReader(conn)
	status, _ := br.ReadString('\n')
	if !strings.Contains(status, "101") {
		t.Fatalf("expected 101, got %q", status)
	}
	for {
		line, _ := br.ReadString('\n')
		if line == "\r\n" || line == "" {
			break
		}
	}
	fmt.Fprintf(conn, "hello\n")
	got, _ := br.ReadString('\n')
	if !strings.Contains(got, "echo:hello") {
		t.Fatalf("relay failed, got %q", got)
	}
}

func TestHandler_logsRequests(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok")
	}))
	defer backend.Close()
	port := mustPort(t, backend.URL)
	store := storeWith(Service{Slug: "svc.local", Port: port, Runtime: "node"})

	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	h := newHandler(store, true)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/svc.local/x", nil))

	out := buf.String()
	for _, want := range []string{"GET", "200", "127.0.0.1:" + strconv.Itoa(port)} {
		if !strings.Contains(out, want) {
			t.Errorf("log line missing %q; got: %q", want, out)
		}
	}
}

func TestHandler_loggingDisabledIsSilent(t *testing.T) {
	store := storeWith(Service{Slug: "svc.local", Port: 4000, Runtime: "node"})
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(os.Stderr)

	h := newHandler(store, false)
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/unknown.local/x", nil))
	if buf.Len() != 0 {
		t.Errorf("expected no log output, got: %q", buf.String())
	}
}

func mustPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	p, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatal(err)
	}
	return p
}
