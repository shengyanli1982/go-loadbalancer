package lb

import (
	"fmt"
	"math"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// latencyBackend 是测试用的 LatencyBackend 实现
type latencyBackend struct {
	address string
	weight  int
	latency float64 // AverageLatency
	conns   int     // ActiveConnections
}

func (b *latencyBackend) Address() string         { return b.address }
func (b *latencyBackend) Weight() int             { return b.weight }
func (b *latencyBackend) ActiveConnections() int  { return b.conns }
func (b *latencyBackend) AverageLatency() float64 { return b.latency }

// ltConfig 简化 newLatencyBackends 的配置
type ltConfig struct {
	addr    string
	weight  int
	latency float64
	conns   int
}

func newLatencyBackends(configs []ltConfig) []Backend {
	result := make([]Backend, len(configs))
	for i, c := range configs {
		result[i] = &latencyBackend{
			address: c.addr,
			weight:  c.weight,
			latency: c.latency,
			conns:   c.conns,
		}
	}
	return result
}

func TestLeastTime_EmptyBackends(t *testing.T) {
	sel := NewLeastTime()
	assert.Nil(t, sel.Select(nil))
	assert.Nil(t, sel.Select([]Backend{}), "expected nil for empty slice")
}

func TestLeastTime_SingleBackend(t *testing.T) {
	sel := NewLeastTime()
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
	})
	got := sel.Select(backends)
	require.NotNil(t, got)
	assert.Equal(t, "svc-a:80", got.Address())
}

func TestLeastTime_LowestScoreWins(t *testing.T) {
	// score = latency * (1 + conns) / weight
	// svc-a: 10 * (1+0) / 1 = 10
	// svc-b: 50 * (1+0) / 1 = 50  ← 更高
	// svc-c: 5  * (1+0) / 1 = 5   ← 最低，应被选中
	tests := []struct {
		name    string
		latency float64
		addr    string
	}{
		{"low latency wins", 5.0, "svc-c:80"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := NewLeastTime()
			backends := newLatencyBackends([]ltConfig{
				{"svc-a:80", 1, 10.0, 0},
				{"svc-b:80", 1, 50.0, 0},
				{"svc-c:80", 1, tt.latency, 0},
			})
			got := sel.Select(backends)
			assert.Equal(t, tt.addr, got.Address())
		})
	}
}

func TestLeastTime_WeightedDistribution(t *testing.T) {
	// score = latency * (1 + conns) / weight
	// score 相同时，权重越高分到越多请求
	// svc-a: 30 * (1+0) / 3 = 10
	// svc-b: 30 * (1+0) / 1 = 30
	// 第一轮 svc-a 胜（score=10 < 30）
	sel := NewLeastTime()
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 3, 30.0, 0}, // score=10
		{"svc-b:80", 1, 30.0, 0}, // score=30
	})
	counts := make(map[string]int)
	for i := 0; i < 4; i++ {
		b := sel.Select(backends)
		counts[b.Address()]++
	}
	// 权重3的后端应该获得更多请求
	assert.Greater(t, counts["svc-a:80"], 0, "high weight backend should get requests, got %+v", counts)
}

func TestLeastTime_ConnectionAware(t *testing.T) {
	// 连接数越多，score 越高
	// svc-a: 10 * (1+0) / 1 = 10   ← 无连接，应被选中
	// svc-b: 10 * (1+5) / 1 = 60   ← 5个连接，score更高
	sel := NewLeastTime()
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 10.0, 5},
	})
	got := sel.Select(backends)
	assert.Equal(t, "svc-a:80", got.Address(), "expected svc-a:80 (fewer connections)")
}

