package lb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRandom_Select(t *testing.T) {
	selector := NewRandom()
	backends := newTestBackends("a", "b", "c")

	result := selector.Select(backends)
	require.NotNil(t, result)
	assert.NotEmpty(t, result.Address())
}

func TestRandom_NilBackends(t *testing.T) {
	selector := NewRandom()
	result := selector.Select(nil)
	assert.Nil(t, result)
}

func TestRandom_EmptyBackends(t *testing.T) {
	selector := NewRandom()
	result := selector.Select([]Backend{})
	assert.Nil(t, result)
}

func TestRandom_SingleBackend(t *testing.T) {
	selector := NewRandom()
	backends := newTestBackends("a")

	for i := 0; i < 10; i++ {
		result := selector.Select(backends)
		require.NotNil(t, result)
		assert.Equal(t, "a", result.Address())
	}
}

func TestRandom_Distribution(t *testing.T) {
	selector := NewRandom()
	backends := newTestBackends("a", "b", "c")
	picks := 10000

	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		b := selector.Select(backends)
		counts[b.Address()]++
	}

	for _, addr := range []string{"a", "b", "c"} {
		assert.InDelta(t, picks/3, counts[addr], 1500,
			"%s count should be ~3333", addr)
	}
}
