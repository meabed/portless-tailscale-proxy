package core

import "testing"

// fixedDiscover returns a discover func yielding the given services (no dups).
func fixedDiscover(svcs ...Service) func() ([]Service, []Duplicate, error) {
	return func() ([]Service, []Duplicate, error) { return svcs, nil, nil }
}

func TestRouteStore_addAndLookup(t *testing.T) {
	store := NewRouteStore(fixedDiscover(Service{Slug: "a", Port: 1, Runtime: "node"}), 1)

	added, repointed, removed, err := store.refresh()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(added) != 1 || added[0].Slug != "a" {
		t.Errorf("expected added=[a], got %v", added)
	}
	if len(repointed) != 0 || len(removed) != 0 {
		t.Errorf("expected no repointed/removed, got %v %v", repointed, removed)
	}
	if svc, ok := store.lookup("a"); !ok || svc.Port != 1 {
		t.Errorf("lookup(a) = (%+v, %v), want port 1 and true", svc, ok)
	}
	if store.snapshot()["a"].Runtime != "node" {
		t.Errorf("snapshot[a].Runtime wrong")
	}
}

func TestRouteStore_repointsWhenHostChanges(t *testing.T) {
	stage := 0
	store := NewRouteStore(func() ([]Service, []Duplicate, error) {
		stage++
		if stage == 1 {
			return []Service{{Slug: "a", Host: "127.0.0.1", Port: 3000}}, nil, nil
		}
		return []Service{{Slug: "a", Host: "172.17.0.2", Port: 3000}}, nil, nil
	}, 1)

	if added, repointed, _, _ := store.refresh(); len(added) != 1 || len(repointed) != 0 {
		t.Fatalf("initial refresh added/repointed = %v/%v", added, repointed)
	}
	_, repointed, _, _ := store.refresh()
	if len(repointed) != 1 || repointed[0].Host != "172.17.0.2" {
		t.Fatalf("host change should repoint, got %+v", repointed)
	}
}

func TestRouteStore_debounceDeregister(t *testing.T) {
	var empty bool
	store := NewRouteStore(func() ([]Service, []Duplicate, error) {
		if empty {
			return nil, nil, nil
		}
		return []Service{{Slug: "a", Port: 1, Runtime: "node"}}, nil, nil
	}, 3)

	store.refresh() // add a
	empty = true

	// Two missing refreshes: a stays (debounce).
	for i := 1; i <= 2; i++ {
		_, _, removed, _ := store.refresh()
		if len(removed) != 0 {
			t.Fatalf("refresh #%d: a removed too early: %v", i, removed)
		}
		if _, ok := store.lookup("a"); !ok {
			t.Fatalf("refresh #%d: a should still resolve", i)
		}
	}
	// Third missing refresh: a is de-registered.
	_, _, removed, _ := store.refresh()
	if len(removed) != 1 || removed[0] != "a" {
		t.Fatalf("refresh #3: expected removed=[a], got %v", removed)
	}
	if _, ok := store.lookup("a"); ok {
		t.Error("a should be gone after deregisterCycles missing scans")
	}
}

func TestRouteStore_reappearResetsMissing(t *testing.T) {
	var empty bool
	store := NewRouteStore(func() ([]Service, []Duplicate, error) {
		if empty {
			return nil, nil, nil
		}
		return []Service{{Slug: "a", Port: 1, Runtime: "node"}}, nil, nil
	}, 3)

	store.refresh()                   // add a
	empty = true                      //
	store.refresh()                   // missing #1
	empty = false                     //
	added, _, _, _ := store.refresh() // a reappears
	if len(added) != 0 {
		t.Errorf("reappear should not re-add a known service, got %v", added)
	}
	// Counter reset: it now takes a full 3 missing scans again.
	empty = true
	for i := 1; i <= 2; i++ {
		_, _, removed, _ := store.refresh()
		if len(removed) != 0 {
			t.Fatalf("post-reappear missing #%d removed too early", i)
		}
	}
	_, _, removed, _ := store.refresh()
	if len(removed) != 1 {
		t.Errorf("a should be removed after 3 fresh missing scans, got %v", removed)
	}
}

func TestRouteStore_repointOnPortChange(t *testing.T) {
	port := 1
	store := NewRouteStore(func() ([]Service, []Duplicate, error) {
		return []Service{{Slug: "a", Port: port, Runtime: "node"}}, nil, nil
	}, 1)

	store.refresh() // add a@1
	port = 2
	added, repointed, removed, _ := store.refresh()
	if len(added) != 0 || len(removed) != 0 {
		t.Errorf("port change should not add/remove, got added=%v removed=%v", added, removed)
	}
	if len(repointed) != 1 || repointed[0].Port != 2 {
		t.Errorf("expected repointed=[a@2], got %v", repointed)
	}
	if store.snapshot()["a"].Port != 2 {
		t.Errorf("snapshot should reflect new port 2")
	}
}

func TestRouteStore_exposesDuplicates(t *testing.T) {
	dup := Duplicate{
		Slug: "a",
		Members: []Service{
			{Slug: "a", Port: 3087, PID: 79759, Runtime: "bun"},
			{Slug: "a-4983", Port: 4983, PID: 15588, Runtime: "node"},
		},
	}
	store := NewRouteStore(func() ([]Service, []Duplicate, error) {
		return []Service{{Slug: "a", Port: 3087, Runtime: "bun"}}, []Duplicate{dup}, nil
	}, 1)
	store.refresh()
	got := store.dupes()
	if len(got) != 1 || got[0].Slug != "a" || len(got[0].Members) != 2 || got[0].Members[1].Slug != "a-4983" {
		t.Fatalf("store.dupes() = %+v, want the agent-api project with 2 members", got)
	}
}