func TestLeastTime_NonLatencyBackendFallback(t *testing.T) {
	// 非 LatencyBackend 的后端延迟未知 → 惩罚哨兵使其恒败于任何已观测后端，
	// 仅在集合中不存在任何可观测后端时才作为兜底被选中（此时退化为 RR 轮转，
	// 见 TestLeastTime_AllBackendsUninstrumented_FallsBackToRR）。
	sel := NewLeastTime()
	backends := []Backend{
		NewBackend("plain-a:80"),                // 非 LatencyBackend → 延迟未知
		NewBackend("plain-b:80"),                // 非 LatencyBackend → 延迟未知
		&latencyBackend{"lat-c:80", 1, 10.0, 0}, // LatencyBackend → score=10
	}
	counts := make(map[string]int)
	for i := 0; i < 20; i++ {
		b := sel.Select(backends)
		counts[b.Address()]++
	}
	// 关键断言：存在可观测后端时，已观测后端获得全部流量，延迟未知后端不得抢占
	assert.Equal(t, 20, counts["lat-c:80"],
		"已观测后端应获得全部流量, got %+v", counts)
	assert.Zero(t, counts["plain-a:80"],
		"延迟未知后端不得抢占已观测后端的流量, got %+v", counts)
	assert.Zero(t, counts["plain-b:80"],
		"延迟未知后端不得抢占已观测后端的流量, got %+v", counts)
}

func TestLeastTime_FairTieBreaking(t *testing.T) {
	// 相同 score 的后端应轮流被选中（round-robin 平局处理）
	sel := NewLeastTime()
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 10.0, 0},
		{"svc-c:80", 1, 10.0, 0},
	})
	counts := make(map[string]int)
	rounds := 90
	for i := 0; i < rounds; i++ {
		b := sel.Select(backends)
		counts[b.Address()]++
	}
	// 三个后端应该分配相对均匀
	for addr, c := range counts {
		assert.InDelta(t, rounds/3, c, 5, "%s distribution should be ~%d", addr, rounds/3)
	}
}

func TestLeastTime_BackendChange(t *testing.T) {
	// 后端列表变化后应正确重建内部状态
	sel := NewLeastTime()
	backends1 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 10.0, 0},
	})
	sel.Select(backends1)
	sel.Select(backends1)

	// 移除 svc-a，添加 svc-c
	backends2 := newLatencyBackends([]ltConfig{
		{"svc-b:80", 1, 10.0, 0},
		{"svc-c:80", 1, 5.0, 0}, // 延迟更低
	})
	got := sel.Select(backends2)
	assert.Equal(t, "svc-c:80", got.Address(), "expected new low-latency backend svc-c:80")
}

func TestLeastTime_ConcurrentSafety(t *testing.T) {
	sel := NewLeastTime()
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 2, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
		{"svc-c:80", 3, 15.0, 2},
	})

	var wg sync.WaitGroup
	numGoroutines := 100
	numSelects := 100

	wg.Add(numGoroutines)
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < numSelects; j++ {
				b := sel.Select(backends)
				assert.NotNil(t, b, "got nil from concurrent Select")
			}
		}()
	}
	wg.Wait()
}

func TestLeastTime_ConcurrentBackendChange(t *testing.T) {
	sel := NewLeastTime()
	backends1 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
	})
	backends2 := newLatencyBackends([]ltConfig{
		{"svc-c:80", 1, 5.0, 0},
		{"svc-d:80", 2, 10.0, 0},
	})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			if i%2 == 0 {
				sel.Select(backends1)
			} else {
				sel.Select(backends2)
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			sel.Select(backends1)
		}
	}()
	wg.Wait()
}

func TestLeastTime_ZeroLatencyVsNonZero(t *testing.T) {
	// 真实测得的零延迟（score=0）仍是全域最优，且不等同于「延迟未知」：
	// 延迟未知后端拿惩罚哨兵，恒败于任何有限延迟（包括 0）的已观测后端。
	sel := NewLeastTime()
	backends := []Backend{
		NewBackend("plain:80"),                    // 延迟未知 → 惩罚哨兵
		&latencyBackend{"zero-lat:80", 1, 0.0, 0}, // score=0（真实零延迟）← 最低
		&latencyBackend{"hi-lat:80", 1, 100.0, 0}, // score=100
	}
	counts := make(map[string]int)
	for i := 0; i < 60; i++ {
		b := sel.Select(backends)
		counts[b.Address()]++
	}
	assert.Equal(t, 60, counts["zero-lat:80"],
		"真实零延迟后端应获得全部流量, got %+v", counts)
	assert.Zero(t, counts["plain:80"],
		"延迟未知后端不得抢占已观测后端的流量, got %+v", counts)
	assert.Zero(t, counts["hi-lat:80"],
		"高延迟后端不应获得流量, got %+v", counts)
}

