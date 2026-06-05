package lb

import (
	"testing"
)

func TestWeightedRR_Select(t *testing.T) {
	selector := NewWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 1),
		NewWeightedBackend("c", 1),
	}

	result := selector.Select(backends)
	if result == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestWeightedRR_NilBackends(t *testing.T) {
	selector := NewWeightedRR()
	result := selector.Select(nil)
	if result != nil {
		t.Errorf("expected nil for nil backends, got '%s'", result.Address())
	}
}

func TestWeightedRR_EmptyBackends(t *testing.T) {
	selector := NewWeightedRR()
	result := selector.Select([]Backend{})
	if result != nil {
		t.Errorf("expected nil for empty backends, got '%s'", result.Address())
	}
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

	if counts["a"] < 1500 || counts["a"] > 3500 {
		t.Errorf("expected ~2500 for 'a' (25%%), got %d", counts["a"])
	}
	if counts["b"] < 6500 || counts["b"] > 8500 {
		t.Errorf("expected ~7500 for 'b' (75%%), got %d", counts["b"])
	}
}

func TestWeightedRR_SingleBackend(t *testing.T) {
	selector := NewWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 5),
	}

	for i := 0; i < 10; i++ {
		result := selector.Select(backends)
		if result == nil {
			t.Fatal("expected non-nil backend")
		}
		if result.Address() != "a" {
			t.Errorf("expected 'a', got '%s'", result.Address())
		}
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

	if counts["a"] == 0 || counts["b"] == 0 {
		t.Errorf("expected both backends selected, got %v", counts)
	}
}
