package lb

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func isValidAddr(t *testing.T, addr string, addrs []string) bool {
	t.Helper()
	for _, a := range addrs {
		if addr == a {
			return true
		}
	}
	return false
}

func assertOnlyFrom(t *testing.T, result Backend, validAddrs []string) {
	t.Helper()
	if !assert.NotNil(t, result) {
		return
	}
	assert.True(t, isValidAddr(t, result.Address(), validAddrs),
		"backend address %q should be one of %v", result.Address(), validAddrs)
}

func TestDynamicBackends_RoundRobin_AddBackend(t *testing.T) {
	s := NewRoundRobin()
	backends1 := newTestBackends("a", "b", "c")
	for i := 0; i < 6; i++ {
		s.Select(backends1)
	}

	backends2 := newTestBackends("a", "b", "c", "d", "e")
	for i := 0; i < 20; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c", "d", "e"})
	}
}

func TestDynamicBackends_RoundRobin_RemoveBackend(t *testing.T) {
	s := NewRoundRobin()
	backends1 := newTestBackends("a", "b", "c", "d", "e")
	for i := 0; i < 10; i++ {
		s.Select(backends1)
	}

	backends2 := newTestBackends("a", "b")
	for i := 0; i < 20; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b"})
	}
}

func TestDynamicBackends_RoundRobin_ReplaceAll(t *testing.T) {
	s := NewRoundRobin()
	backends1 := newTestBackends("old1", "old2", "old3")
	for i := 0; i < 10; i++ {
		s.Select(backends1)
	}

	backends2 := newTestBackends("new1", "new2")
	for i := 0; i < 20; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"new1", "new2"})
	}
}

func TestDynamicBackends_Random_AddBackend(t *testing.T) {
	s := NewRandom()
	backends1 := newTestBackends("a", "b")
	for i := 0; i < 10; i++ {
		s.Select(backends1)
	}

	backends2 := newTestBackends("a", "b", "c", "d", "e")
	seen := map[string]bool{}
	for i := 0; i < 500; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c", "d", "e"})
		seen[r.Address()] = true
	}
	for _, addr := range []string{"a", "b", "c", "d", "e"} {
		assert.True(t, seen[addr], "expected %q to be selected after adding backend", addr)
	}
}

func TestDynamicBackends_Random_RemoveBackend(t *testing.T) {
	s := NewRandom()
	backends1 := newTestBackends("a", "b", "c", "d", "e")
	for i := 0; i < 10; i++ {
		s.Select(backends1)
	}

	backends2 := newTestBackends("x", "y")
	for i := 0; i < 50; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"x", "y"})
	}
}

func TestDynamicBackends_IPHash_AddBackend(t *testing.T) {
	s := NewIPHash()
	backends1 := newTestBackends("a", "b", "c")
	backends2 := newTestBackends("a", "b", "c", "d", "e")

	for i := 0; i < 20; i++ {
		r1 := s.SelectByHash(backends1, []byte("192.168.1.1"))
		assertOnlyFrom(t, r1, []string{"a", "b", "c"})

		r2 := s.SelectByHash(backends2, []byte("192.168.1.1"))
		assertOnlyFrom(t, r2, []string{"a", "b", "c", "d", "e"})
	}
}

func TestDynamicBackends_IPHash_RemoveBackend(t *testing.T) {
	s := NewIPHash()
	backends1 := newTestBackends("a", "b", "c", "d", "e")
	backends2 := newTestBackends("a", "b")

	for i := 0; i < 20; i++ {
		r1 := s.SelectByHash(backends1, []byte("10.0.0.1"))
		assertOnlyFrom(t, r1, []string{"a", "b", "c", "d", "e"})

		r2 := s.SelectByHash(backends2, []byte("10.0.0.1"))
		assertOnlyFrom(t, r2, []string{"a", "b"})
	}
}

func TestDynamicBackends_URIHash_AddBackend(t *testing.T) {
	s := NewURIHash(nil)
	backends1 := newTestBackends("a", "b", "c")
	backends2 := newTestBackends("a", "b", "c", "d", "e")

	for i := 0; i < 20; i++ {
		r1 := s.SelectByHash(backends1, []byte("/api/users"))
		assertOnlyFrom(t, r1, []string{"a", "b", "c"})

		r2 := s.SelectByHash(backends2, []byte("/api/users"))
		assertOnlyFrom(t, r2, []string{"a", "b", "c", "d", "e"})
	}
}

func TestDynamicBackends_URIHash_RemoveBackend(t *testing.T) {
	s := NewURIHash(nil)
	backends1 := newTestBackends("a", "b", "c", "d", "e")
	backends2 := newTestBackends("a", "b")

	for i := 0; i < 20; i++ {
		r1 := s.SelectByHash(backends1, []byte("/path"))
		assertOnlyFrom(t, r1, []string{"a", "b", "c", "d", "e"})

		r2 := s.SelectByHash(backends2, []byte("/path"))
		assertOnlyFrom(t, r2, []string{"a", "b"})
	}
}

func TestDynamicBackends_LeastConn_AddBackend(t *testing.T) {
	s := NewLeastConn()
	releaser, ok := s.(RequestReleaser)
	require.True(t, ok)

	backends1 := newTestBackends("a", "b", "c")
	for i := 0; i < 6; i++ {
		b := s.Select(backends1)
		releaser.Release(b)
	}

	backends2 := newTestBackends("a", "b", "c", "d", "e")
	counts := map[string]int{}
	for i := 0; i < 100; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c", "d", "e"})
		counts[r.Address()]++
		releaser.Release(r)
	}
	assert.True(t, counts["d"] > 0, "new backend 'd' should be selected")
	assert.True(t, counts["e"] > 0, "new backend 'e' should be selected")
}

