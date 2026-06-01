package core

import (
	"strings"
	"testing"
)

type scriptRunner struct {
	responses map[string][3]string
}

func (s scriptRunner) Run(name string, args ...string) (string, string, error) {
	key := name + " " + strings.Join(args, " ")
	r, ok := s.responses[key]
	if !ok {
		return "", "not stubbed", errString("stub")
	}
	var err error
	if r[2] != "" {
		err = errString(r[2])
	}
	return r[0], r[1], err
}

type errString string

func (e errString) Error() string { return string(e) }

func TestDoctor_tailscaleMissing(t *testing.T) {
	r := scriptRunner{responses: map[string][3]string{}}
	disc := newDiscoverer(r)
	cfg := discoverConfig{rng: PortRange{3000, 5000}}
	checks := runDoctor(r, disc, cfg, ModeFunnel)
	c := findCheck(t, checks, "tailscale installed")
	if c.OK || !strings.Contains(c.Fix, "tailscale.com/download") {
		t.Fatalf("expected failing tailscale check with link, got %+v", c)
	}
}

func TestDoctor_serveModeSkipsFunnelCheck(t *testing.T) {
	r := scriptRunner{responses: map[string][3]string{
		"tailscale version":                 {"1.98.2", "", ""},
		"tailscale status":                  {"100.1.1.1 node user macOS -", "", ""},
		"lsof -nP -iTCP -sTCP:LISTEN -Fpcn": {"", "", ""},
	}}
	disc := newDiscoverer(r)
	cfg := discoverConfig{rng: PortRange{3000, 5000}}
	checks := runDoctor(r, disc, cfg, ModeServe)
	for _, c := range checks {
		if c.Name == "funnel enabled" {
			t.Fatal("serve mode should not check funnel")
		}
	}
}

func TestDoctor_magicDNSAdvisoryWhenAcceptDNSOn(t *testing.T) {
	r := scriptRunner{responses: map[string][3]string{
		"tailscale version":                 {"1.98.2", "", ""},
		"tailscale status":                  {"100.1.1.1 node user macOS -", "", ""},
		"tailscale funnel status":           {"Funnel on", "", ""},
		"tailscale debug prefs":             {`{"CorpDNS":true}`, "", ""},
		"lsof -nP -iTCP -sTCP:LISTEN -Fpcn": {"", "", ""},
	}}
	disc := newDiscoverer(r)
	cfg := discoverConfig{rng: PortRange{3000, 5000}}
	checks := runDoctor(r, disc, cfg, ModeFunnel)
	c := findCheck(t, checks, "magicdns")
	if !c.OK {
		t.Fatal("magicdns advisory must not fail doctor")
	}
	if !strings.Contains(c.Note, "accept-dns=false") {
		t.Fatalf("note should suggest disabling accept-dns, got %q", c.Note)
	}
}

func TestDoctor_noMagicDNSAdvisoryWhenAcceptDNSOff(t *testing.T) {
	r := scriptRunner{responses: map[string][3]string{
		"tailscale version":                 {"1.98.2", "", ""},
		"tailscale status":                  {"100.1.1.1 node user macOS -", "", ""},
		"tailscale funnel status":           {"Funnel on", "", ""},
		"tailscale debug prefs":             {`{"CorpDNS":false}`, "", ""},
		"lsof -nP -iTCP -sTCP:LISTEN -Fpcn": {"", "", ""},
	}}
	disc := newDiscoverer(r)
	cfg := discoverConfig{rng: PortRange{3000, 5000}}
	for _, c := range runDoctor(r, disc, cfg, ModeFunnel) {
		if c.Name == "magicdns" {
			t.Fatal("no advisory expected when accept-dns is off")
		}
	}
}

func findCheck(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("check %q not found", name)
	return Check{}
}
