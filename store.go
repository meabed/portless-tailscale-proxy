package main

import (
	"sort"
	"sync"
)

// RouteStore holds the current slug→Service map with a de-register debounce:
// a service missing from discovery is kept until it has been absent for
// deregisterCycles consecutive refreshes. Fed by an injectable discover func.
type RouteStore struct {
	mu               sync.RWMutex
	services         map[string]Service
	missing          map[string]int // slug → consecutive missing refreshes
	deregisterCycles int
	discover         func() ([]Service, error)
}

// NewRouteStore creates an empty store. deregisterCycles < 1 is clamped to 1.
func NewRouteStore(discover func() ([]Service, error), deregisterCycles int) *RouteStore {
	if deregisterCycles < 1 {
		deregisterCycles = 1
	}
	return &RouteStore{
		services:         map[string]Service{},
		missing:          map[string]int{},
		deregisterCycles: deregisterCycles,
		discover:         discover,
	}
}

func (s *RouteStore) lookup(slug string) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	svc, ok := s.services[slug]
	return svc.Port, ok
}

func (s *RouteStore) snapshot() map[string]Service {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Service, len(s.services))
	for k, v := range s.services {
		out[k] = v
	}
	return out
}

// refresh re-discovers services. Newly-seen services are returned in `added`
// (and immediately registered/updated). Services missing for deregisterCycles
// consecutive refreshes are removed and returned in `removed` (slugs).
func (s *RouteStore) refresh() (added []Service, removed []string, err error) {
	svcs, err := s.discover()
	if err != nil {
		return nil, nil, err
	}
	next := make(map[string]Service, len(svcs))
	for _, svc := range svcs {
		next[svc.Slug] = svc
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for slug, svc := range next {
		if _, ok := s.services[slug]; !ok {
			added = append(added, svc)
		}
		s.services[slug] = svc // register or update (port may change)
		delete(s.missing, slug)
	}
	for slug := range s.services {
		if _, present := next[slug]; present {
			continue
		}
		s.missing[slug]++
		if s.missing[slug] >= s.deregisterCycles {
			delete(s.services, slug)
			delete(s.missing, slug)
			removed = append(removed, slug)
		}
	}
	sort.Slice(added, func(i, j int) bool { return added[i].Slug < added[j].Slug })
	sort.Strings(removed)
	return added, removed, nil
}
