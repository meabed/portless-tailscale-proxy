package main

import (
	"testing"
)

func TestRouteStore_addAndLookup(t *testing.T) {
	store := NewRouteStore(func() ([]Service, error) {
		return []Service{{Slug: "a", Port: 1, Runtime: "node"}}, nil
	}, 1)

	added, removed, err := store.refresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(added) != 1 || added[0].Slug != "a" {
		t.Errorf("expected added=[{Slug:a,...}], got %v", added)
	}
	if len(removed) != 0 {
		t.Errorf("expected removed=[], got %v", removed)
	}
	port, ok := store.lookup("a")
	if !ok || port != 1 {
		t.Errorf("lookup(a) = (%d, %v), want (1, true)", port, ok)
	}
	snap := store.snapshot()
	if snap["a"].Runtime != "node" {
		t.Errorf("snapshot[a].Runtime = %q, want %q", snap["a"].Runtime, "node")
	}
}

func TestRouteStore_debounceDeregister(t *testing.T) {
	var returnEmpty bool
	store := NewRouteStore(func() ([]Service, error) {
		if returnEmpty {
			return nil, nil
		}
		return []Service{{Slug: "a", Port: 1, Runtime: "node"}}, nil
	}, 3)

	// First refresh: a is added.
	added, removed, err := store.refresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(added) != 1 || added[0].Slug != "a" {
		t.Errorf("expected added=[a], got %v", added)
	}

	// Now return empty.
	returnEmpty = true

	// Refresh #1 missing: a should still be present.
	added, removed, err = store.refresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("refresh #1: a should not be removed yet, removed=%v", removed)
	}
	if _, ok := store.lookup("a"); !ok {
		t.Error("refresh #1: a should still be reachable via lookup")
	}

	// Refresh #2 missing: a should still be present.
	added, removed, err = store.refresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("refresh #2: a should not be removed yet, removed=%v", removed)
	}
	if _, ok := store.lookup("a"); !ok {
		t.Error("refresh #2: a should still be reachable via lookup")
	}

	// Refresh #3 missing: a should now be removed.
	added, removed, err = store.refresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 1 || removed[0] != "a" {
		t.Errorf("refresh #3: expected removed=[a], got %v", removed)
	}
	if _, ok := store.lookup("a"); ok {
		t.Error("refresh #3: a should have been de-registered")
	}
	_ = added
}

func TestRouteStore_reappearResetsMissing(t *testing.T) {
	var returnEmpty bool
	store := NewRouteStore(func() ([]Service, error) {
		if returnEmpty {
			return nil, nil
		}
		return []Service{{Slug: "a", Port: 1, Runtime: "node"}}, nil
	}, 3)

	// Add a.
	_, _, err := store.refresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// One empty refresh (missing count = 1).
	returnEmpty = true
	_, removed, err := store.refresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(removed) != 0 {
		t.Errorf("expected a to still be present, removed=%v", removed)
	}

	// a reappears — missing count resets to 0, should NOT appear in added (already known).
	returnEmpty = false
	added, removed, err := store.refresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("a reappear: expected added=[], got %v", added)
	}
	if len(removed) != 0 {
		t.Errorf("a reappear: expected removed=[], got %v", removed)
	}
	if _, ok := store.lookup("a"); !ok {
		t.Error("a should still be reachable after reappearing")
	}

	// Now need 3 full consecutive missing refreshes to remove.
	returnEmpty = true
	for i := 1; i <= 2; i++ {
		_, removed, err = store.refresh()
		if err != nil {
			t.Fatalf("unexpected error on missing refresh %d: %v", i, err)
		}
		if len(removed) != 0 {
			t.Errorf("missing refresh %d: expected a still present, removed=%v", i, removed)
		}
		if _, ok := store.lookup("a"); !ok {
			t.Errorf("missing refresh %d: a should still be reachable", i)
		}
	}
	// 3rd missing refresh should finally remove a.
	_, removed, err = store.refresh()
	if err != nil {
		t.Fatalf("unexpected error on final missing refresh: %v", err)
	}
	if len(removed) != 1 || removed[0] != "a" {
		t.Errorf("final missing refresh: expected removed=[a], got %v", removed)
	}
	if _, ok := store.lookup("a"); ok {
		t.Error("a should have been de-registered after 3 consecutive missing refreshes")
	}
}

func TestRouteStore_portUpdateNotReAdded(t *testing.T) {
	port := 1
	store := NewRouteStore(func() ([]Service, error) {
		return []Service{{Slug: "a", Port: port, Runtime: "node"}}, nil
	}, 1)

	// First refresh: a@port1 is added.
	added, _, err := store.refresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(added) != 1 || added[0].Slug != "a" {
		t.Errorf("expected added=[a], got %v", added)
	}

	// Second refresh: a@port2 — same slug, different port.
	port = 2
	added, removed, err := store.refresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(added) != 0 {
		t.Errorf("port update: expected added=[], got %v", added)
	}
	if len(removed) != 0 {
		t.Errorf("port update: expected removed=[], got %v", removed)
	}
	snap := store.snapshot()
	if snap["a"].Port != 2 {
		t.Errorf("port update: snapshot[a].Port = %d, want 2", snap["a"].Port)
	}
}
