package core

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestPoll_picksUpChanges(t *testing.T) {
	var stage atomic.Int32
	store := NewRouteStore(func() ([]Service, []Duplicate, error) {
		if stage.Load() == 0 {
			return nil, nil, nil
		}
		return []Service{{Slug: "x", Port: 9, Runtime: "node"}}, nil, nil
	}, 5)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { poll(ctx, store, 10*time.Millisecond, ""); close(done) }()
	// Join the poll goroutine before the test ends so it can't keep writing to the
	// global logger while a later test reads it (-race cross-test data race).
	t.Cleanup(func() { cancel(); <-done })
	stage.Store(1)
	deadline := time.After(2 * time.Second)
	for {
		if p, ok := store.lookup("x"); ok && p == 9 {
			return
		}
		select {
		case <-deadline:
			t.Fatal("poll did not pick up the new service")
		case <-time.After(10 * time.Millisecond):
		}
	}
}
