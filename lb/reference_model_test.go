package lb

import (
	"fmt"
	"math"
	"math/rand/v2"
	"testing"

	"github.com/stretchr/testify/require"
)

// ============================================================================
// P2-3：参考模型对拍（reference model differential testing）
//
// 背景：least_time.go 的 Select 有 4 条逐行同构的评分分支（hasUniformWeights ×
// allLatencyBackends），arb.go 有 3 条（等权+bias1 / bias1 / 通用），least_conn.go
// 有 2 条（等权 / 加权交叉乘法）。同构分支之间已经发生过语义漂移并被容忍
// （least_conn.go / arb.go 的 Select 注释自认「跨阈值轮转相位不保证一致，
// 已审计接受」）。本文件把「评分语义一致」变成可执行断言：
// 对每个算法写一个朴素、直白、按文档公式计算的参考模型，用固定播种的随机
// 配置驱动真实 Select 与模型逐步对拍。任何一条实现分支偏离文档公式，
// 对拍立即失败。
//
// 已知且被接受的偏差的排除方式（详见 assertRefMatch 注释）：
//   - 非平局（score 严格不等）：精确断言相等——此时平局相位无关，
//     线性路径（n<32）与堆路径（n>=32）必须给出同一答案；
//   - 平局：只断言「返回值 ∈ 参考平局集合」——线性路径用 rrIndex%tieLen
//     轮转、堆路径固定取最小 index，跨 32 阈值相位不一致是代码注释自认
//     「已审计接受」的偏差，参考模型无法同时匹配两种相位，
//     集合成员断言与相位无关，两种路径都必须满足。
//
// 浮点等价性论证（与 arb_heap_test.go 既有注释一致）：
//   - bias=1 的 ARB 与加权 LeastConn 在模型中用整数交叉乘法（精确），
//     测试规模下（w ≤ 5，conn ≤ 数百）任意两个不同分数的差 ≥ 1/(c1·c2) ~1e-5，
//     远大于 float64 在该量级的分辨率 ~1e-15，故与实现线性路径的浮点除法
//     序关系/相等关系等价；相等分数经 IEEE 正确舍入的除法得到逐位相同的结果；
//   - bias<1 与 LeastTime 的模型按与实现完全相同的表达式与运算次序计算
//     （同操作数 + 同次序 → 逐位一致 → == 平局判定可靠）。
// ============================================================================

// refModelSizes 后端数取值：必须跨越 TreeThresholdLeastConn/TreeThresholdARB = 32
// 阈值两侧——1/2 为退化小集合、5 典型线性路径、31/32/33 为阈值临界点、
// 64/100 为规模堆路径（LeastTime 无堆路径，但同一尺寸集覆盖其 4 条评分分支）。
var refModelSizes = []int{1, 2, 5, 31, 32, 33, 64, 100}

const (
	refModelSteps          = 200 // LeastConn/ARB 每配置的 Select/Release 交织步数（状态逐步演化）
	refModelLeastTimeSteps = 12  // LeastTime 指标静态（无内部计数），12 步覆盖平局轮转的多个相位
	refModelSeedBase       = 0x5EED
	refModelSeedStep       = 7 // 固定种子增量：跨运行可复现，且各配置随机流互不相关
)

// refWeightsUniform 参考模型侧的等权判定（独立于实现的 allWeightsEqual，
// 语义按文档：所有后端有效权重相等）。
func refWeightsUniform(weights []int) bool {
	for _, w := range weights[1:] {
		if w != weights[0] {
			return false
		}
	}
	return true
}

// refLeastConnTieSet 朴素 O(n) 扫描计算 LeastConn 参考胜出集合（升序）。
// 文档公式：等权 score = conn 取最小；加权 score = conn/weight 取最小，
// 用正权交叉乘法精确比较（conn_i/w_i < conn_b/w_b ⟺ conn_i·w_b < conn_b·w_i）。
func refLeastConnTieSet(conn, weights []int) []int {
	uniform := refWeightsUniform(weights)
	best := []int{0}
	for i := 1; i < len(conn); i++ {
		var better, equal bool
		if uniform {
			better, equal = conn[i] < conn[best[0]], conn[i] == conn[best[0]]
		} else {
			lhs := int64(conn[i]) * int64(weights[best[0]])
			rhs := int64(conn[best[0]]) * int64(weights[i])
			better, equal = lhs < rhs, lhs == rhs
		}
		if better {
			best = []int{i}
		} else if equal {
			best = append(best, i)
		}
	}
	return best
}

