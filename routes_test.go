package main

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func writeTemp(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "routes.json")
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadRoutes_parsesHostnamePort(t *testing.T) {
	p := writeTemp(t, `[
	  {"hostname":"www-web-help-ai.local","port":4764,"pid":4154},
	  {"hostname":"module-help-ai-agent-api.local","port":4434,"pid":4315}
	]`)
	m, err := loadRoutes(p)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := m["www-web-help-ai.local"]; got != 4764 {
		t.Errorf("want 4764, got %d", got)
	}
	if got := m["module-help-ai-agent-api.local"]; got != 4434 {
		t.Errorf("want 4434, got %d", got)
	}
}

func TestLoadRoutes_missingFileIsEmpty(t *testing.T) {
	m, err := loadRoutes(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("missing file should not error, got %v", err)
	}
	if len(m) != 0 {
		t.Errorf("want empty map, got %d entries", len(m))
	}
}

func TestLoadRoutes_skipsInvalidEntries(t *testing.T) {
	p := writeTemp(t, `[{"hostname":"","port":1},{"hostname":"ok.local","port":0},{"hostname":"good.local","port":3000}]`)
	m, err := loadRoutes(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(m) != 1 || m["good.local"] != 3000 {
		t.Errorf("want only good.local=3000, got %v", m)
	}
}

func TestRouteStore_refreshDiff(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "routes.json")
	write := func(s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(`[{"hostname":"a.local","port":1}]`)
	s := NewRouteStore(p)

	added, removed, err := s.refresh()
	if err != nil {
		t.Fatal(err)
	}
	if len(added) != 1 || added[0] != "a.local" || len(removed) != 0 {
		t.Fatalf("first refresh: added=%v removed=%v", added, removed)
	}
	if port, ok := s.lookup("a.local"); !ok || port != 1 {
		t.Fatalf("lookup a.local: %d %v", port, ok)
	}

	write(`[{"hostname":"b.local","port":2}]`)
	added, removed, _ = s.refresh()
	sort.Strings(added)
	if len(added) != 1 || added[0] != "b.local" || len(removed) != 1 || removed[0] != "a.local" {
		t.Fatalf("second refresh: added=%v removed=%v", added, removed)
	}
	if _, ok := s.lookup("a.local"); ok {
		t.Fatal("a.local should be gone")
	}
}
