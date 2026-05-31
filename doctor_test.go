package main

import (
	"strings"
	"testing"
)

// scriptRunner returns different output per (name,args) key.
type scriptRunner struct {
	responses map[string][3]string // key -> {stdout, stderr, errMsg}
}

func (s scriptRunner) Run(name string, args ...string) (string, string, error) {
	key := name + " " + strings.Join(args, " ")
	r, ok := s.responses[key]
	if !ok {
		return "", "not stubbed", errStub
	}
	var err error
	if r[2] != "" {
		err = errString(r[2])
	}
	return r[0], r[1], err
}

func TestDoctor_tailscaleMissing(t *testing.T) {
	r := scriptRunner{responses: map[string][3]string{}} // nothing stubbed → all error
	checks := runDoctor(r, "/nonexistent/routes.json")
	c := findCheck(t, checks, "tailscale installed")
	if c.OK {
		t.Fatal("expected tailscale check to fail")
	}
	if !strings.Contains(c.Fix, "tailscale.com/download") {
		t.Errorf("fix should link to install docs, got %q", c.Fix)
	}
}

func TestDoctor_allGood(t *testing.T) {
	statePath := writeTemp(t, `[{"hostname":"a.local","port":1}]`)
	r := scriptRunner{responses: map[string][3]string{
		"tailscale version":       {"1.80.0", "", ""},
		"tailscale status":        {"100.1.1.1 node user macOS -", "", ""},
		"tailscale funnel status": {"https://node.ts.net (Funnel on)", "", ""},
	}}
	checks := runDoctor(r, statePath)
	for _, c := range checks {
		if !c.OK {
			t.Errorf("check %q unexpectedly failed: %s", c.Name, c.Detail)
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

type errString string

func (e errString) Error() string { return string(e) }

var errStub = errString("stub: not configured")
