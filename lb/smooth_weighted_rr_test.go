package lb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSmoothWeightedRR_Select(t *testing.T) {
	selector := NewSmoothWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 2),
		NewWeightedBackend("c", 3),
	}

	result := selector.Select(backends)
	require.NotNil(t, result)
}

func TestSmoothWeightedRR_NilBackends(t *testing.T) {
	selector := NewSmoothWeightedRR()
	result := selector.Select(nil)
	assert.Nil(t, result)
}

func TestSmoothWeightedRR_EmptyBackends(t *testing.T) {
	selector := NewSmoothWeightedRR()
	result := selector.Select([]Backend{})
	assert.Nil(t, result)
}

func TestSmoothWeightedRR_Distribution(t *testing.T) {
	selector := NewSmoothWeightedRR()
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