func TestDynamicBackends_LeastConn_RemoveBackend(t *testing.T) {
	s := NewLeastConn()
	releaser, ok := s.(RequestReleaser)
	require.True(t, ok)

	backends1 := newTestBackends("a", "b", "c", "d", "e")
	for i := 0; i < 20; i++ {
		b := s.Select(backends1)
		releaser.Release(b)
	}

	backends2 := newTestBackends("a", "b")
	for i := 0; i < 50; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b"})
		releaser.Release(r)
	}
}

func TestDynamicBackends_LeastConn_ReplaceBackend(t *testing.T) {
	s := NewLeastConn()
	releaser, ok := s.(RequestReleaser)
	require.True(t, ok)

	backends1 := newTestBackends("old1", "old2", "old3")
	for i := 0; i < 10; i++ {
		b := s.Select(backends1)
		releaser.Release(b)
	}

	backends2 := newTestBackends("new1", "new2", "new3", "new4")
	for i := 0; i < 50; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"new1", "new2", "new3", "new4"})
		releaser.Release(r)
	}
}

func TestDynamicBackends_LeastConn_ConnectionTrackingPreserved(t *testing.T) {
	s := NewLeastConn()
	releaser, ok := s.(RequestReleaser)
	require.True(t, ok)

	backends1 := newTestBackends("a", "b", "c")
	var picked []Backend
	for i := 0; i < 20; i++ {
		b := s.Select(backends1)
		picked = append(picked, b)
	}

	backends2 := newTestBackends("a", "b", "c", "d")
	results := make([]Backend, 0, 100)
	for i := 0; i < 100; i++ {
		r := s.Select(backends2)
		results = append(results, r)
	}

	dCount := 0
	for _, r := range results {
		if r.Address() == "d" {
			dCount++
		}
	}
	// 确定性推导（LeastConn 线性路径无随机性，可用精确值断言）：
	// 前 20 次在 [a b c] 上选择且全程不 Release ⇒ 结束时 conn=[a:7, b:6, c:7]，合计 20。
	// 切到 [a b c d] 后 rebuildIndex 按**地址**迁移计数，d 为新后端 ⇒ conn[d]=0。
	// 再选 100 次 ⇒ 总在途 = 20 + 100 = 120。
	// LeastConn 每次只递增一个「当前最小」后端，故任意时刻 max-min ≤ 1；
	// 120 在 4 个后端上满足 spread ≤ 1 的整数解唯一 ⇒ conn=[30,30,30,30]。
	// d 从 0 起步且始终是唯一最小值直到追平 ⇒ 必须精确吸收 30 次；
	// 对应 a/b/c 分别补 30-7=23、30-6=24、30-7=23（23+24+23+30=100 ✓）。
	assert.Equal(t, 30, dCount,
		"new backend 'd' should absorb exactly 30/100 picks to reach the balanced level 30, got %d/100", dCount)

	for _, b := range picked {
		releaser.Release(b)
	}
	for _, b := range results {
		releaser.Release(b)
	}
}

func TestDynamicBackends_P2C_AddBackend(t *testing.T) {
	s := NewP2C()
	releaser, ok := s.(RequestReleaser)
	require.True(t, ok)

	backends1 := newTestBackends("a", "b")
	for i := 0; i < 10; i++ {
		b := s.Select(backends1)
		releaser.Release(b)
	}

	backends2 := newTestBackends("a", "b", "c", "d", "e")
	counts := map[string]int{}
	for i := 0; i < 200; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c", "d", "e"})
		counts[r.Address()]++
		releaser.Release(r)
	}
	assert.True(t, counts["c"] > 0, "new backend 'c' should be selected")
	assert.True(t, counts["d"] > 0, "new backend 'd' should be selected")
	assert.True(t, counts["e"] > 0, "new backend 'e' should be selected")
}

func TestDynamicBackends_P2C_RemoveBackend(t *testing.T) {
	s := NewP2C()
	releaser, ok := s.(RequestReleaser)
	require.True(t, ok)

	backends1 := newTestBackends("a", "b", "c", "d", "e")
	for i := 0; i < 50; i++ {
		b := s.Select(backends1)
		releaser.Release(b)
	}

	backends2 := newTestBackends("a", "b")
	for i := 0; i < 50; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b"})
		releaser.Release(r)
	}
}

func TestDynamicBackends_P2C_ReplaceAll(t *testing.T) {
	s := NewP2C()
	sel, ok := s.(Selector)
	require.True(t, ok)

	backends1 := newTestBackends("old1", "old2")
	for i := 0; i < 20; i++ {
		sel.Select(backends1)
	}

	backends2 := newTestBackends("new1", "new2", "new3")
	for i := 0; i < 50; i++ {
		r := sel.Select(backends2)
		assertOnlyFrom(t, r, []string{"new1", "new2", "new3"})
	}
}

func TestDynamicBackends_P2C_SingleBackend(t *testing.T) {
	s := NewP2C()

	backends1 := newTestBackends("a", "b", "c")
	for i := 0; i < 10; i++ {
		s.Select(backends1)
	}

	backends2 := newTestBackends("solo")
	for i := 0; i < 20; i++ {
		r := s.Select(backends2)
		require.NotNil(t, r)
		assert.Equal(t, "solo", r.Address())
	}
}

