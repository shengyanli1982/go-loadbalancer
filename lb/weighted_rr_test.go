package lb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWeightedRR_Select(t *testing.T) {
	selector := NewWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 1),
		NewWeightedBackend("c", 1),
	}

	result := selector.Select(backends)
	require.NotNil(t, result)
}

func TestWeightedRR_NilBackends(t *testing.T) {
	selector := NewWeightedRR()
	result := selector.Select(nil)
	assert.Nil(t, result)
}

func TestWeightedRR_EmptyBackends(t *testing.T) {
	selector := NewWeightedRR()
	result := selector.Select([]Backend{})
	assert.Nil(t, result)
}

func TestWeightedRR_WeightedDistribution(t *testing.T) {
	selector := NewWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 3),
	}
	picks := 10000

	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		b := selector.Select(backends)
		counts[b.Address()]++
	}

	assert.InDelta(t, 2500, counts["a"], 1000, "'a' (25%%) count")
	assert.InDelta(t, 7500, counts["b"], 1000, "'b' (75%%) count")
}

func TestWeightedRR_SingleBackend(t *testing.T) {
	selector := NewWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 5),
	}

	for i := 0; i < 10; i++ {
		result := selector.Select(backends)
		require.NotNil(t, result)
		assert.Equal(t, "a", result.Address())
	}
}

func TestWeightedRR_FallbackToWeight1(t *testing.T) {
	selector := NewWeightedRR()
	backends := []Backend{
		NewBackend("a"),
		NewBackend("b"),
	}
	picks := 100

	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		b := selector.Select(backends)
		counts[b.Address()]++
	}

	assert.True(t, counts["a"] > 0, "'a' should be selected")
	assert.True(t, counts["b"] > 0, "'b' should be selected")
}
