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
		"/usr/bin/python":        "python",
		"python3.12":             "python",
		"uvicorn":                "python",
		"gunicorn":               "python",
		"ruby":                   "ruby",
		"puma":                   "ruby",
		"/usr/bin/php":           "php",
		"php-fpm":                "php",
		"java":                   "java",
		"dotnet":                 "dotnet",
		"beam.smp":               "elixir",
		"perl":                   "perl",
		"docker-proxy":           "docker",
		"com.docker.backend":     "docker",
		"go":                     "go",
		"/var/folders/x/go-build123/b001/exe/main":      "go", // `go run` temp binary
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

// mkProject creates root/<name> with a .git marker and returns its path.
func mkProject(t *testing.T, root, name string) string {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func svcMap(svcs []Service) map[string]Service {
	m := map[string]Service{}
	for _, s := range svcs {
		m[s.Slug] = s
	}
	return m
}

func TestBuildServices_filtersUnknownRuntime(t *testing.T) {
	root := t.TempDir()
	app := mkProject(t, root, "app")
	svcs, _ := buildServices([]listener{
		{Port: 4983, PID: 1, Comm: "/usr/bin/node", Cwd: app},
		{Port: 4222, PID: 2, Comm: "nats-server", Cwd: ""}, // unknown runtime → dropped
	}, false, nil)
	if len(svcs) != 1 || svcs[0].Slug != "app" || svcs[0].Runtime != "node" {
		t.Fatalf("want one node service 'app', got %+v", svcs)
	}
}

func TestBuildServices_distinctProcessesSameFolder(t *testing.T) {
	// Same project folder, two DISTINCT processes: the real dev server (bun :3087)
	// and a tool (node :4983, e.g. @ai-sdk/devtools). BOTH are exposed — the
	// lowest-port process is "main" (clean slug); the other is suffixed so it
	// stays reachable.
	root := t.TempDir()
	agent := mkProject(t, root, "module-help-ai-agent-api")
	svcs, dups := buildServices([]listener{
		{Port: 4983, PID: 15588, Comm: "node", Cwd: agent}, // devtools
		{Port: 3087, PID: 79759, Comm: "bun", Cwd: agent},  // real service (lowest port)
	}, false, nil)

	got := svcMap(svcs)
	if len(got) != 2 {
		t.Fatalf("expected BOTH services exposed, got %v", keysOf(got))
	}
	main, ok := got["module-help-ai-agent-api"]
	if !ok || main.Port != 3087 {
		t.Fatalf("main (lowest port) should be :3087 under the clean slug, got %v", keysOf(got))
	}
	other, ok := got["module-help-ai-agent-api-4983"]
	if !ok || other.Port != 4983 {
		t.Fatalf("the tool should be suffixed as -4983, got %v", keysOf(got))
	}
	if len(dups) != 1 || len(dups[0].Members) != 2 || dups[0].Members[0].Port != 3087 {
		t.Fatalf("expected one project note with main :3087 first, got %+v", dups)
	}
}

func TestBuildServices_sameProcessMultiPort(t *testing.T) {
	// One process listening on two ports (dev server + its HMR port). Collapses to
	// that process's lowest port — one clean service, NOT a duplicate.
	root := t.TempDir()
	web := mkProject(t, root, "web-www-help-ai")
	svcs, dups := buildServices([]listener{
		{Port: 4501, PID: 78327, Comm: "node", Cwd: web},
		{Port: 4206, PID: 78327, Comm: "node", Cwd: web},
	}, false, nil)
	if len(svcs) != 1 || svcs[0].Slug != "web-www-help-ai" {
		t.Fatalf("expected one clean slug, got %+v", svcs)
	}
	if svcs[0].Port != 4206 {
		t.Errorf("a single process should collapse to its lowest port 4206, got %d", svcs[0].Port)
	}
	if len(dups) != 0 {
		t.Errorf("a single process is not a duplicate, got %+v", dups)
	}
}

func TestBuildServices_differentProjectsSameName(t *testing.T) {
	// Two DISTINCT projects that share a folder name "api" must NOT be merged;
	// they get a -<port> suffix to stay unique.
	root := t.TempDir()
	a := mkProject(t, filepath.Join(root, "svc1"), "api")
	b := mkProject(t, filepath.Join(root, "svc2"), "api")
	svcs, _ := buildServices([]listener{
		{Port: 4001, PID: 10, Comm: "node", Cwd: a},
		{Port: 4002, PID: 20, Comm: "node", Cwd: b},
	}, false, nil)
	got := svcMap(svcs)
	if len(got) != 2 || got["api-4001"].Port != 4001 || got["api-4002"].Port != 4002 {
		t.Fatalf("expected api-4001 and api-4002 (distinct projects), got %v", keysOf(got))
	}
}

func TestBuildServices_noCwdFallback(t *testing.T) {
	// No discoverable project → runtime-port slug; two such are not collapsed.
	svcs, _ := buildServices([]listener{
		{Port: 3000, PID: 3, Comm: "bun", Cwd: ""},
		{Port: 3001, PID: 4, Comm: "bun", Cwd: ""},
	}, false, nil)
	got := svcMap(svcs)
	if _, ok := got["bun-3000"]; !ok {
		t.Errorf("expected bun-3000, got %v", keysOf(got))
	}
	if _, ok := got["bun-3001"]; !ok {
		t.Errorf("expected bun-3001, got %v", keysOf(got))
	}
}

func TestBuildServices_includeAllAndRuntimesFilter(t *testing.T) {
	root := t.TempDir()
	app := mkProject(t, root, "app")
	ls := []listener{
		{Port: 4983, PID: 1, Comm: "node", Cwd: app},
		{Port: 4222, PID: 2, Comm: "nats-server", Cwd: ""},
	}
	// --all includes the unknown runtime too.
	all, _ := buildServices(ls, true, nil)
	if len(all) != 2 {
		t.Errorf("--all should include the nats listener, got %+v", all)
	}
	// --runtimes bun excludes the node service.
	only, _ := buildServices(ls, false, map[string]bool{"bun": true})
	if len(only) != 0 {
		t.Errorf("--runtimes bun should exclude node, got %+v", only)
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

func keysOf(m map[string]Service) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
