package lb

import (
	"fmt"
	"testing"
)

func TestIPHash_SelectByHash(t *testing.T) {
	selector := NewIPHash()
	backends := newTestBackends("a", "b", "c")

	result := selector.SelectByHash(backends, []byte("192.168.1.1"))
	if result == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestIPHash_NilBackends(t *testing.T) {
	selector := NewIPHash()
	result := selector.SelectByHash(nil, []byte("192.168.1.1"))
	if result != nil {
		t.Errorf("expected nil for nil backends")
	}
}

func TestIPHash_SameIPSameBackend(t *testing.T) {
	selector := NewIPHash()
	backends := newTestBackends("a", "b", "c")

	result1 := selector.SelectByHash(backends, []byte("192.168.1.1"))
	result2 := selector.SelectByHash(backends, []byte("192.168.1.1"))

	if result1.Address() != result2.Address() {
		t.Errorf("same IP should return same backend")
	}
}

func TestURIHash_SelectByHash(t *testing.T) {
	selector := NewURIHash(nil)
	backends := newTestBackends("a", "b", "c")

	result := selector.SelectByHash(backends, []byte("/api/users"))
	if result == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestURIHash_SameURISameBackend(t *testing.T) {
	selector := NewURIHash(nil)
	backends := newTestBackends("a", "b", "c")

	result1 := selector.SelectByHash(backends, []byte("/api/users"))
	result2 := selector.SelectByHash(backends, []byte("/api/users"))

	if result1.Address() != result2.Address() {
		t.Errorf("same URI should return same backend")
	}
}

func TestRingHash_Select(t *testing.T) {
	selector := NewRingHash(nil)
	backends := newTestBackends("a", "b", "c")

	result := selector.Select(backends)
	if result == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestMaglev_Select(t *testing.T) {
	selector := NewMaglev(nil)
	backends := newTestBackends("a", "b", "c")

	result := selector.Select(backends)
	if result == nil {
		t.Fatal("expected non-nil backend")
	}
}

func TestRingHash_SameKeySameBackend(t *testing.T) {
	selector := NewRingHash(nil)
	backends := newTestBackends("a", "b", "c", "d", "e")
	key := []byte("test-key-12345")

	result1 := selector.SelectByHash(backends, key)
	result2 := selector.SelectByHash(backends, key)
	result3 := selector.SelectByHash(backends, key)

	if result1 == nil || result2 == nil || result3 == nil {
		t.Fatal("expected non-nil backend")
	}
	if result1.Address() != result2.Address() || result2.Address() != result3.Address() {
		t.Errorf("same key should return same backend: got %s, %s, %s",
			result1.Address(), result2.Address(), result3.Address())
	}
}

func TestRingHash_DifferentKeysDistribution(t *testing.T) {
	selector := NewRingHash(nil)
	backends := newTestBackends("a", "b", "c")

	picks := 10000
	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		result := selector.SelectByHash(backends, key)
		if result != nil {
			counts[result.Address()]++
		}
	}

	for _, addr := range []string{"a", "b", "c"} {
		if counts[addr] == 0 {
			t.Errorf("backend %s was never selected", addr)
		}
		if counts[addr] < 1000 || counts[addr] > 6000 {
			t.Errorf("backend %s has unusual distribution: %d", addr, counts[addr])
		}
	}
}

func TestRingHash_MinimalRemapping(t *testing.T) {
	selector := NewRingHash(nil)

	backends3 := newTestBackends("a", "b", "c")
	backends4 := newTestBackends("a", "b", "c", "d")

	keys := make([][]byte, 1000)
	for i := 0; i < 1000; i++ {
		keys[i] = []byte(fmt.Sprintf("key-%d", i))
	}

	mappingBefore := make(map[string]string)
	for _, key := range keys {
		result := selector.SelectByHash(backends3, key)
		if result != nil {
			mappingBefore[string(key)] = result.Address()
		}
	}

	unchanged := 0
	for _, key := range keys {
		result := selector.SelectByHash(backends4, key)
		if result != nil && mappingBefore[string(key)] == result.Address() {
			unchanged++
		}
	}

	remapRatio := float64(unchanged) / float64(1000)
	if remapRatio < 0.5 {
		t.Errorf("expected >50%% keys unchanged when adding backend, got %.2f%% (%d/1000)",
			remapRatio*100, unchanged)
	}
}

func TestRingHash_SelectByHashConsistency(t *testing.T) {
	selector := NewRingHash(&RingHashOptions{RingSize: 1024})
	backends := newTestBackends("server1", "server2", "server3")

	for trial := 0; trial < 100; trial++ {
		key := []byte(fmt.Sprintf("consistent-key-%d", trial))
		result1 := selector.SelectByHash(backends, key)
		result2 := selector.SelectByHash(backends, key)

		if result1 == nil || result2 == nil {
			t.Fatal("expected non-nil backend")
		}
		if result1.Address() != result2.Address() {
			t.Errorf("inconsistent result for same key: %s vs %s",
				result1.Address(), result2.Address())
		}
	}
}

func TestMaglev_SelectByHash(t *testing.T) {
	var selector HashSelector = NewMaglev(nil)
	backends := newTestBackends("a", "b", "c")

	result := selector.SelectByHash(backends, []byte("test-key"))
	if result == nil {
		t.Fatal("expected non-nil backend")
	}
	addr := result.Address()
	if addr != "a" && addr != "b" && addr != "c" {
		t.Errorf("expected one of a, b, c, got %s", addr)
	}
}

func TestMaglev_SameKeySameBackend(t *testing.T) {
	var selector HashSelector = NewMaglev(nil)
	backends := newTestBackends("a", "b", "c")
	key := []byte("maglev-key-12345")

	result1 := selector.SelectByHash(backends, key)
	result2 := selector.SelectByHash(backends, key)
	result3 := selector.SelectByHash(backends, key)

	if result1 == nil || result2 == nil || result3 == nil {
		t.Fatal("expected non-nil backend")
	}
	if result1.Address() != result2.Address() || result2.Address() != result3.Address() {
		t.Errorf("same key should return same backend: got %s, %s, %s",
			result1.Address(), result2.Address(), result3.Address())
	}
}

func TestMaglev_UniformDistribution(t *testing.T) {
	var selector HashSelector = NewMaglev(nil)
	backends := newTestBackends("a", "b", "c")

	picks := 10000
	counts := map[string]int{}
	for i := 0; i < picks; i++ {
		key := []byte(fmt.Sprintf("maglev-key-%d", i))
		result := selector.SelectByHash(backends, key)
		if result != nil {
			counts[result.Address()]++
		}
	}

	for _, addr := range []string{"a", "b", "c"} {
		if counts[addr] == 0 {
			t.Errorf("backend %s was never selected", addr)
		}
		if counts[addr] < 1500 || counts[addr] > 5000 {
			t.Errorf("backend %s has unusual distribution: %d", addr, counts[addr])
		}
	}
}

func TestMaglev_DifferentKeysSelectDifferentBackends(t *testing.T) {
	var selector HashSelector = NewMaglev(nil)
	backends := newTestBackends("a", "b", "c", "d", "e")

	selected := make(map[string]bool)
	for i := 0; i < 1000; i++ {
		key := []byte(fmt.Sprintf("maglev-key-%d", i))
		result := selector.SelectByHash(backends, key)
		if result != nil {
			selected[result.Address()] = true
		}
	}

	if len(selected) < 3 {
		t.Errorf("expected at least 3 different backends selected, got %d: %v",
			len(selected), selected)
	}
}