// refARBTieSet 朴素 O(n) 扫描计算 ARB 参考胜出集合（升序）。
// 文档公式：score = weight/(conn+1)^bias 取最大；
// bias=1 用整数交叉乘法 w_i·(c_b+1) vs w_b·(c_i+1)（精确，见文件头等价性论证），
// bias<1 用与实现 betterScore 完全相同的表达式 arbScore（同序浮点运算）。
func refARBTieSet(conn, weights []int, bias float64) []int {
	best := []int{0}
	for i := 1; i < len(conn); i++ {
		var better, equal bool
		if bias == 1.0 {
			lhs := int64(weights[i]) * int64(conn[best[0]]+1)
			rhs := int64(weights[best[0]]) * int64(conn[i]+1)
			better, equal = lhs > rhs, lhs == rhs
		} else {
			si := arbScore(weights[i], conn[i], bias)
			sb := arbScore(weights[best[0]], conn[best[0]], bias)
			better, equal = si > sb, si == sb
		}
		if better {
			best = []int{i}
		} else if equal {
			best = append(best, i)
		}
	}
	return best
}

// refLeastTimeTieSet 朴素 O(n) 扫描计算 LeastTime 参考胜出集合（升序）。
// 文档公式与哨兵语义（依据 latencyConns / sanitizeScore 的注释约定）：
//   - LatencyBackend：score = latency·(1+conns)（等权，文档「省去一次除法」）
//     或 latency·(1+conns)/weight（加权），运算次序与实现逐位一致；
//   - 非 LatencyBackend：延迟未知 → +Inf 惩罚哨兵（conns 视为 0），
//     仅在全集无可观测后端时入选（此时全员 +Inf 平局 → RR 退化）；
//   - 索引 0 的 NaN：bestScore 初始化时归一为 +Inf（sanitizeScore 语义，
//     防 NaN 在「取最小」比较域中垄断流量）；
//   - 索引 >0 的 NaN：NaN 与任何值的 < 与 == 均为 false → 不胜出、不平局，
//     从平局集合中排除（sanitizeScore 注释明确记录的既有良性行为）。
//
// 权重读取用 getWeight（即「有效权重」的规范来源：非加权/无效权重 → 1）。
func refLeastTimeTieSet(backends []Backend) []int {
	weights := make([]int, len(backends))
	for i, b := range backends {
		weights[i] = getWeight(b)
	}
	uniform := refWeightsUniform(weights)

	scoreOf := func(i int) float64 {
		lb, ok := backends[i].(LatencyBackend)
		if !ok {
			return math.Inf(1)
		}
		raw := lb.AverageLatency() * float64(1+lb.ActiveConnections())
		if !uniform {
			raw /= float64(weights[i])
		}
		return raw
	}

	best := []int{0}
	bestScore := scoreOf(0)
	if math.IsNaN(bestScore) {
		bestScore = math.Inf(1)
	}
	for i := 1; i < len(backends); i++ {
		s := scoreOf(i)
		if s < bestScore {
			bestScore = s
			best = []int{i}
		} else if s == bestScore {
			best = append(best, i)
		}
	}
	return best
}

// assertRefMatch 断言真实选择与参考模型一致（规则见文件头注释）：
// 唯一胜出 → 精确断言；平局 → 集合成员断言（对「已审计接受」的
// 跨阈值平局相位偏差免疫，同时仍能抓住任何越出平局集合的漂移）。
func assertRefMatch(t *testing.T, tieSet []int, gotIdx, step, n int) {
	t.Helper()
	if len(tieSet) == 1 {
		require.Equal(t, tieSet[0], gotIdx,
			"step %d (n=%d): 非平局选择偏离参考模型（同构评分分支发生语义漂移）", step, n)
	} else {
		require.Contains(t, tieSet, gotIdx,
			"step %d (n=%d): 平局选择必须落在参考平局集合 %v 内", step, n, tieSet)
	}
}

