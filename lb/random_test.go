package lb

import (
	"testing"
)

func TestRandom_Select(t *testing.T) {
	selector := NewRandom()
	backends := newTestBackends("a", "b", "c")

	result := selector.Select(backends)
	if result == nil {
		t.Fatal("expected non-nil backend")
	}
	if result.Address() == "" {
		t.Error("expected non-empty address")
	}
}

func TestRandom_NilBackends(t *testing.T) {
	selector := NewRandom()
	result := selector.Select(nil)
	if result != nil {
		t.Errorf("expected nil for nil backends, got '%s'", result.Address())
	}
}

func TestRandom_EmptyBackends(t *testing.T) {
	selector := NewRandom()
	result := selector.Select([]Backend{})
	if result != nil {
		t.Errorf("expected nil for empty backends, got '%s'", result.Address())
	}
}

func TestRandom_SingleBackend(t *testing.T) {
	selector := NewRandom()
	backends := newTestBackends("a")

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

func TestRandom_Distribution(t *testing.T) {
	selector := NewRandom()
	backends := newTestBackends("a", "b", "c")
	picks := 10000

	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		b := selector.Select(backends)
		counts[b.Address()]++
	}

	if counts["a"] < 2000 || counts["a"] > 5000 {
		t.Errorf("expected ~3333 each, got a=%d", counts["a"])
	}
	if counts["b"] < 2000 || counts["b"] > 5000 {
		t.Errorf("expected ~3333 each, got b=%d", counts["b"])
	}
	if counts["c"] < 2000 || counts["c"] > 5000 {
		t.Errorf("expected ~3333 each, got c=%d", counts["c"])
	}
}