func TestLeastTime_MixedBackends_PrefersInstrumented(t *testing.T) {
	// 混合后端类型（灰度迁移常见场景：部分后端已接延迟观测、部分未接）。
	// 缺陷形态：未实现 LatencyBackend 的后端曾得到 score=0，而 score=0 在「越小越好」的
	// 评分域中是全域最优值 → 无观测数据后端恒胜，已观测的低延迟后端被完全饿死。
	// 期望：延迟未知者恒败于任何有观测数据的后端。
	tests := []struct {
		name     string
		backends []Backend
		want     string
	}{
		{
			// 等权重路径（Select 的 hasUniformWeights && !allLatencyBackends 分支）
			name: "uniform_weights",
			backends: []Backend{
				&latencyBackend{address: "fast:80", weight: 1, latency: 1.0, conns: 0},
				NewBackend("plain:80"),
			},
			want: "fast:80",
		},
		{
			// 加权路径（Select 的 !hasUniformWeights && !allLatencyBackends 分支）：
			// 延迟未知后端即使权重高 3 倍也不得抢占流量
			name: "weighted_uninstrumented_has_higher_weight",
			backends: []Backend{
				&latencyBackend{address: "fast:80", weight: 1, latency: 1.0, conns: 0},
				NewWeightedBackend("plain:80", 3),
			},
			want: "fast:80",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := NewLeastTime()
			// LeastTime 是确定性算法（score 只读外部注入指标，无内部状态演化）→ 精确断言
			for i := 0; i < 10; i++ {
				got := sel.Select(tt.backends)
				require.NotNil(t, got)
				require.Equal(t, tt.want, got.Address(),
					"round %d: 已观测低延迟后端必须胜出，延迟未知后端不得垄断流量", i)
			}
		})
	}
}

func TestLeastTime_MixedBackends_HighLatencyInstrumentedStillLoses(t *testing.T) {
	// 关键反例：即使已观测后端延迟极差（1000ms），延迟未知后端仍须恒败。
	// 钉住「惩罚哨兵必须严格大于任何有限延迟」——防止哨兵被改成一个小常数后本组测试依然通过。
	// 延迟未知后端置于 index 0，覆盖 Select 中 latencyConns(0) 的初始 bestScore 赋值路径。
	tests := []struct {
		name     string
		backends []Backend
		want     string
	}{
		{
			name: "uniform_weights_uninstrumented_first",
			backends: []Backend{
				NewBackend("plain:80"),
				&latencyBackend{address: "slow:80", weight: 1, latency: 1000.0, conns: 0},
			},
			want: "slow:80",
		},
		{
			name: "weighted_uninstrumented_first",
			backends: []Backend{
				NewWeightedBackend("plain:80", 3),
				&latencyBackend{address: "slow:80", weight: 1, latency: 1000.0, conns: 0},
			},
			want: "slow:80",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := NewLeastTime()
			for i := 0; i < 10; i++ {
				got := sel.Select(tt.backends)
				require.NotNil(t, got)
				require.Equal(t, tt.want, got.Address(),
					"round %d: 延迟 1000ms 的已观测后端仍须优于延迟未知后端", i)
			}
		})
	}
}