// refTrackedConfig 随机生成对拍配置：地址、权重（等权/不等权两模式）、
// 随机初始在途连接计数，并返回 addr→index 反查表。
func refTrackedConfig(rng *rand.Rand, n int, uniformWeights bool) (backends []Backend, weights, conn []int, addrToIdx map[string]int) {
	backends = make([]Backend, n)
	weights = make([]int, n)
	conn = make([]int, n)
	addrToIdx = make(map[string]int, n)
	for i := 0; i < n; i++ {
		addr := fmt.Sprintf("svc-%d:80", i)
		w := 3 // 等权模式统一取 3（非 1，同时覆盖 getWeight 的非平凡等权路径）
		if !uniformWeights {
			w = rng.IntN(5) + 1
		}
		weights[i] = w
		conn[i] = rng.IntN(8) // 随机初始在途连接计数
		backends[i] = NewWeightedBackend(addr, w)
		addrToIdx[addr] = i
	}
	return backends, weights, conn, addrToIdx
}

// refPrimeCounts 白盒注入随机初始计数：把 conn 写进 connByAddr，
// 首次 Select 的 rebuildIndex 会按地址回填 connByIndex（计数按地址跨 rebuild
// 持久化是文档化语义），等价于「selector 带着历史在途请求恢复」。
// 公开 API 无法构造任意初始计数（Select 只会产生算法自身形态的计数），
// 故走包内白盒——与同包既有测试的白盒先例一致（如 least_time_test.go 读
// sel.weightCache、rcu_snapshot_test.go 读 data.Load()）。
func refPrimeCounts(connByAddr map[string]int, backends []Backend, conn []int) {
	for i, b := range backends {
		connByAddr[b.Address()] = conn[i]
	}
}

// runLeastConnReference 单配置对拍：随机交织 Select/Release，逐步校验。
// 模型侧 conn[] 与实现侧 connByIndex 通过归纳保持相等（初始由注入保证，
// Select 使选中者 +1，Release 使 outstanding 中某项 −1）。
func runLeastConnReference(t *testing.T, seed uint64, n int, uniformWeights bool) {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))
	backends, weights, conn, addrToIdx := refTrackedConfig(rng, n, uniformWeights)

	sel := NewLeastConn().(*leastConn)
	refPrimeCounts(sel.connByAddr, backends, conn)

	outstanding := make([]int, 0, 64)
	for step := 0; step < refModelSteps; step++ {
		// 约 1/3 概率 Release（有未释放项时），其余 Select——与既有堆对拍骨架同构
		if len(outstanding) > 0 && rng.IntN(3) == 0 {
			k := rng.IntN(len(outstanding))
			idx := outstanding[k]
			outstanding = append(outstanding[:k], outstanding[k+1:]...)
			sel.Release(backends[idx])
			conn[idx]--
			continue
		}

		tieSet := refLeastConnTieSet(conn, weights)
		got := sel.Select(backends)
		require.NotNil(t, got)
		gotIdx := addrToIdx[got.Address()]
		assertRefMatch(t, tieSet, gotIdx, step, n)
		conn[gotIdx]++
		outstanding = append(outstanding, gotIdx)
	}
}

// runARBReference ARB 单配置对拍，结构同 runLeastConnReference，
// 参考公式换为 score = weight/(conn+1)^bias 取最大。
func runARBReference(t *testing.T, seed uint64, n int, uniformWeights bool, bias float64) {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))
	backends, weights, conn, addrToIdx := refTrackedConfig(rng, n, uniformWeights)

	sel := NewActiveRequestBiasWithOptions(&ARBOptions{Bias: bias}).(*activeRequestBias)
	refPrimeCounts(sel.connByAddr, backends, conn)

	outstanding := make([]int, 0, 64)
	for step := 0; step < refModelSteps; step++ {
		if len(outstanding) > 0 && rng.IntN(3) == 0 {
			k := rng.IntN(len(outstanding))
			idx := outstanding[k]
			outstanding = append(outstanding[:k], outstanding[k+1:]...)
			sel.Release(backends[idx])
			conn[idx]--
			continue
		}

		tieSet := refARBTieSet(conn, weights, bias)
		got := sel.Select(backends)
		require.NotNil(t, got)
		gotIdx := addrToIdx[got.Address()]
		assertRefMatch(t, tieSet, gotIdx, step, n)
		conn[gotIdx]++
		outstanding = append(outstanding, gotIdx)
	}
}

