package lb

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFastPathZeroAlloc 把 README 的头号卖点「Zero Allocation — all selectors
// allocate nothing on the fast path」变成可执行的 CI 门禁：对全部 14 个算法的
// 稳态热路径用 testing.AllocsPerRun 断言 == 0 allocs/op。
//
// 此前该性质只能靠人工跑 benchmark 维护（61 个 benchmark 中曾有 22 个连
// ReportAllocs 都没开），回归引入分配（如闭包意外逃逸、缓存未命中触发重建、
// 热路径误用 fmt/map）时无人报警；本测试失败即门禁拦截。
//
// 测量方法与同包 TestRCU_SteadyStateFastPathZeroAlloc 一致：
//   - 必须在闭包外先完成一次预热调用——首次 Select/SelectByHash 走冷路径
//     （rebuild 建缓存/建表/建环，有分配），不属于稳态语义；AllocsPerRun
//     内部的一次预热跑不足以覆盖带 fingerprint 缓存的慢路径；
//   - 预热后用同一个 backends slice 反复调用，命中 ptr+len 快速路径。
//
// RingHash/Maglev/Rendezvous 是 ConsistentHashSelector 双入口，
// Select（内部 randomKey8 派生随机键）与 SelectByHash（调用方给键）两条路径分别断言。
func TestFastPathZeroAlloc(t *testing.T) {
	const iterations = 1000

	// setup 完成 selector 构造 + 后端构造 + 预热，返回只走稳态热路径的闭包
	cases := []struct {
		name  string
		setup func(t *testing.T) func()
	}{
		{"round_robin", func(t *testing.T) func() {
			sel := NewRoundRobin()
			backends := generateBackends(8)
			require.NotNil(t, sel.Select(backends), "预热：建立缓存")
			return func() { sel.Select(backends) }
		}},
		{"random", func(t *testing.T) func() {
			sel := NewRandom()
			backends := generateBackends(8)
			require.NotNil(t, sel.Select(backends), "预热：建立缓存")
			return func() { sel.Select(backends) }
		}},
		{"weighted_rr", func(t *testing.T) func() {
			sel := NewWeightedRR()
			backends := generateWeightedBackends(8)
			require.NotNil(t, sel.Select(backends), "预热：建立累积权重缓存")
			return func() { sel.Select(backends) }
		}},
		{"smooth_weighted_rr", func(t *testing.T) func() {
			sel := NewSmoothWeightedRR()
			backends := generateWeightedBackends(8)
			require.NotNil(t, sel.Select(backends), "预热：建立 currentWeight/effectiveWeight 缓存")
			return func() { sel.Select(backends) }
		}},
		{"edf", func(t *testing.T) func() {
			sel := NewEDF()
			backends := generateWeightedBackends(8)
			require.NotNil(t, sel.Select(backends), "预热：建立 deadline 堆")
			return func() { sel.Select(backends) }
		}},
		{"least_conn", func(t *testing.T) func() {
			sel := NewLeastConn()
			backends := generateBackends(8)
			require.NotNil(t, sel.Select(backends), "预热：建立连接计数索引")
			return func() { sel.Select(backends) }
		}},
		{"p2c", func(t *testing.T) func() {
			sel := NewP2C()
			backends := generateBackends(8)
			require.NotNil(t, sel.Select(backends), "预热：建立负载快照")
			return func() { sel.Select(backends) }
		}},
		{"least_time", func(t *testing.T) func() {
			sel := NewLeastTime()
			configs := make([]ltConfig, 8)
			for i := range configs {
				configs[i] = ltConfig{
					addr:    fmt.Sprintf("svc-%d:80", i),
					weight:  (i % 3) + 1, // 非等权：覆盖加权评分分支
					latency: float64(i*5 + 1),
					conns:   i % 4,
				}
			}
			backends := newLatencyBackends(configs)
			require.NotNil(t, sel.Select(backends), "预热：建立 LatencyBackend 断言缓存")
			return func() { sel.Select(backends) }
		}},
		{"active_request_bias", func(t *testing.T) func() {
			sel := NewActiveRequestBias()
			backends := generateWeightedBackends(8)
			require.NotNil(t, sel.Select(backends), "预热：建立连接计数索引")
			return func() { sel.Select(backends) }
		}},
		{"ip_hash", func(t *testing.T) func() {
			sel := NewIPHash()
			backends := generateBackends(8)
			key := []byte("192.168.1.100")
			require.NotNil(t, sel.SelectByHash(backends, key), "预热")
			return func() { sel.SelectByHash(backends, key) }
		}},
		{"uri_hash", func(t *testing.T) func() {
			sel := NewURIHash(nil)
			backends := generateBackends(8)
			key := []byte("/api/users/123")
			require.NotNil(t, sel.SelectByHash(backends, key), "预热")
			return func() { sel.SelectByHash(backends, key) }
		}},
		// RCU 三算法：小表/小环配置与同包 rcuStressCases 先例一致，控制预热建表成本
		{"ring_hash_select_by_hash", func(t *testing.T) func() {
			sel := NewRingHash(&RingHashOptions{VirtualNodes: 16})
			backends := generateBackends(50)
			key := []byte("zero-alloc-key")
			require.NotNil(t, sel.SelectByHash(backends, key), "预热：建哈希环")
			return func() { sel.SelectByHash(backends, key) }
		}},
		{"ring_hash_select", func(t *testing.T) func() {
			sel := NewRingHash(&RingHashOptions{VirtualNodes: 16})
			backends := generateBackends(50)
			require.NotNil(t, sel.Select(backends), "预热：建哈希环")
			return func() { sel.Select(backends) }
		}},
		{"maglev_select_by_hash", func(t *testing.T) func() {
			sel := NewMaglev(&MaglevOptions{TableSize: 257})
			backends := generateBackends(50)
			key := []byte("zero-alloc-key")
			require.NotNil(t, sel.SelectByHash(backends, key), "预热：建查找表")
			return func() { sel.SelectByHash(backends, key) }
		}},
		{"maglev_select", func(t *testing.T) func() {
			sel := NewMaglev(&MaglevOptions{TableSize: 257})
			backends := generateBackends(50)
			require.NotNil(t, sel.Select(backends), "预热：建查找表")
			return func() { sel.Select(backends) }
		}},
		{"rendezvous_select_by_hash", func(t *testing.T) func() {
			sel := NewRendezvous()
			backends := generateBackends(50)
			key := []byte("zero-alloc-key")
			require.NotNil(t, sel.SelectByHash(backends, key), "预热：建地址哈希缓存")
			return func() { sel.SelectByHash(backends, key) }
		}},
		{"rendezvous_select", func(t *testing.T) func() {
			sel := NewRendezvous()
			backends := generateBackends(50)
			require.NotNil(t, sel.Select(backends), "预热：建地址哈希缓存")
			return func() { sel.Select(backends) }
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := tc.setup(t)
			allocs := testing.AllocsPerRun(iterations, f)
			assert.Equal(t, float64(0), allocs,
				"稳态热路径出现分配：%.1f allocs/op（README 承诺 fast path 零分配）", allocs)
		})
	}
}