func TestLeastTime_AllBackendsUninstrumented_FallsBackToRR(t *testing.T) {
	// 全部后端都未实现 LatencyBackend → 所有 score 同为惩罚哨兵 → 平局 →
	// 走既有 tiedIndices + rrIndex 轮转（等价于 RR）。
	// 这是修复前即存在的合理退化，本测试作为保留性回归钉子，修复前后都必须通过。
	sel := NewLeastTime()
	backends := []Backend{
		NewBackend("plain-a:80"),
		NewBackend("plain-b:80"),
		NewBackend("plain-c:80"),
	}
	// rrIndex 自 0 起、平局集合恒为 [0,1,2] → 选择序列确定，可精确断言
	want := []string{
		"plain-a:80", "plain-b:80", "plain-c:80",
		"plain-a:80", "plain-b:80", "plain-c:80",
	}
	for i, w := range want {
		got := sel.Select(backends)
		require.NotNil(t, got)
		require.Equal(t, w, got.Address(), "round %d: 全不可观测时应按 RR 顺序轮转", i)
	}
}

func TestLeastTime_AllUninstrumented_DifferentWeights_StillTiesToRR(t *testing.T) {
	// 关键反例：全部后端均未实现 LatencyBackend 且权重不等（3 vs 1）。
	// 加权路径 score = 哨兵 * (1+0) / weight：若哨兵为有限值（如 MaxFloat64），
	// 则 score 随权重缩放而互不相同 → 不平局 → 最高权重后端恒胜（行为增量）。
	// 哨兵取 +Inf 时，Inf / weight 对任意有限正权重仍为 +Inf → 平局 → RR 轮转。
	// 本测试在 MaxFloat64 哨兵下 FAIL（最高权重垄断），在 Inf(1) 哨兵下 PASS。
	sel := NewLeastTime()
	backends := []Backend{
		NewWeightedBackend("plain-hi:80", 3),
		NewWeightedBackend("plain-lo:80", 1),
	}
	const rounds = 100
	counts := map[string]int{}
	for i := 0; i < rounds; i++ {
		got := sel.Select(backends)
		require.NotNil(t, got)
		counts[got.Address()]++
	}
	// RR 轮转：两后端被选次数相等或相差 ≤1，而非最高权重恒胜
	diff := counts["plain-hi:80"] - counts["plain-lo:80"]
	if diff < 0 {
		diff = -diff
	}
	require.LessOrEqual(t, diff, 1,
		"全部不可观测且权重不等时仍应 RR 轮转（次数相差 ≤1），而非最高权重垄断；实测 hi=%d lo=%d",
		counts["plain-hi:80"], counts["plain-lo:80"])
}

func TestLeastTime_AllInstrumented_Unchanged(t *testing.T) {
	// 全部后端实现 LatencyBackend → allLatencyBackends 快速路径（不经过 latencyConns）→
	// 修复前后行为必须完全一致：最低 latency*(1+conns)/weight 者恒定胜出。防回归钉子。
	tests := []struct {
		name     string
		backends []Backend
		want     string
	}{
		{
			// 等权重快速路径：比较 latency*(1+conns)
			// svc-a: 10*(1+0)=10   svc-b: 4*(1+2)=12   svc-c: 6*(1+0)=6 ← 最低
			name: "uniform_weights_lowest_latency_times_conns_wins",
			backends: []Backend{
				&latencyBackend{address: "svc-a:80", weight: 1, latency: 10.0, conns: 0},
				&latencyBackend{address: "svc-b:80", weight: 1, latency: 4.0, conns: 2},
				&latencyBackend{address: "svc-c:80", weight: 1, latency: 6.0, conns: 0},
			},
			want: "svc-c:80",
		},
		{
			// 加权路径：score = latency*(1+conns)/weight
			// svc-a: 30*1/3=10 ← 最低   svc-b: 20*1/1=20   svc-c: 30*2/2=30
			name: "weighted_score_divides_by_weight",
			backends: []Backend{
				&latencyBackend{address: "svc-a:80", weight: 3, latency: 30.0, conns: 0},
				&latencyBackend{address: "svc-b:80", weight: 1, latency: 20.0, conns: 0},
				&latencyBackend{address: "svc-c:80", weight: 2, latency: 30.0, conns: 1},
			},
			want: "svc-a:80",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := NewLeastTime()
			for i := 0; i < 10; i++ {
				got := sel.Select(tt.backends)
				require.NotNil(t, got)
				require.Equal(t, tt.want, got.Address(), "round %d: 最低 score 者应恒定胜出", i)
			}
		})
	}
}

