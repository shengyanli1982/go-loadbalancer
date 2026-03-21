package balancer

import (
	"strings"
	"sync"
	"time"
)

// AffinityStore stores session-to-node affinity hints.
type AffinityStore interface {
	Get(sessionID string) (string, bool)
	Set(sessionID, nodeID string, ttl time.Duration)
	Delete(sessionID string)
}

type affinityEntry struct {
	nodeID    string
	expiresAt time.Time
}

type memoryAffinityStore struct {
	mu    sync.RWMutex
	items map[string]affinityEntry
	now   func() time.Time
}

func newMemoryAffinityStore() *memoryAffinityStore {
	return newMemoryAffinityStoreWithNow(time.Now)
}

func newMemoryAffinityStoreWithNow(now func() time.Time) *memoryAffinityStore {
	return &memoryAffinityStore{
		items: make(map[string]affinityEntry),
		now:   now,
	}
}

func (s *memoryAffinityStore) Get(sessionID string) (string, bool) {
	if strings.TrimSpace(sessionID) == "" {
		return "", false
	}

	s.mu.RLock()
	entry, ok := s.items[sessionID]
	s.mu.RUnlock()
	if !ok {
		return "", false
	}
	if !entry.expiresAt.IsZero() && s.now().After(entry.expiresAt) {
		s.Delete(sessionID)
		return "", false
	}
	if strings.TrimSpace(entry.nodeID) == "" {
		return "", false
	}
	return entry.nodeID, true
}

func (s *memoryAffinityStore) Set(sessionID, nodeID string, ttl time.Duration) {
	if strings.TrimSpace(sessionID) == "" || strings.TrimSpace(nodeID) == "" {
		return
	}

	entry := affinityEntry{nodeID: nodeID}
	if ttl > 0 {
		entry.expiresAt = s.now().Add(ttl)
	}

	s.mu.Lock()
	s.items[sessionID] = entry
	s.mu.Unlock()
}

func (s *memoryAffinityStore) Delete(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}

	s.mu.Lock()
	delete(s.items, sessionID)
	s.mu.Unlock()
}
