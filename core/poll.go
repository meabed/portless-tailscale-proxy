package core

import (
	"context"
	"log"
	"time"
)

// poll refreshes the store on an interval (and once immediately), logging
// discovered / re-pointed / de-registered services and same-project duplicates.
func poll(ctx context.Context, store *RouteStore, interval time.Duration) {
	var lastDupKey string
	refresh := func() {
		added, repointed, removed, err := store.refresh()
		if err != nil {
			log.Printf("warn: discovery failed: %v", err)
			return
		}
		for _, svc := range added {
			log.Printf("discovered  %s  %s  :%d  pid %d  %s",
				svc.Slug, runtimeOr(svc.Runtime), svc.Port, svc.PID, dirOr(svc.Dir))
		}
		for _, svc := range repointed {
			log.Printf("re-pointed  %s  →  :%d  pid %d  (most recent instance changed)",
				svc.Slug, svc.Port, svc.PID)
		}
		for _, slug := range removed {
			log.Printf("de-registered  %s  (gone %d scans)", slug, store.deregisterCycles)
		}
		// Log duplicate notes only when the set changes (avoid per-scan spam).
		dups := store.dupes()
		if key := dupKey(dups); key != lastDupKey {
			lastDupKey = key
			for _, d := range dups {
				log.Printf("note: project exposes %d services — %s", len(d.Members), portList(d))
			}
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
