package balancer

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestMemoryAffinityStoreSessionTTLExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0).UTC()
	store := newMemoryAffinityStoreWithNow(func() time.Time { return now })

	store.Set("session-a", "node-a", 5*time.Second)

	nodeID, ok := store.Get("session-a")
	assert.True(t, ok)
	assert.Equal(t, "node-a", nodeID)

	now = now.Add(6 * time.Second)
	nodeID, ok = store.Get("session-a")
	assert.False(t, ok)
	assert.Empty(t, nodeID)
}

func TestMemoryAffinityStoreSessionDelete(t *testing.T) {
	store := newMemoryAffinityStore()
	store.Set("session-a", "node-a", 0)

	nodeID, ok := store.Get("session-a")
	assert.True(t, ok)
	assert.Equal(t, "node-a", nodeID)

	store.Delete("session-a")

	nodeID, ok = store.Get("session-a")
	assert.False(t, ok)
	assert.Empty(t, nodeID)
}
