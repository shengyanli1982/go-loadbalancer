package lb

import (
	"testing"
)

func TestLeastConn_Select(t *testing.T) {
	selector := NewLeastConn()
	backends := newTestBackends("a", "b", "c")

	result := selector.Select(backends)
	if result == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestLeastConn_NilBackends(t *testing.T) {
	selector := NewLeastConn()
	result := selector.Select(nil)
	if result != nil {
		t.Errorf("expected nil for nil backends")
	}
}

func TestLeastConn_EmptyBackends(t *testing.T) {
	selector := NewLeastConn()
	result := selector.Select([]Backend{})
	if result != nil {
		t.Errorf("expected nil for empty backends")
	}
}

func TestLeastConn_SingleBackend(t *testing.T) {
	selector := NewLeastConn()
	backends := newTestBackends("a")

	for i := 0; i < 5; i++ {
		result := selector.Select(backends)
		if result == nil || result.Address() != "a" {
			t.Errorf("expected 'a', got '%s'", result.Address())
		}
	}
}

func TestP2C_Select(t *testing.T) {
	selector := NewP2C()
	backends := newTestBackends("a", "b", "c")

	result := selector.Select(backends)
	if result == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestP2C_NilBackends(t *testing.T) {
	selector := NewP2C()
	result := selector.Select(nil)
	if result != nil {
		t.Errorf("expected nil for nil backends")
	}
}

func TestP2C_SingleBackend(t *testing.T) {
	selector := NewP2C()
	backends := newTestBackends("a")

	for i := 0; i < 5; i++ {
		result := selector.Select(backends)
		if result == nil || result.Address() != "a" {
			t.Errorf("expected 'a', got '%s'", result.Address())
		}
	}
}