func TestDynamicBackends_RingHash_AddBackend(t *testing.T) {
	s := NewRingHash(nil)
	backends1 := newTestBackends("a", "b", "c")
	backends2 := newTestBackends("a", "b", "c", "d", "e")

	keys := make([][]byte, 200)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("key-%d", i))
	}

	beforeMapping := map[string]string{}
	for _, key := range keys {
		r := s.SelectByHash(backends1, key)
		require.NotNil(t, r)
		beforeMapping[string(key)] = r.Address()
	}

	unchanged := 0
	for _, key := range keys {
		r := s.SelectByHash(backends2, key)
		assertOnlyFrom(t, r, []string{"a", "b", "c", "d", "e"})
		if beforeMapping[string(key)] == r.Address() {
			unchanged++
		}
	}
	assert.Greater(t, unchanged, len(keys)/3,
		"at least 1/3 of keys should remain on same backend after adding backends")
}

func TestDynamicBackends_RingHash_RemoveBackend(t *testing.T) {
	s := NewRingHash(nil)
	backends1 := newTestBackends("a", "b", "c", "d", "e")
	backends2 := newTestBackends("a", "b", "c")

	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		r1 := s.SelectByHash(backends1, key)
		assertOnlyFrom(t, r1, []string{"a", "b", "c", "d", "e"})

		r2 := s.SelectByHash(backends2, key)
		assertOnlyFrom(t, r2, []string{"a", "b", "c"})
	}
}

func TestDynamicBackends_RingHash_RebuildCorrectness(t *testing.T) {
	s := NewRingHash(&RingHashOptions{VirtualNodes: 100})

	backends1 := newTestBackends("s1", "s2", "s3")
	key := []byte("test-key-rebuild")
	r1 := s.SelectByHash(backends1, key)
	require.NotNil(t, r1)
	addrBefore := r1.Address()

	backends2 := newTestBackends("s1", "s2", "s3", "s4", "s5")
	for i := 0; i < 10; i++ {
		r := s.SelectByHash(backends2, key)
		require.NotNil(t, r)
		assertOnlyFrom(t, r, []string{"s1", "s2", "s3", "s4", "s5"})
	}

	backends3 := newTestBackends("s1", "s2", "s3")
	for i := 0; i < 10; i++ {
		r := s.SelectByHash(backends3, key)
		require.NotNil(t, r)
		assert.Equal(t, addrBefore, r.Address(),
			"after removing newly added backends, same key should map back to original backend")
	}
}

func TestDynamicBackends_Maglev_AddBackend(t *testing.T) {
	s := NewMaglev(nil)
	backends1 := newTestBackends("a", "b", "c")
	backends2 := newTestBackends("a", "b", "c", "d", "e")

	keys := make([][]byte, 200)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("key-%d", i))
	}

	beforeMapping := map[string]string{}
	for _, key := range keys {
		r := s.SelectByHash(backends1, key)
		require.NotNil(t, r)
		beforeMapping[string(key)] = r.Address()
	}

	unchanged := 0
	for _, key := range keys {
		r := s.SelectByHash(backends2, key)
		assertOnlyFrom(t, r, []string{"a", "b", "c", "d", "e"})
		if beforeMapping[string(key)] == r.Address() {
			unchanged++
		}
	}
	assert.Greater(t, unchanged, len(keys)/3,
		"at least 1/3 of keys should remain on same backend after adding backends")
}

func TestDynamicBackends_Maglev_RemoveBackend(t *testing.T) {
	s := NewMaglev(nil)
	backends1 := newTestBackends("a", "b", "c", "d", "e")
	backends2 := newTestBackends("a", "b")

	for i := 0; i < 50; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		r1 := s.SelectByHash(backends1, key)
		assertOnlyFrom(t, r1, []string{"a", "b", "c", "d", "e"})

		r2 := s.SelectByHash(backends2, key)
		assertOnlyFrom(t, r2, []string{"a", "b"})
	}
}

func TestDynamicBackends_Maglev_RebuildCorrectness(t *testing.T) {
	s := NewMaglev(&MaglevOptions{TableSize: 1031})

	backends1 := newTestBackends("x", "y", "z")
	keys := [][]byte{[]byte("k1"), []byte("k2"), []byte("k3"), []byte("k4")}
	beforeMapping := map[string]string{}
	for _, key := range keys {
		r := s.SelectByHash(backends1, key)
		require.NotNil(t, r)
		beforeMapping[string(key)] = r.Address()
	}

	backends2 := newTestBackends("x", "y", "z", "w")
	for i := 0; i < 20; i++ {
		r := s.SelectByHash(backends2, []byte("k1"))
		assertOnlyFrom(t, r, []string{"x", "y", "z", "w"})
	}

	backends3 := newTestBackends("x", "y", "z")
	for _, key := range keys {
		r := s.SelectByHash(backends3, key)
		require.NotNil(t, r)
		assert.Equal(t, beforeMapping[string(key)], r.Address(),
			"after reverting backends to original, key mapping should be preserved")
	}
}