func TestLeastTime_SentinelScoreNeverNaNAndAlwaysLoses(t *testing.T) {
	// 直接断言惩罚哨兵经 Select 的两条评分公式运算后：
	//  1. 不产生 NaN（NaN 会使 < 与 == 同时为 false，导致平局轮转失效与选择乱序）；
	//  2. 严格大于任何有限延迟后端的 score（恒败于已观测后端）；
	//  3. 多个无数据后端之间 score 相等（平局）——即使权重不等，加权路径下 Inf/w 仍为 Inf。
	//     这正是 Inf 哨兵区别于有限哨兵（如 MaxFloat64）的关键：有限哨兵会被权重缩放而破坏平局。
	sel := NewLeastTime().(*leastTime)
	backends := []Backend{
		NewWeightedBackend("plain-hi:80", 3), // 无数据，权重 3
		NewWeightedBackend("plain-lo:80", 1), // 无数据，权重 1
		&latencyBackend{address: "slow:80", weight: 1, latency: 1000.0, conns: 0},
	}
	sel.Select(backends) // 触发 rebuildIndex，填充 latencyBackends/weightCache

	ltHi, lcHi := sel.latencyConns(0)
	ltLo, lcLo := sel.latencyConns(1)
	ltSlow, lcSlow := sel.latencyConns(2)
	require.False(t, math.IsNaN(ltHi), "哨兵延迟不得为 NaN")

	// 等权重路径公式：score = latency * (1+conns)
	uniformHi := ltHi * float64(1+lcHi)
	uniformLo := ltLo * float64(1+lcLo)
	uniformSlow := ltSlow * float64(1+lcSlow)
	// 加权路径公式：score = latency * (1+conns) / weight
	weightedHi := ltHi * float64(1+lcHi) / float64(sel.weightCache[0])
	weightedLo := ltLo * float64(1+lcLo) / float64(sel.weightCache[1])
	weightedSlow := ltSlow * float64(1+lcSlow) / float64(sel.weightCache[2])

	require.False(t, math.IsNaN(uniformHi), "等权重路径哨兵 score 不得为 NaN")
	require.False(t, math.IsNaN(weightedHi), "加权路径哨兵 score 不得为 NaN")

	// 恒败于已观测后端
	require.Greater(t, uniformHi, uniformSlow, "等权重路径：哨兵必须严格大于有限延迟后端的 score")
	require.Greater(t, weightedHi, weightedSlow, "加权路径：哨兵必须严格大于有限延迟后端的 score")

	// 无数据后端之间平局（即使权重 3 vs 1 不等，Inf/w 仍为 Inf）
	require.Equal(t, uniformHi, uniformLo, "等权重路径：两个无数据后端 score 必须相等（平局）")
	require.Equal(t, weightedHi, weightedLo, "加权路径：权重不等时 Inf/w 仍为 Inf，两个无数据后端 score 必须相等（平局）")
}

