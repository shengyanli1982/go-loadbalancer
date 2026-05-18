package lb

import (
	"testing"
)

type testBackend struct {
	addr string
}

func (b *testBackend) Address() string {
	return b.addr
}

func newTestBackends(addrs ...string) []Backend {
	backends := make([]Backend, len(addrs))
	for i, addr := range addrs {
		backends[i] = &testBackend{addr: addr}
	}
	return backends
}

func TestRoundRobin_Select(t *testing.T) {
	selector := NewRoundRobin()

	result := selector.Select(newTestBackends("a", "b", "c"))
	if result == nil {
		t.Fatal("expected non-nil backend")
	}
	if result.Address() == "" {
		t.Error("expected non-empty address")
	}
}

func TestRoundRobin_SingleBackend(t *testing.T) {
	selector := NewRoundRobin()
	backends := newTestBackends("a")

	for i := 0; i < 5; i++ {
		result := selector.Select(backends)
		if result == nil {
			t.Fatal("expected non-nil backend")
		}
		if result.Address() != "a" {
			t.Errorf("expected 'a', got '%s'", result.Address())
		}
	}
}

func TestRoundRobin_NilBackends(t *testing.T) {
	selector := NewRoundRobin()
	result := selector.Select(nil)
	if result != nil {
		t.Errorf("expected nil for nil backends, got '%s'", result.Address())
	}
}

func TestRoundRobin_EmptyBackends(t *testing.T) {
	selector := NewRoundRobin()
	result := selector.Select([]Backend{})
	if result != nil {
		t.Errorf("expected nil for empty backends, got '%s'", result.Address())
	}
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

	if counts["a"] != 10 || counts["b"] != 10 || counts["c"] != 10 {
		t.Errorf("expected uniform distribution (10 each), got %v", counts)
	}
}