func TestDynamicBackends_WeightedRR_AddBackend(t *testing.T) {
	s := NewWeightedRR()
	backends1 := []Backend{
		NewWeightedBackend("a", 3),
		NewWeightedBackend("b", 1),
	}
	for i := 0; i < 8; i++ {
		s.Select(backends1)
	}

	backends2 := []Backend{
		NewWeightedBackend("a", 3),
		NewWeightedBackend("b", 1),
		NewWeightedBackend("c", 5),
		NewWeightedBackend("d", 1),
	}
	counts := map[string]int{}
	for i := 0; i < 200; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c", "d"})
		counts[r.Address()]++
	}
	assert.True(t, counts["c"] > 0, "new backend 'c' should be selected")
	assert.True(t, counts["d"] > 0, "new backend 'd' should be selected")
}

func TestDynamicBackends_WeightedRR_RemoveBackend(t *testing.T) {
	s := NewWeightedRR()
	backends1 := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 2),
		NewWeightedBackend("c", 3),
		NewWeightedBackend("d", 4),
	}
	for i := 0; i < 20; i++ {
		s.Select(backends1)
	}

	backends2 := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 2),
	}
	for i := 0; i < 50; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b"})
	}
}

func TestDynamicBackends_WeightedRR_ChangeWeight(t *testing.T) {
	s := NewWeightedRR()
	backends1 := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 1),
	}
	for i := 0; i < 10; i++ {
		s.Select(backends1)
	}

	backends2 := []Backend{
		NewWeightedBackend("a", 9),
		NewWeightedBackend("b", 1),
	}
	counts := map[string]int{}
	for i := 0; i < 200; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b"})
		counts[r.Address()]++
	}
	assert.True(t, counts["a"] > counts["b"]*5,
		"backend 'a' with weight 9 should far outpace 'b' with weight 1, got a=%d b=%d",
		counts["a"], counts["b"])
}

func TestDynamicBackends_SmoothWeightedRR_AddBackend(t *testing.T) {
	s := NewSmoothWeightedRR()
	backends1 := []Backend{
		NewWeightedBackend("a", 2),
		NewWeightedBackend("b", 1),
	}
	for i := 0; i < 10; i++ {
		s.Select(backends1)
	}

	backends2 := []Backend{
		NewWeightedBackend("a", 2),
		NewWeightedBackend("b", 1),
		NewWeightedBackend("c", 5),
	}
	counts := map[string]int{}
	for i := 0; i < 200; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c"})
		counts[r.Address()]++
	}
	assert.True(t, counts["c"] > 0, "new backend 'c' should be selected")
}

func TestDynamicBackends_SmoothWeightedRR_RemoveBackend(t *testing.T) {
	s := NewSmoothWeightedRR()
	backends1 := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 2),
		NewWeightedBackend("c", 3),
	}
	for i := 0; i < 20; i++ {
		s.Select(backends1)
	}

	backends2 := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 2),
	}
	for i := 0; i < 50; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b"})
	}
}

func TestDynamicBackends_SmoothWeightedRR_ChangeWeight(t *testing.T) {
	s := NewSmoothWeightedRR()
	backends1 := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 1),
	}
	for i := 0; i < 10; i++ {
		s.Select(backends1)
	}

	backends2 := []Backend{
		NewWeightedBackend("a", 10),
		NewWeightedBackend("b", 1),
	}
	counts := map[string]int{}
	for i := 0; i < 220; i++ {
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b"})
		counts[r.Address()]++
	}
	ratio := float64(counts["a"]) / float64(counts["b"])
	assert.Greater(t, ratio, 5.0,
		"weight ratio should reflect 10:1, got a=%d b=%d ratio=%.2f",
		counts["a"], counts["b"], ratio)
}

func TestDynamicBackends_SmoothWeightedRR_RebuildResetsState(t *testing.T) {
	s := NewSmoothWeightedRR()
	backends1 := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 5),
	}
	for i := 0; i < 50; i++ {
		s.Select(backends1)
	}

	backends2 := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 5),
		NewWeightedBackend("c", 3),
	}
	r := s.Select(backends2)
	require.NotNil(t, r)
	// rebuild resets currentWeight, so after rebuild the first few selections follow new weights
	assertOnlyFrom(t, r, []string{"a", "b", "c"})
}

func TestEDF_ReturnsCurrentBackend(t *testing.T) {
	s := NewEDF()
	b1 := NewWeightedBackend("a", 1)
	b2 := NewWeightedBackend("b", 1)
	backends := []Backend{b1, b2}
	s.Select(backends)

	b3 := NewWeightedBackend("a", 1)
	b4 := NewWeightedBackend("b", 1)
	backends2 := []Backend{b3, b4}
	for i := 0; i < 10; i++ {
		got := s.Select(backends2)
		if got != b3 && got != b4 {
			t.Fatalf("expected current backend, got stale cached object")
		}
	}
}

func TestSmoothWeightedRR_ReturnsCurrentBackend(t *testing.T) {
	s := NewSmoothWeightedRR()
	b1 := NewWeightedBackend("a", 1)
	b2 := NewWeightedBackend("b", 1)
	backends := []Backend{b1, b2}
	s.Select(backends)

	b3 := NewWeightedBackend("a", 1)
	b4 := NewWeightedBackend("b", 1)
	backends2 := []Backend{b3, b4}
	for i := 0; i < 10; i++ {
		got := s.Select(backends2)
		if got != b3 && got != b4 {
			t.Fatalf("expected current backend, got stale cached object")
		}
	}
}

func TestRingHash_ReturnsCurrentBackend(t *testing.T) {
	s := NewRingHash(nil)
	b1 := NewBackend("a")
	b2 := NewBackend("b")
	backends := []Backend{b1, b2}
	s.SelectByHash(backends, []byte("key1"))

	b3 := NewBackend("a")
	b4 := NewBackend("b")
	backends2 := []Backend{b3, b4}
	for i := 0; i < 20; i++ {
		got := s.SelectByHash(backends2, []byte("key1"))
		if got != b3 && got != b4 {
			t.Fatal("ring_hash returned stale cached backend")
		}
	}
}