func TestLeastTime_NaNLatencyDoesNotMonopolize(t *testing.T) {
	// 潜在缺陷回归：AverageLatency() 返回 NaN 时（用户实现「0 个样本求均值」即 0/0 就会触发），
	// 在「取最小者胜」的比较域中 NaN 与任何值的 < 与 == 均为 false。
	// 若 backends[0] 的 bestScore 被污染为 NaN，则其后所有后端都无法胜出，
	// bestIdx 永远停在 0 → 该 NaN 后端 100% 垄断流量且永不自愈。
	// 修复：每个分支 bestScore 初始化处对 NaN 归一为 +Inf（与 latencyConns 的「延迟未知」哨兵一致）。
	t.Run("first_backend_nan_finite_wins_uniform", func(t *testing.T) {
		// 等权重 + 全 LatencyBackend 分支：backends[0] 延迟 NaN，其余有限。
		sel := NewLeastTime()
		backends := []Backend{
			&latencyBackend{address: "nan:80", weight: 1, latency: math.NaN(), conns: 0},
			&latencyBackend{address: "mid:80", weight: 1, latency: 100.0, conns: 0},
			&latencyBackend{address: "fast:80", weight: 1, latency: 50.0, conns: 0},
		}
		const rounds = 30
		counts := map[string]int{}
		for i := 0; i < rounds; i++ {
			got := sel.Select(backends)
			require.NotNil(t, got)
			counts[got.Address()]++
		}
		require.Equal(t, rounds, counts["fast:80"],
			"NaN 延迟后端不得垄断流量：最低有限延迟后端 fast:80 应获得全部流量, got %+v", counts)
		require.Zero(t, counts["nan:80"],
			"NaN 延迟后端不得获得任何流量, got %+v", counts)
		require.Zero(t, counts["mid:80"],
			"较高有限延迟后端 mid:80 不应胜出, got %+v", counts)
	})

	t.Run("first_backend_nan_finite_wins_weighted", func(t *testing.T) {
		// 加权 + 全 LatencyBackend 分支：backends[0] 延迟 NaN（权重低），
		// backends[1] 延迟 1000（权重高）。修复后即使有限后端延迟极差也应胜出。
		sel := NewLeastTime()
		backends := []Backend{
			&latencyBackend{address: "nan:80", weight: 1, latency: math.NaN(), conns: 0},
			&latencyBackend{address: "slow:80", weight: 3, latency: 1000.0, conns: 0},
		}
		const rounds = 30
		counts := map[string]int{}
		for i := 0; i < rounds; i++ {
			got := sel.Select(backends)
			require.NotNil(t, got)
			counts[got.Address()]++
		}
		require.Equal(t, rounds, counts["slow:80"],
			"NaN 延迟后端不得垄断流量：有限延迟后端 slow:80 应获得全部流量, got %+v", counts)
		require.Zero(t, counts["nan:80"],
			"NaN 延迟后端不得获得任何流量, got %+v", counts)
	})

	t.Run("middle_backend_nan_not_selected", func(t *testing.T) {
		// 中间位置 NaN：该后端永不入选（既有的良性退化），最低有限延迟后端胜出。
		sel := NewLeastTime()
		backends := []Backend{
			&latencyBackend{address: "a:80", weight: 1, latency: 10.0, conns: 0},
			&latencyBackend{address: "nan:80", weight: 1, latency: math.NaN(), conns: 0},
			&latencyBackend{address: "c:80", weight: 1, latency: 5.0, conns: 0},
		}
		const rounds = 30
		counts := map[string]int{}
		for i := 0; i < rounds; i++ {
			got := sel.Select(backends)
			require.NotNil(t, got)
			counts[got.Address()]++
		}
		require.Equal(t, rounds, counts["c:80"],
			"最低有限延迟后端 c:80 应获得全部流量, got %+v", counts)
		require.Zero(t, counts["nan:80"],
			"中间位置的 NaN 后端不得被选中, got %+v", counts)
	})

	t.Run("all_nan_degrades_to_first", func(t *testing.T) {
		// 全部 NaN：完全无信息时的退化，固定落到 backends[0]（钉住既有行为，防回归）。
		sel := NewLeastTime()
		backends := []Backend{
			&latencyBackend{address: "nan0:80", weight: 1, latency: math.NaN(), conns: 0},
			&latencyBackend{address: "nan1:80", weight: 1, latency: math.NaN(), conns: 0},
			&latencyBackend{address: "nan2:80", weight: 1, latency: math.NaN(), conns: 0},
		}
		const rounds = 30
		counts := map[string]int{}
		for i := 0; i < rounds; i++ {
			got := sel.Select(backends)
			require.NotNil(t, got)
			counts[got.Address()]++
		}
		require.Equal(t, rounds, counts["nan0:80"],
			"全部 NaN 时应退化为 backends[0], got %+v", counts)
		require.Zero(t, counts["nan1:80"], "全部 NaN 退化时不应选到 nan1, got %+v", counts)
		require.Zero(t, counts["nan2:80"], "全部 NaN 退化时不应选到 nan2, got %+v", counts)
	})
}

