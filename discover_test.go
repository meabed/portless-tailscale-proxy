package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassifyRuntime(t *testing.T) {
	cases := map[string]string{
		"/opt/homebrew/bin/node": "node",
		"bun":                    "bun",
		"/usr/bin/bun.exe":       "bun",
		"deno":                   "deno",
		"/usr/bin/python":        "", // not a default runtime anymore
		"ruby":                   "", // not a default runtime anymore
		"/opt/homebrew/opt/nats-server/bin/nats-server": "",
		"epmd": "",
	}
	for in, want := range cases {
		if got := classifyRuntime(in); got != want {
			t.Errorf("classifyRuntime(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestProjectRoot(t *testing.T) {
	root := t.TempDir()
	// root/proj/.git , root/proj/apps/web (cwd) -> "proj"
	web := filepath.Join(root, "proj", "apps", "web")
	if err := os.MkdirAll(web, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "proj", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := projectRoot(web); got != "proj" {
		t.Errorf("projectRoot = %q, want proj", got)
	}
	// no markers -> basename of cwd
	plain := filepath.Join(root, "loose", "thing")
	os.MkdirAll(plain, 0o755)
	if got := projectRoot(plain); got != "thing" {
		t.Errorf("projectRoot(no markers) = %q, want thing", got)
	}
	if got := projectRoot(""); got != "" {
		t.Errorf("projectRoot(empty) = %q, want empty", got)
	}
}

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Help AI Web":  "help-ai-web",
		"my_app.v2":    "my-app-v2",
		"  Spaced  ":   "spaced",
		"already-slug": "already-slug",
		"@scope/pkg":   "scope-pkg",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildServices_filtersAndDisambiguates(t *testing.T) {
	dirA := t.TempDir() // -> basename used as slug source
	listeners := []listener{
		{Port: 4983, PID: 1, Comm: "/usr/bin/node", Cwd: dirA},
		{Port: 4222, PID: 2, Comm: "nats-server", Cwd: ""}, // unknown runtime -> dropped
		{Port: 3000, PID: 3, Comm: "bun", Cwd: ""},         // no cwd -> bun-3000
		{Port: 3001, PID: 4, Comm: "bun", Cwd: ""},         // no cwd -> bun-3001
	}
	svcs := buildServices(listeners, false, nil)
	got := map[string]Service{}
	for _, s := range svcs {
		got[s.Slug] = s
	}
	if _, ok := got["bun-3000"]; !ok {
		t.Errorf("expected bun-3000 slug, got %v", keysOf(got))
	}
	if _, ok := got["bun-3001"]; !ok {
		t.Errorf("expected bun-3001 slug, got %v", keysOf(got))
	}
	for _, s := range svcs {
		if s.Runtime == "" {
			t.Errorf("unknown-runtime service should have been filtered: %+v", s)
		}
	}
}

func TestBuildServices_collisionSuffix(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "proj", "api")
	b := filepath.Join(root, "proj", "api2")
	os.MkdirAll(filepath.Join(root, "proj", ".git"), 0o755)
	os.MkdirAll(a, 0o755)
	os.MkdirAll(b, 0o755)
	// both resolve project root to "proj" -> collision -> -<port> suffix
	svcs := buildServices([]listener{
		{Port: 4001, PID: 1, Comm: "node", Cwd: a},
		{Port: 4002, PID: 2, Comm: "node", Cwd: b},
	}, false, nil)
	slugs := map[string]bool{}
	for _, s := range svcs {
		slugs[s.Slug] = true
	}
	if !slugs["proj-4001"] || !slugs["proj-4002"] {
		t.Errorf("expected proj-4001 and proj-4002, got %v", slugs)
	}
}

func TestParsePortRange(t *testing.T) {
	r, err := parsePortRange("3000-5000")
	if err != nil || r.Lo != 3000 || r.Hi != 5000 {
		t.Fatalf("got %+v err %v", r, err)
	}
	// Single port is accepted and yields an inclusive single-port range.
	r, err = parsePortRange("4000")
	if err != nil || r.Lo != 4000 || r.Hi != 4000 {
		t.Fatalf("single port: got %+v err %v", r, err)
	}
	for _, bad := range []string{"5000-3000", "abc", "0-10", "1-70000", "0", "70000", "-5"} {
		if _, err := parsePortRange(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestDisambiguate_tripleCollisionWithExistingPortSlug(t *testing.T) {
	// Two services slug to "proj" (ports 4001, 4002); a third already slugs to
	// "proj-4001". After the -port pass, the renamed proj@4001 collides with the
	// pre-existing proj-4001 → both get a -<pid> tie-break so all slugs are unique.
	svcs := []Service{
		{Slug: "proj", Port: 4001, PID: 11, Runtime: "node"},
		{Slug: "proj", Port: 4002, PID: 12, Runtime: "node"},
		{Slug: "proj-4001", Port: 4003, PID: 13, Runtime: "node"},
	}
	disambiguate(svcs)
	seen := map[string]int{}
	for _, s := range svcs {
		seen[s.Slug]++
	}
	for slug, n := range seen {
		if n > 1 {
			t.Fatalf("slug %q still collides (%d): %+v", slug, n, svcs)
		}
	}
}

func keysOf(m map[string]Service) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
