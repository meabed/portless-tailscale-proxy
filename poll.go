package main

import (
	"context"
	"log"
	"strconv"
	"strings"
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
				log.Printf("note: %q listens on %d ports [%s] — serving :%d (pid %d, most recent)",
					d.Slug, len(d.Others)+1, portList(d), d.Chosen.Port, d.Chosen.PID)
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

func runtimeOr(rt string) string {
	if rt == "" {
		return "?"
	}
	return rt
}

func dirOr(dir string) string {
	if dir == "" {
		return "—"
	}
	return dir
}

// portList renders all instance ports for a duplicate, marking the served one.
func portList(d Duplicate) string {
	all := append([]Service{d.Chosen}, d.Others...)
	parts := make([]string, 0, len(all))
	for _, s := range all {
		p := ":" + strconv.Itoa(s.Port) + "(pid " + strconv.Itoa(s.PID) + ")"
		if s.Port == d.Chosen.Port && s.PID == d.Chosen.PID {
			p += "←used"
		}
		parts = append(parts, p)
	}
	return strings.Join(parts, ", ")
}

// dupKey is a stable fingerprint of the duplicate set for change detection.
func dupKey(dups []Duplicate) string {
	var b strings.Builder
	for _, d := range dups {
		b.WriteString(d.Slug)
		b.WriteByte('=')
		b.WriteString(strconv.Itoa(d.Chosen.Port))
		for _, o := range d.Others {
			b.WriteByte(',')
			b.WriteString(strconv.Itoa(o.Port))
		}
		b.WriteByte(';')
	}
	return b.String()
}