// runLeastTimeReference LeastTime 单配置对拍。
// LeastTime 不维护内部连接计数（conns 读自后端注入指标，Select 不递增），
// 配置静态 → 参考平局集合全程不变；多步循环覆盖 rrIndex 平局轮转的多个相位。
// 随机配置覆盖：等权/加权 × 全观测/混合（含非 LatencyBackend 的 +Inf 哨兵）×
// 10% 概率的 NaN 延迟（索引 0 归一 +Inf、索引 >0 排除平局两种规则均被随机命中）。
func runLeastTimeReference(t *testing.T, seed uint64, n int, uniformWeights, allLatency bool) {
	t.Helper()
	rng := rand.New(rand.NewPCG(seed, seed^0x9E3779B97F4A7C15))

	backends := make([]Backend, n)
	addrToIdx := make(map[string]int, n)
	for i := 0; i < n; i++ {
		addr := fmt.Sprintf("svc-%d:80", i)
		addrToIdx[addr] = i
		w := 1
		if !uniformWeights {
			w = rng.IntN(4) + 1
		}
		instrumented := allLatency || rng.IntN(4) > 0 // mixed 模式：~1/4 未观测（+Inf 哨兵）
		if !instrumented {
			if w == 1 {
				backends[i] = NewBackend(addr)
			} else {
				backends[i] = NewWeightedBackend(addr, w)
			}
			continue
		}
		latency := float64(rng.IntN(20)+1) * 0.5 // {0.5,1.0,…,10.0}：二进制精确表示
		if rng.IntN(10) == 0 {
			latency = math.NaN()
		}
		backends[i] = &latencyBackend{
			address: addr,
			weight:  w,
			latency: latency,
			conns:   rng.IntN(6),
		}
	}

	tieSet := refLeastTimeTieSet(backends)
	sel := NewLeastTime()
	for step := 0; step < refModelLeastTimeSteps; step++ {
		got := sel.Select(backends)
		require.NotNil(t, got)
		assertRefMatch(t, tieSet, addrToIdx[got.Address()], step, n)
	}
}

// TestReferenceModel_LeastConn 对拍 least_conn.go 的 2 条评分分支
// （等权 conn 比较 / 加权交叉乘法）×（n<32 线性 / n>=32 堆）共 4 种组合路径。
func TestReferenceModel_LeastConn(t *testing.T) {
	seed := uint64(refModelSeedBase)
	for _, n := range refModelSizes {
		for _, uniform := range []bool{true, false} {
			name := fmt.Sprintf("mixed_weights_n_%d", n)
			if uniform {
				name = fmt.Sprintf("uniform_weights_n_%d", n)
			}
			t.Run(name, func(t *testing.T) {
				runLeastConnReference(t, seed, n, uniform)
			})
			seed += refModelSeedStep
		}
	}
}

// TestReferenceModel_ARB 对拍 arb.go 的 3 条评分分支
// （等权+bias1 整数比较 / bias1 加权 / 0<bias<1 通用浮点）× 线性/堆路径。
func TestReferenceModel_ARB(t *testing.T) {
	seed := uint64(refModelSeedBase)
	for _, n := range refModelSizes {
		for _, bias := range []float64{1.0, 0.5} {
			for _, uniform := range []bool{true, false} {
				name := fmt.Sprintf("mixed_weights_bias_%v_n_%d", bias, n)
				if uniform {
					name = fmt.Sprintf("uniform_weights_bias_%v_n_%d", bias, n)
				}
				t.Run(name, func(t *testing.T) {
					runARBReference(t, seed, n, uniform, bias)
				})
				seed += refModelSeedStep
			}
		}
	}
}

// TestReferenceModel_LeastTime 对拍 least_time.go 的 4 条评分分支
// （hasUniformWeights × allLatencyBackends 的四种组合），
// 含 +Inf 哨兵与 NaN 归一/排除语义。
func TestReferenceModel_LeastTime(t *testing.T) {
	seed := uint64(refModelSeedBase)
	for _, n := range refModelSizes {
		for _, uniform := range []bool{true, false} {
			for _, allLatency := range []bool{true, false} {
				weightMode := "mixed_weights"
				if uniform {
					weightMode = "uniform_weights"
				}
				latencyMode := "mixed_latency"
				if allLatency {
					latencyMode = "all_latency"
				}
				name := fmt.Sprintf("%s_%s_n_%d", weightMode, latencyMode, n)
				t.Run(name, func(t *testing.T) {
					runLeastTimeReference(t, seed, n, uniform, allLatency)
				})
				seed += refModelSeedStep
			}
		}
	}
}