func TestLeastTime_ReturnsCurrentBackend(t *testing.T) {
	sel := NewLeastTime()
	b1 := &latencyBackend{address: "a", weight: 1, latency: 10.0}
	b2 := &latencyBackend{address: "b", weight: 1, latency: 10.0}
	backends := []Backend{b1, b2}
	sel.Select(backends)

	b3 := &latencyBackend{address: "a", weight: 1, latency: 10.0}
	b4 := &latencyBackend{address: "b", weight: 1, latency: 10.0}
	backends2 := []Backend{b3, b4}
	for i := 0; i < 10; i++ {
		got := sel.Select(backends2)
		if got != b3 && got != b4 {
			t.Fatalf("expected current backend, got stale cached object")
		}
	}
}

func TestComputeBackendsFingerprint_NoAmbiguityOnConcat(t *testing.T) {
	b1 := []Backend{NewBackend("a"), NewBackend("bc")}
	b2 := []Backend{NewBackend("ab"), NewBackend("c")}
	fp1 := computeBackendsFingerprint(b1)
	fp2 := computeBackendsFingerprint(b2)
	if fp1 == fp2 {
		t.Fatalf("expected different fingerprints, both got %d", fp1)
	}
}

func TestRendezvous_AsSelector(t *testing.T) {
	s := NewRendezvous()
	backends := []Backend{NewBackend("a"), NewBackend("b"), NewBackend("c")}
	for i := 0; i < 100; i++ {
		got := s.Select(backends)
		if got == nil {
			t.Fatal("Select returned nil")
		}
		assertOnlyFrom(t, got, []string{"a", "b", "c"})
	}
}

func TestDynamicBackends_AllAlgorithms_SameContentDifferentSlice(t *testing.T) {
	backends1 := newTestBackends("a", "b", "c")
	backends2 := newTestBackends("a", "b", "c")

	t.Run("RoundRobin", func(t *testing.T) {
		s := NewRoundRobin()
		s.Select(backends1)
		s.Select(backends1)
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c"})
	})

	t.Run("Random", func(t *testing.T) {
		s := NewRandom()
		s.Select(backends1)
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c"})
	})

	t.Run("LeastConn", func(t *testing.T) {
		s := NewLeastConn()
		for i := 0; i < 5; i++ {
			s.Select(backends1)
		}
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c"})
	})

	t.Run("P2C", func(t *testing.T) {
		s := NewP2C()
		for i := 0; i < 5; i++ {
			s.Select(backends1)
		}
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c"})
	})

	t.Run("WeightedRR", func(t *testing.T) {
		s := NewWeightedRR()
		wb := []Backend{
			NewWeightedBackend("a", 1),
			NewWeightedBackend("b", 1),
			NewWeightedBackend("c", 1),
		}
		wb2 := []Backend{
			NewWeightedBackend("a", 1),
			NewWeightedBackend("b", 1),
			NewWeightedBackend("c", 1),
		}
		for i := 0; i < 10; i++ {
			s.Select(wb)
		}
		r := s.Select(wb2)
		assertOnlyFrom(t, r, []string{"a", "b", "c"})
	})

	t.Run("SmoothWeightedRR", func(t *testing.T) {
		s := NewSmoothWeightedRR()
		wb := []Backend{
			NewWeightedBackend("a", 1),
			NewWeightedBackend("b", 1),
			NewWeightedBackend("c", 1),
		}
		wb2 := []Backend{
			NewWeightedBackend("a", 1),
			NewWeightedBackend("b", 1),
			NewWeightedBackend("c", 1),
		}
		for i := 0; i < 10; i++ {
			s.Select(wb)
		}
		r := s.Select(wb2)
		assertOnlyFrom(t, r, []string{"a", "b", "c"})
	})

	t.Run("RingHash", func(t *testing.T) {
		s := NewRingHash(nil)
		key := []byte("test-key")
		r1 := s.SelectByHash(backends1, key)
		r2 := s.SelectByHash(backends2, key)
		assertOnlyFrom(t, r1, []string{"a", "b", "c"})
		assertOnlyFrom(t, r2, []string{"a", "b", "c"})
	})

	t.Run("Maglev", func(t *testing.T) {
		s := NewMaglev(nil)
		key := []byte("test-key")
		r1 := s.SelectByHash(backends1, key)
		r2 := s.SelectByHash(backends2, key)
		assertOnlyFrom(t, r1, []string{"a", "b", "c"})
		assertOnlyFrom(t, r2, []string{"a", "b", "c"})
	})

	t.Run("ARB", func(t *testing.T) {
		s := NewActiveRequestBias()
		releaser, ok := s.(RequestReleaser)
		require.True(t, ok)
		for i := 0; i < 5; i++ {
			b := s.Select(backends1)
			releaser.Release(b)
		}
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c"})
		releaser.Release(r)
	})

	t.Run("Rendezvous", func(t *testing.T) {
		s := NewRendezvous()
		key := []byte("test-key")
		r1 := s.SelectByHash(backends1, key)
		r2 := s.SelectByHash(backends2, key)
		assertOnlyFrom(t, r1, []string{"a", "b", "c"})
		assertOnlyFrom(t, r2, []string{"a", "b", "c"})
	})

	t.Run("EDF", func(t *testing.T) {
		s := NewEDF()
		for i := 0; i < 5; i++ {
			s.Select(backends1)
		}
		r := s.Select(backends2)
		assertOnlyFrom(t, r, []string{"a", "b", "c"})
	})

	t.Run("LeastTime", func(t *testing.T) {
		sel := NewLeastTime()
		// 两代实例同 addr+weight 但延迟反转：gen1 a 最低（应选 a），gen2 c 最低（应选 c）。
		// 若指标源未随 slice 更换而重绑到新实例，gen2 的决策仍由 gen1 冻结延迟驱动而错选 a。
		b1 := newLatencyBackends([]ltConfig{
			{"a", 1, 1.0, 0},
			{"b", 1, 50.0, 0},
			{"c", 1, 99.0, 0},
		})
		b2 := newLatencyBackends([]ltConfig{
			{"a", 1, 99.0, 0},
			{"b", 1, 50.0, 0},
			{"c", 1, 1.0, 0},
		})
		for i := 0; i < 5; i++ {
			assertOnlyFrom(t, sel.Select(b1), []string{"a"})
		}
		r := sel.Select(b2)
		assertOnlyFrom(t, r, []string{"c"})
	})
}

