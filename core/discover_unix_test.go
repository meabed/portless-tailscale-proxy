//go:build !windows

package core

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

func TestParseDockerListeners_hostBoundAndInternalFallback(t *testing.T) {
	body := []byte(`[
		{
			"Names":["/web"],
			"Ports":[
				{"PrivatePort":3000,"PublicPort":49153},
				{"PrivatePort":8080,"PublicPort":0}
			],
			"NetworkSettings":{"Networks":{"bridge":{"IPAddress":"172.17.0.2"}}}
		},
		{
			"Names":["/worker"],
			"Ports":[{"PrivatePort":9000,"PublicPort":0}],
			"NetworkSettings":{"Networks":{"bridge":{"IPAddress":""}}}
		}
	]`)

	ls := parseDockerListeners(body, PortRange{Lo: 3000, Hi: 50000})
	if len(ls) != 2 {
		t.Fatalf("got %d listeners: %+v", len(ls), ls)
	}
	if ls[0].Port != 49153 || ls[0].Host != "127.0.0.1" || ls[0].Cwd != "web" {
		t.Fatalf("host-bound listener wrong: %+v", ls[0])
	}
	if ls[1].Port != 8080 || ls[1].Host != "172.17.0.2" || ls[1].Cwd != "web" {
		t.Fatalf("internal fallback listener wrong: %+v", ls[1])
	}
	if ls[0].PID == ls[1].PID {
		t.Fatalf("docker ports need distinct synthetic PIDs, got %+v", ls)
	}
}

func TestDockerListenersBuildServices_keepsMultipleContainerPorts(t *testing.T) {
	ls := []listener{
		{Port: 3000, Host: "172.17.0.2", PID: -1, Comm: "docker", Cwd: "web"},
		{Port: 8080, Host: "172.17.0.2", PID: -2, Comm: "docker", Cwd: "web"},
	}

	svcs, dups := buildServices(ls, false, nil)
	if len(svcs) != 2 {
		t.Fatalf("got %d services: %+v", len(svcs), svcs)
	}
	if len(dups) != 1 {
		t.Fatalf("got %d duplicate groups: %+v", len(dups), dups)
	}
	if svcs[0].Host != "172.17.0.2" || svcs[1].Host != "172.17.0.2" {
		t.Fatalf("service hosts not preserved: %+v", svcs)
	}
}

func TestDockerListenerContainerNamesRemainDistinctProjects(t *testing.T) {
	ls := []listener{
		{Port: 3000, Host: "127.0.0.1", PID: -1, Comm: "docker", Cwd: "web-api"},
		{Port: 8090, Host: "127.0.0.1", PID: -2, Comm: "docker", Cwd: "model-server"},
	}

	svcs, dups := buildServices(ls, false, nil)
	if len(svcs) != 2 {
		t.Fatalf("got %d services: %+v", len(svcs), svcs)
	}
	if len(dups) != 0 {
		t.Fatalf("distinct containers should not be duplicates: %+v", dups)
	}
	bySlug := map[string]Service{}
	for _, svc := range svcs {
		bySlug[svc.Slug] = svc
	}
	if _, ok := bySlug["web-api"]; !ok {
		t.Fatalf("missing web-api slug: %+v", svcs)
	}
	if _, ok := bySlug["model-server"]; !ok {
		t.Fatalf("missing model-server slug: %+v", svcs)
	}
}
