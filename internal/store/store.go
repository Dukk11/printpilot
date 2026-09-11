// Package store keeps the current status of every printer in memory.
package store

import (
	"sort"
	"sync"
	"time"

	"github.com/Dukk11/printpilot/internal/printer"
)

// Store is a concurrency-safe in-memory status registry.
type Store struct {
	mu      sync.RWMutex
	order   []string // printer IDs in config order
	status  map[string]printer.Status
	markOff func(id string) // optional callback invoked when offline is recorded
}

// New creates a store pre-seeded with the given printer IDs (config order).
// Every printer starts as offline so the UI is honest before the first update.
func New(ids []string) *Store {
	s := &Store{order: ids, status: make(map[string]printer.Status, len(ids))}
	for _, id := range ids {
		s.status[id] = printer.Status{ID: id, State: printer.StateOffline}
	}
	return s
}

// OnOffline registers a callback fired whenever a printer transitions into
// the offline state (used by the alert manager).
func (s *Store) OnOffline(fn func(id string)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.markOff = fn
}

// Update stores the newest status for id and returns the previous one.
func (s *Store) Update(st printer.Status) (prev printer.Status, had bool) {
	st.UpdatedAt = time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	prev, had = s.status[st.ID], true
	s.status[st.ID] = st
	if s.markOff != nil && prev.State != printer.StateOffline && st.State == printer.StateOffline {
		go s.markOff(st.ID)
	}
	return prev, had
}

// Snapshot returns all statuses in config order.
func (s *Store) Snapshot() []printer.Status {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]printer.Status, 0, len(s.order))
	for _, id := range s.order {
		out = append(out, s.status[id])
	}
	if len(s.order) == 0 {
		// Mock/demo fallback: any dynamically registered printers.
		for _, st := range s.status {
			out = append(out, st)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	}
	return out
}

// Get returns the current status of id.
func (s *Store) Get(id string) (printer.Status, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	st, ok := s.status[id]
	return st, ok
}