func TestDynamicBackends_AllAlgorithms_SizeFluctuation(t *testing.T) {
	t.Run("LeastConn", func(t *testing.T) {
		s := NewLeastConn()
		for size := 2; size <= 20; size += 3 {
			addrs := make([]string, size)
			for i := 0; i < size; i++ {
				addrs[i] = fmt.Sprintf("b%d", i)
			}
			backends := newTestBackends(addrs...)
			for i := 0; i < 30; i++ {
				r := s.Select(backends)
				assertOnlyFrom(t, r, addrs)
			}
		}
	})

	t.Run("P2C", func(t *testing.T) {
		s := NewP2C()
		for size := 2; size <= 20; size += 3 {
			addrs := make([]string, size)
			for i := 0; i < size; i++ {
				addrs[i] = fmt.Sprintf("b%d", i)
			}
			backends := newTestBackends(addrs...)
			for i := 0; i < 30; i++ {
				r := s.Select(backends)
				assertOnlyFrom(t, r, addrs)
			}
		}
	})

	t.Run("WeightedRR", func(t *testing.T) {
		s := NewWeightedRR()
		for size := 2; size <= 20; size += 3 {
			backends := make([]Backend, size)
			addrs := make([]string, size)
			for i := 0; i < size; i++ {
				addr := fmt.Sprintf("b%d", i)
				addrs[i] = addr
				backends[i] = NewWeightedBackend(addr, i+1)
			}
			for i := 0; i < 30; i++ {
				r := s.Select(backends)
				assertOnlyFrom(t, r, addrs)
			}
		}
	})

	t.Run("SmoothWeightedRR", func(t *testing.T) {
		s := NewSmoothWeightedRR()
		for size := 2; size <= 20; size += 3 {
			backends := make([]Backend, size)
			addrs := make([]string, size)
			for i := 0; i < size; i++ {
				addr := fmt.Sprintf("b%d", i)
				addrs[i] = addr
				backends[i] = NewWeightedBackend(addr, i+1)
			}
			for i := 0; i < 30; i++ {
				r := s.Select(backends)
				assertOnlyFrom(t, r, addrs)
			}
		}
	})

	t.Run("RingHash", func(t *testing.T) {
		s := NewRingHash(nil)
		key := []byte("test-key")
		for size := 2; size <= 20; size += 3 {
			addrs := make([]string, size)
			for i := 0; i < size; i++ {
				addrs[i] = fmt.Sprintf("b%d", i)
			}
			backends := newTestBackends(addrs...)
			for i := 0; i < 20; i++ {
				r := s.SelectByHash(backends, key)
				assertOnlyFrom(t, r, addrs)
			}
		}
	})

	t.Run("Maglev", func(t *testing.T) {
		s := NewMaglev(nil)
		key := []byte("test-key")
		for size := 2; size <= 20; size += 3 {
			addrs := make([]string, size)
			for i := 0; i < size; i++ {
				addrs[i] = fmt.Sprintf("b%d", i)
			}
			backends := newTestBackends(addrs...)
			for i := 0; i < 20; i++ {
				r := s.SelectByHash(backends, key)
				assertOnlyFrom(t, r, addrs)
			}
		}
	})

	t.Run("ARB", func(t *testing.T) {
		s := NewActiveRequestBias()
		releaser, ok := s.(RequestReleaser)
		require.True(t, ok)
		for size := 2; size <= 20; size += 3 {
			addrs := make([]string, size)
			for i := 0; i < size; i++ {
				addrs[i] = fmt.Sprintf("b%d", i)
			}
			backends := newTestBackends(addrs...)
			for i := 0; i < 30; i++ {
				r := s.Select(backends)
				assertOnlyFrom(t, r, addrs)
				releaser.Release(r)
			}
		}
	})

	t.Run("Rendezvous", func(t *testing.T) {
		s := NewRendezvous()
		key := []byte("test-key")
		for size := 2; size <= 20; size += 3 {
			addrs := make([]string, size)
			for i := 0; i < size; i++ {
				addrs[i] = fmt.Sprintf("b%d", i)
			}
			backends := make([]Backend, size)
			for i := 0; i < size; i++ {
				backends[i] = NewWeightedBackend(addrs[i], (i%5)+1)
			}
			for i := 0; i < 20; i++ {
				r := s.SelectByHash(backends, key)
				assertOnlyFrom(t, r, addrs)
			}
		}
	})

	t.Run("EDF", func(t *testing.T) {
		s := NewEDF()
		for size := 2; size <= 20; size += 3 {
			addrs := make([]string, size)
			backends := make([]Backend, size)
			for i := 0; i < size; i++ {
				addrs[i] = fmt.Sprintf("b%d", i)
				backends[i] = NewWeightedBackend(addrs[i], (i%5)+1)
			}
			for i := 0; i < 30; i++ {
				r := s.Select(backends)
				assertOnlyFrom(t, r, addrs)
			}
		}
	})

	t.Run("LeastTime", func(t *testing.T) {
		sel := NewLeastTime()
		for size := 2; size <= 20; size += 3 {
			addrs := make([]string, size)
			configs := make([]ltConfig, size)
			for i := 0; i < size; i++ {
				addr := fmt.Sprintf("svc-%d:80", i)
				addrs[i] = addr
				configs[i] = ltConfig{
					addr:    addr,
					weight:  (i % 5) + 1,
					latency: float64(i*5 + 1),
					conns:   i % 10,
				}
			}
			backends := newLatencyBackends(configs)
			for i := 0; i < 30; i++ {
				r := sel.Select(backends)
				assertOnlyFrom(t, r, addrs)
			}
		}
	})
}