func TestLeastTime_DynamicBackends_AddBackend(t *testing.T) {
	sel := NewLeastTime()
	backends1 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
	})
	for i := 0; i < 10; i++ {
		sel.Select(backends1)
	}

	backends2 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
		{"svc-c:80", 2, 5.0, 0}, // 新后端：低延迟+高权重
	})
	counts := map[string]int{}
	for i := 0; i < 30; i++ {
		r := sel.Select(backends2)
		counts[r.Address()]++
	}
	assert.True(t, counts["svc-c:80"] > 0, "new low-latency backend should be selected")
}

func TestLeastTime_DynamicBackends_RemoveBackend(t *testing.T) {
	sel := NewLeastTime()
	backends1 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
		{"svc-c:80", 1, 30.0, 0},
	})
	for i := 0; i < 20; i++ {
		sel.Select(backends1)
	}

	backends2 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 20.0, 0},
	})
	for i := 0; i < 10; i++ {
		r := sel.Select(backends2)
		assertOnlyFrom(t, r, []string{"svc-a:80", "svc-b:80"})
	}
}

func TestLeastTime_DynamicBackends_ReplaceAll(t *testing.T) {
	sel := NewLeastTime()
	backends1 := newLatencyBackends([]ltConfig{
		{"old1:80", 1, 50.0, 0},
		{"old2:80", 1, 100.0, 0},
	})
	for i := 0; i < 10; i++ {
		sel.Select(backends1)
	}

	backends2 := newLatencyBackends([]ltConfig{
		{"new1:80", 1, 5.0, 0},
		{"new2:80", 1, 10.0, 0},
	})
	for i := 0; i < 10; i++ {
		r := sel.Select(backends2)
		assertOnlyFrom(t, r, []string{"new1:80", "new2:80"})
	}
}

func TestLeastTime_DynamicBackends_AddBackendFairShare(t *testing.T) {
	sel := NewLeastTime()

	backends1 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 10.0, 0},
	})
	// 先在旧集合上建立选择历史（触发 rebuildIndex 并缓存 LatencyBackend 断言）
	for i := 0; i < 20; i++ {
		sel.Select(backends1)
	}

	// 添加新后端
	backends2 := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0},
		{"svc-b:80", 1, 10.0, 0},
		{"svc-c:80", 1, 10.0, 0},
	})
	dCount := 0
	for i := 0; i < 60; i++ {
		r := sel.Select(backends2)
		if r.Address() == "svc-c:80" {
			dCount++
		}
	}
	// 三个后端外部指标恒同（weight=1/latency=10.0/conns=0）⇒ score 恒平局。
	// LeastTime 的 Select 从不递增内部计数，故 60 次选择全程平局：
	// tiedIndices=[0,1,2]、tieLen=3，bestIdx=tiedIndices[rrIndex%3] 后 rrIndex++，
	// 即严格 3 轮转。60 % 3 == 0 ⇒ 与 rrIndex 起始相位无关，每个后端恰好 20 次。
	// 用精确值而非宽区间：确定性算法的任何偏移都意味着平局轮转或计数迁移被破坏。
	assert.Equal(t, 20, dCount,
		"新增后端 svc-c 应在同分平局的 RR 轮转中精确获得 1/3 流量（60/3）, got %d/60", dCount)
}

func TestLeastTime_DynamicBackends_SizeFluctuation(t *testing.T) {
	sel := NewLeastTime()
	for size := 2; size <= 20; size += 3 {
		configs := make([]ltConfig, size)
		addrs := make([]string, size)
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
}

// BenchmarkLeastTime_Select_UniformWeights benchmarks the uniform weights fast path
func BenchmarkLeastTime_Select_UniformWeights(b *testing.B) {
	n := 10
	sel := NewLeastTime()
	configs := make([]ltConfig, n)
	for i := 0; i < n; i++ {
		configs[i] = ltConfig{
			addr:    fmt.Sprintf("svc-%d:80", i),
			weight:  1, // uniform weights
			latency: float64(i*5 + 1),
			conns:   i % 10,
		}
	}
	backends := newLatencyBackends(configs)
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			sel.Select(backends)
		}
	})
}
