//go:build !windows

package main

import "testing"

func TestParseLsofListeners(t *testing.T) {
	// lsof -nP -iTCP -sTCP:LISTEN -Fpcn  (p=pid, c=command, n=name)
	out := "p4231\ncbun\nn*:4295\nn[::1]:4295\n" +
		"p630\ncControlCe\nn*:5000\n" +
		"p999\ncnode\nn127.0.0.1:8080\n" // 8080 out of range
	rng := PortRange{Lo: 3000, Hi: 5000}
	ls := parseLsofListeners(out, rng)
	// 4295 (deduped across v4/v6), 5000; not 8080
	if len(ls) != 2 {
		t.Fatalf("got %d listeners: %+v", len(ls), ls)
	}
	byPort := map[int]listener{}
	for _, l := range ls {
		byPort[l.Port] = l
	}
	if byPort[4295].PID != 4231 || byPort[4295].Comm != "bun" {
		t.Errorf("4295 wrong: %+v", byPort[4295])
	}
	if _, ok := byPort[5000]; !ok {
		t.Errorf("expected port 5000")
	}
}

func TestPortFromAddr(t *testing.T) {
	cases := map[string]int{"*:4764": 4764, "127.0.0.1:3000": 3000, "[::1]:3000": 3000, "[::]:5000": 5000, "bad": 0}
	for in, want := range cases {
		if got := portFromAddr(in); got != want {
			t.Errorf("portFromAddr(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestParsePsComm(t *testing.T) {
	out := "  4231 /opt/homebrew/bin/bun\n  630 /System/.../ControlCenter\n"
	m := parsePsComm(out)
	if m[4231] != "/opt/homebrew/bin/bun" {
		t.Errorf("4231 = %q", m[4231])
	}
}

func TestParseLsofCwd(t *testing.T) {
	out := "p4231\nn/Users/me/work/help-ai/services/agent\np630\nn/\n"
	m := parseLsofCwd(out)
	if m[4231] != "/Users/me/work/help-ai/services/agent" {
		t.Errorf("4231 cwd = %q", m[4231])
	}
}
