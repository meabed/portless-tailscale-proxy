package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// Route mirrors one entry in ~/.portless/routes.json.
type Route struct {
	Hostname string `json:"hostname"`
	Port     int    `json:"port"`
	PID      int    `json:"pid"`
}

// defaultStatePath returns the portless routes file under the user's home dir.
func defaultStatePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".portless", "routes.json"), nil
}

// loadRoutes reads and parses the portless routes file into a hostname→port map.
// A missing file yields an empty map and no error (portless may not be running).
func loadRoutes(statePath string) (map[string]int, error) {
	data, err := os.ReadFile(statePath)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]int{}, nil
		}
		return nil, err
	}
	var routes []Route
	if err := json.Unmarshal(data, &routes); err != nil {
		return nil, err
	}
	m := make(map[string]int, len(routes))
	for _, r := range routes {
		if r.Hostname != "" && r.Port > 0 {
			m[r.Hostname] = r.Port
		}
	}
	return m, nil
}

// RouteStore holds the current hostname→port map behind a RWMutex.
type RouteStore struct {
	mu        sync.RWMutex
	routes    map[string]int
	statePath string
}

// NewRouteStore creates an empty store bound to a routes.json path.
func NewRouteStore(statePath string) *RouteStore {
	return &RouteStore{routes: map[string]int{}, statePath: statePath}
}

// lookup returns the port for a hostname and whether it is registered.
func (s *RouteStore) lookup(hostname string) (int, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.routes[hostname]
	return p, ok
}

// snapshot returns a copy of the current map.
func (s *RouteStore) snapshot() map[string]int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]int, len(s.routes))
	for k, v := range s.routes {
		out[k] = v
	}
	return out
}

// refresh reloads routes from disk, swaps the map atomically, and reports diffs.
func (s *RouteStore) refresh() (added, removed []string, err error) {
	next, err := loadRoutes(s.statePath)
	if err != nil {
		return nil, nil, err
	}
	s.mu.Lock()
	prev := s.routes
	s.routes = next
	s.mu.Unlock()
	for h := range next {
		if _, ok := prev[h]; !ok {
			added = append(added, h)
		}
	}
	for h := range prev {
		if _, ok := next[h]; !ok {
			removed = append(removed, h)
		}
	}
	sort.Strings(added)
	sort.Strings(removed)
	return added, removed, nil
}
