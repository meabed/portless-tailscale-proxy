package main

import (
	"strings"
	"testing"
)

// fakeRunner records invocations and returns canned output.
type fakeRunner struct {
	calls  [][]string
	stdout string
	stderr string
	err    error
}

func (f *fakeRunner) Run(name string, args ...string) (string, string, error) {
	f.calls = append(f.calls, append([]string{name}, args...))
	return f.stdout, f.stderr, f.err
}

func TestFunnelStart_defaultPort(t *testing.T) {
	r := &fakeRunner{}
	if err := funnelStart(r, 8443, 443); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.calls[0], " ")
	if got != "tailscale funnel --bg 8443" {
		t.Fatalf("got %q", got)
	}
}

func TestFunnelStart_customPublicPort(t *testing.T) {
	r := &fakeRunner{}
	if err := funnelStart(r, 8443, 8443); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(r.calls[0], " ")
	if got != "tailscale funnel --bg --https 8443 8443" {
		t.Fatalf("got %q", got)
	}
}

func TestFunnelReset(t *testing.T) {
	r := &fakeRunner{}
	if err := funnelReset(r); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.calls[0], " ") != "tailscale funnel reset" {
		t.Fatalf("got %v", r.calls[0])
	}
}

func TestNodeDNSName(t *testing.T) {
	r := &fakeRunner{stdout: `{"Self":{"DNSName":"bigfoot.quoll-adhara.ts.net."}}`}
	name, err := nodeDNSName(r)
	if err != nil {
		t.Fatal(err)
	}
	if name != "bigfoot.quoll-adhara.ts.net" {
		t.Fatalf("got %q, want trailing dot stripped", name)
	}
}

func TestPublicBase(t *testing.T) {
	if got := publicBase("n.ts.net", 443); got != "https://n.ts.net" {
		t.Errorf("443: got %q", got)
	}
	if got := publicBase("n.ts.net", 8443); got != "https://n.ts.net:8443" {
		t.Errorf("8443: got %q", got)
	}
}
