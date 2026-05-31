package main

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
	defer cancel()
	go poll(ctx, store, 10*time.Millisecond)
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