func TestDynamicBackends_Rendezvous_AllScenarios(t *testing.T) {
	t.Run("AddBackend", func(t *testing.T) {
		s := NewRendezvous()
		b1 := []Backend{NewWeightedBackend("a", 1), NewWeightedBackend("b", 1)}
		b2 := []Backend{NewWeightedBackend("a", 1), NewWeightedBackend("b", 1), NewWeightedBackend("c", 1)}
		for i := 0; i < 30; i++ {
			key := []byte(fmt.Sprintf("key-%d", i))
			r := s.SelectByHash(b2, key)
			assertOnlyFrom(t, r, []string{"a", "b", "c"})
		}
		// 确保旧列表仍可用
		for i := 0; i < 30; i++ {
			key := []byte(fmt.Sprintf("key-old-%d", i))
			r := s.SelectByHash(b1, key)
			assertOnlyFrom(t, r, []string{"a", "b"})
		}
	})

	t.Run("RemoveBackend", func(t *testing.T) {
		s := NewRendezvous()
		b2 := []Backend{NewWeightedBackend("a", 1)}
		for i := 0; i < 50; i++ {
			key := []byte(fmt.Sprintf("key-%d", i))
			r := s.SelectByHash(b2, key)
			assertOnlyFrom(t, r, []string{"a"})
		}
	})

	t.Run("ReplaceAll", func(t *testing.T) {
		s := NewRendezvous()
		b1 := []Backend{NewWeightedBackend("old1", 1), NewWeightedBackend("old2", 1)}
		b2 := []Backend{NewWeightedBackend("new1", 1), NewWeightedBackend("new2", 1)}
		for i := 0; i < 50; i++ {
			key := []byte(fmt.Sprintf("key-%d", i))
			s.SelectByHash(b1, key)
			r := s.SelectByHash(b2, key)
			assertOnlyFrom(t, r, []string{"new1", "new2"})
		}
	})
}

func TestDynamicBackends_EDF_AllScenarios(t *testing.T) {
	t.Run("AddBackend", func(t *testing.T) {
		s := NewEDF()
		b1 := []Backend{NewWeightedBackend("a", 1), NewWeightedBackend("b", 1)}
		for i := 0; i < 10; i++ {
			s.Select(b1)
		}
		b2 := []Backend{NewWeightedBackend("a", 1), NewWeightedBackend("b", 1), NewWeightedBackend("c", 1)}
		counts := map[string]int{}
		for i := 0; i < 90; i++ {
			r := s.Select(b2)
			assertOnlyFrom(t, r, []string{"a", "b", "c"})
			counts[r.Address()]++
		}
		assert.True(t, counts["c"] > 0, "new backend should be selected")
	})

	t.Run("RemoveBackend", func(t *testing.T) {
		s := NewEDF()
		b1 := []Backend{NewWeightedBackend("a", 1), NewWeightedBackend("b", 1), NewWeightedBackend("c", 1)}
		for i := 0; i < 20; i++ {
			s.Select(b1)
		}
		b2 := []Backend{NewWeightedBackend("a", 1)}
		for i := 0; i < 30; i++ {
			r := s.Select(b2)
			require.NotNil(t, r)
			assert.Equal(t, "a", r.Address())
		}
	})

	t.Run("ReplaceAll", func(t *testing.T) {
		s := NewEDF()
		b1 := []Backend{NewWeightedBackend("x", 1)}
		for i := 0; i < 5; i++ {
			s.Select(b1)
		}
		b2 := []Backend{NewWeightedBackend("y", 1), NewWeightedBackend("z", 1)}
		for i := 0; i < 30; i++ {
			r := s.Select(b2)
			assertOnlyFrom(t, r, []string{"y", "z"})
		}
	})

	t.Run("ChangeWeight", func(t *testing.T) {
		s := NewEDF()
		b1 := []Backend{NewWeightedBackend("a", 1)}
		for i := 0; i < 5; i++ {
			s.Select(b1)
		}
		b2 := []Backend{NewWeightedBackend("a", 10), NewWeightedBackend("b", 1)}
		counts := map[string]int{}
		for i := 0; i < 220; i++ {
			r := s.Select(b2)
			counts[r.Address()]++
		}
		ratio := float64(counts["a"]) / float64(counts["b"])
		assert.Greater(t, ratio, 5.0, "weight 10:1 should yield ~10x ratio, got %.2f", ratio)
	})
}

