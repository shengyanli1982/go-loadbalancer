package lb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestBackends(addrs ...string) []Backend {
	backends := make([]Backend, len(addrs))
	for i, addr := range addrs {
		backends[i] = NewBackend(addr)
	}
	return backends
}

func TestRoundRobin_Select(t *testing.T) {
	selector := NewRoundRobin()

	result := selector.Select(newTestBackends("a", "b", "c"))
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Address())
}

func TestRoundRobin_SingleBackend(t *testing.T) {
	selector := NewRoundRobin()
	backends := newTestBackends("a")

	for i := 0; i < 5; i++ {
		result := selector.Select(backends)
		require.NotNil(t, result)
		assert.Equal(t, "a", result.Address())
	}
}

func TestRoundRobin_NilBackends(t *testing.T) {
	selector := NewRoundRobin()
	result := selector.Select(nil)
	assert.Nil(t, result)
}

func TestRoundRobin_EmptyBackends(t *testing.T) {
	selector := NewRoundRobin()
	result := selector.Select([]Backend{})
	assert.Nil(t, result)
}

func TestRoundRobin_Distribution(t *testing.T) {
	selector := NewRoundRobin()
	backends := newTestBackends("a", "b", "c")
	picks := 30

	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		b := selector.Select(backends)
		counts[b.Address()]++
	}

	assert.Equal(t, 10, counts["a"])
	assert.Equal(t, 10, counts["b"])
	assert.Equal(t, 10, counts["c"])
}

func TestRoundRobin_Overflow(t *testing.T) {
	rr := NewRoundRobin().(*roundRobin)
	backends := newTestBackends("a", "b", "c")

	// 将 counter 设置到接近 uint64 最大值
	rr.index.Store(0xFFFFFFFFFFFFFFFF - 2)

	// 连续调用 5 次，不应 panic（包括 wraparound）
	for i := 0; i < 5; i++ {
		b := rr.Select(backends)
		require.NotNil(t, b, "unexpected nil backend at iteration %d", i)
	}
}
