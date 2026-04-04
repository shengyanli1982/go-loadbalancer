package lb

import (
	"testing"
)

func TestSmoothWeightedRR_Select(t *testing.T) {
	selector := NewSmoothWeightedRR()
	backends := []Backend{
		&simpleWeightedBackend{addr: "a", weight: 1},
		&simpleWeightedBackend{addr: "b", weight: 2},
		&simpleWeightedBackend{addr: "c", weight: 3},
	}

	result := selector.Select(backends)
	if result == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestSmoothWeightedRR_NilBackends(t *testing.T) {
	selector := NewSmoothWeightedRR()
	result := selector.Select(nil)
	if result != nil {
		t.Errorf("expected nil for nil backends")
	}
}

func TestSmoothWeightedRR_EmptyBackends(t *testing.T) {
	selector := NewSmoothWeightedRR()
	result := selector.Select([]Backend{})
	if result != nil {
		t.Errorf("expected nil for empty backends")
	}
}

func TestSmoothWeightedRR_Distribution(t *testing.T) {
	selector := NewSmoothWeightedRR()
	backends := []Backend{
		&simpleWeightedBackend{addr: "a", weight: 1},
		&simpleWeightedBackend{addr: "b", weight: 3},
	}
	picks := 10000

	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		b := selector.Select(backends)
		counts[b.Address()]++
	}

	if counts["a"] < 1500 || counts["a"] > 3500 {
		t.Errorf("expected ~2500 for 'a' (25%%), got %d", counts["a"])
	}
	if counts["b"] < 6500 || counts["b"] > 8500 {
		t.Errorf("expected ~7500 for 'b' (75%%), got %d", counts["b"])
	}
}
