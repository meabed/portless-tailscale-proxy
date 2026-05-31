package main

import (
	"context"
	"log"
	"time"
)

// poll refreshes the store on an interval (and once immediately), logging
// discovered and de-registered services.
func poll(ctx context.Context, store *RouteStore, interval time.Duration) {
	refresh := func() {
		added, removed, err := store.refresh()
		if err != nil {
			log.Printf("warn: discovery failed: %v", err)
			return
		}
		for _, svc := range added {
			rt := svc.Runtime
			if rt == "" {
				rt = "?"
			}
			dir := svc.Dir
			if dir == "" {
				dir = "—"
			}
			log.Printf("discovered %s  %s  :%d  %s", svc.Slug, rt, svc.Port, dir)
		}
		for _, slug := range removed {
			log.Printf("de-registered %s (gone %d scans)", slug, store.deregisterCycles)
		}
	}
	refresh()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			refresh()
		}
	}
}
