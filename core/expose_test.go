package core

import (
	"strings"
	"testing"
)

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

func TestExposeArgs_funnelDefault(t *testing.T) {
	got := strings.Join(exposeArgs(ModeFunnel, 8443, 443), " ")
	if got != "funnel --bg 8443" {
		t.Fatalf("got %q", got)
	}
}

func TestExposeArgs_funnelCustomPort(t *testing.T) {
	got := strings.Join(exposeArgs(ModeFunnel, 8443, 8443), " ")
	if got != "funnel --bg --https 8443 8443" {
		t.Fatalf("got %q", got)
	}
}

func TestExposeArgs_serve(t *testing.T) {
	got := strings.Join(exposeArgs(ModeServe, 8443, 443), " ")
	if got != "serve --bg 8443" {
		t.Fatalf("got %q", got)
	}
}

func TestExposeStartAndReset(t *testing.T) {
	r := &fakeRunner{}
	if err := exposeStart(r, ModeServe, 8443, 443); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.calls[0], " ") != "tailscale serve --bg 8443" {
		t.Fatalf("start: %v", r.calls[0])
	}
	r2 := &fakeRunner{}
	if err := exposeReset(r2, ModeFunnel); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r2.calls[0], " ") != "tailscale funnel reset" {
		t.Fatalf("reset: %v", r2.calls[0])
	}
}

func TestNodeDNSName(t *testing.T) {
	r := &fakeRunner{stdout: `{"Self":{"DNSName":"bigfoot.quoll-adhara.ts.net."}}`}
	name, err := nodeDNSName(r)
	if err != nil {
		t.Fatal(err)
	}
	if name != "bigfoot.quoll-adhara.ts.net" {
		t.Fatalf("got %q", name)
	}
}

func TestSetAcceptDNS(t *testing.T) {
	r := &fakeRunner{}
	if err := setAcceptDNS(r, "false"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(r.calls[0], " ") != "tailscale set --accept-dns=false" {
		t.Fatalf("got %v", r.calls[0])
	}
	if err := setAcceptDNS(&fakeRunner{}, "maybe"); err == nil {
		t.Fatal("expected error for invalid value")
	}
}

func TestAcceptDNSEnabled(t *testing.T) {
	on, known := acceptDNSEnabled(&fakeRunner{stdout: `{"CorpDNS":true}`})
	if !known || !on {
		t.Fatalf("want on/known, got on=%v known=%v", on, known)
	}
	off, known := acceptDNSEnabled(&fakeRunner{stdout: `{"CorpDNS":false}`})
	if !known || off {
		t.Fatalf("want off/known, got off=%v known=%v", off, known)
	}
	if _, known := acceptDNSEnabled(&fakeRunner{err: errString("boom")}); known {
		t.Fatal("error should yield known=false")
	}
}

func TestPublicBase(t *testing.T) {
	if got := publicBase("n.ts.net", 443); got != "https://n.ts.net" {
		t.Errorf("443: %q", got)
	}
	if got := publicBase("n.ts.net", 8443); got != "https://n.ts.net:8443" {
		t.Errorf("8443: %q", got)
	}
}