func TestDynamicBackends_LeastTime_AllScenarios(t *testing.T) {
	makeLatBackends := func(addrs []string, weights []int, latencies []float64) []Backend {
		result := make([]Backend, len(addrs))
		for i := range addrs {
			w := 1
			if i < len(weights) {
				w = weights[i]
			}
			l := 10.0
			if i < len(latencies) {
				l = latencies[i]
			}
			result[i] = &latencyBackend{address: addrs[i], weight: w, latency: l, conns: 0}
		}
		return result
	}

	t.Run("AddBackend", func(t *testing.T) {
		sel := NewLeastTime()
		b1 := makeLatBackends([]string{"a:80", "b:80"}, []int{1, 1}, []float64{10, 20})
		for i := 0; i < 10; i++ {
			sel.Select(b1)
		}
		b2 := makeLatBackends([]string{"a:80", "b:80", "c:80"}, []int{1, 1, 2}, []float64{10, 20, 1})
		counts := map[string]int{}
		for i := 0; i < 30; i++ {
			r := sel.Select(b2)
			assertOnlyFrom(t, r, []string{"a:80", "b:80", "c:80"})
			counts[r.Address()]++
		}
		assert.True(t, counts["c:80"] > 0, "new low-latency backend should be selected")
	})

	t.Run("RemoveBackend", func(t *testing.T) {
		sel := NewLeastTime()
		b1 := makeLatBackends([]string{"a:80", "b:80", "c:80"}, nil, nil)
		for i := 0; i < 10; i++ {
			sel.Select(b1)
		}
		b2 := makeLatBackends([]string{"a:80"}, nil, nil)
		for i := 0; i < 10; i++ {
			r := sel.Select(b2)
			assertOnlyFrom(t, r, []string{"a:80"})
		}
	})

	t.Run("ReplaceAll", func(t *testing.T) {
		sel := NewLeastTime()
		b1 := makeLatBackends([]string{"old1:80", "old2:80"}, nil, nil)
		for i := 0; i < 10; i++ {
			sel.Select(b1)
		}
		b2 := makeLatBackends([]string{"new1:80", "new2:80"}, nil, nil)
		for i := 0; i < 10; i++ {
			r := sel.Select(b2)
			assertOnlyFrom(t, r, []string{"new1:80", "new2:80"})
		}
	})

	t.Run("ConcurrentChange", func(t *testing.T) {
		sel := NewLeastTime()
		b1 := makeLatBackends([]string{"a:80", "b:80", "c:80"}, []int{1, 2, 1}, []float64{10, 5, 20})
		b2 := makeLatBackends([]string{"d:80", "e:80"}, nil, nil)

		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				sel.Select(b1)
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 200; i++ {
				sel.Select(b2)
			}
		}()
		wg.Wait()
	})

	t.Run("SizeFluctuation", func(t *testing.T) {
		sel := NewLeastTime()
		for size := 2; size <= 20; size += 3 {
			addrs := make([]string, size)
			ws := make([]int, size)
			ls := make([]float64, size)
			for i := 0; i < size; i++ {
				addrs[i] = fmt.Sprintf("svc-%d:80", i)
				ws[i] = (i % 5) + 1
				ls[i] = float64(i*5 + 1)
			}
			backends := makeLatBackends(addrs, ws, ls)
			for i := 0; i < 30; i++ {
				r := sel.Select(backends)
				assertOnlyFrom(t, r, addrs)
			}
		}
	})
}

func TestDynamicBackends_ACTIVE_REQUEST_BIAS_AllScenarios(t *testing.T) {
	t.Run("AddBackend", func(t *testing.T) {
		s := NewActiveRequestBias()
		releaser, ok := s.(RequestReleaser)
		require.True(t, ok)
		b1 := newTestBackends("a", "b", "c")
		for i := 0; i < 6; i++ {
			b := s.Select(b1)
			releaser.Release(b)
		}
		b2 := newTestBackends("a", "b", "c", "d", "e")
		for i := 0; i < 50; i++ {
			r := s.Select(b2)
			assertOnlyFrom(t, r, []string{"a", "b", "c", "d", "e"})
			releaser.Release(r)
		}
	})

	t.Run("RemoveBackend", func(t *testing.T) {
		s := NewActiveRequestBias()
		releaser, ok := s.(RequestReleaser)
		require.True(t, ok)
		b1 := newTestBackends("a", "b", "c", "d", "e")
		for i := 0; i < 20; i++ {
			b := s.Select(b1)
			releaser.Release(b)
		}
		b2 := newTestBackends("a", "b")
		for i := 0; i < 30; i++ {
			r := s.Select(b2)
			assertOnlyFrom(t, r, []string{"a", "b"})
			releaser.Release(r)
		}
	})

	t.Run("ReplaceAll", func(t *testing.T) {
		s := NewActiveRequestBias()
		b1 := newTestBackends("old1", "old2", "old3")
		for i := 0; i < 10; i++ {
			s.Select(b1)
		}
		b2 := newTestBackends("new1", "new2")
		for i := 0; i < 30; i++ {
			r := s.Select(b2)
			assertOnlyFrom(t, r, []string{"new1", "new2"})
		}
	})
}
