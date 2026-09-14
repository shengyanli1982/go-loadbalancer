package lb

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestURIHashOptions_ZeroValueMatchesNilDefault 钉住 URIHashOptions 的「零值 == 默认」契约：
// NewURIHash(nil) 与 NewURIHash(&URIHashOptions{}) 必须产出完全相同的选择结果。
//
// 历史缺陷（质量审查 P1-2）：旧实现用正向布尔字段表达该开关，语义落在 true 一侧，
// 于是 nil 分支显式置 true（包含 query），而零值 &URIHashOptions{} 落到 false（排除 query），
// 导致 Go 零值与 nil 语义相反，且 godoc 只描述了 nil 分支。
// 修复方式是反命名布尔字段为 ExcludeQuery，让零值天然等于默认行为，
// else 分支随之消失。本测试即该修复的行为级判据。
func TestURIHashOptions_ZeroValueMatchesNilDefault(t *testing.T) {
	backends := newTestBackends("a", "b", "c", "d", "e")
	keys := []string{
		"/api/users?page=1",
		"/api/users?page=2",
		"/api/orders?id=42",
		"/static/app.js?v=7",
		"/v1/search?q=golang",
	}

	t.Run("nil_and_zero_value_select_identically", func(t *testing.T) {
		nilSel := NewURIHash(nil)
		zeroSel := NewURIHash(&URIHashOptions{})
		for _, key := range keys {
			got := nilSel.SelectByHash(backends, []byte(key))
			want := zeroSel.SelectByHash(backends, []byte(key))
			require.NotNil(t, got, "key %q 的选择结果不应为 nil", key)
			require.NotNil(t, want, "key %q 的选择结果不应为 nil", key)
			assert.Equal(t, want.Address(), got.Address(),
				"key %q: NewURIHash(nil) 与 NewURIHash(&URIHashOptions{}) 必须选中同一后端（零值 == 默认）", key)
		}
	})

	t.Run("default_keeps_query_in_hash", func(t *testing.T) {
		sel := NewURIHash(nil)
		first := sel.SelectByHash(backends, []byte("/api/users?page=1"))
		second := sel.SelectByHash(backends, []byte("/api/users?page=2"))
		require.NotNil(t, first)
		require.NotNil(t, second)
		assert.NotEqual(t, first.Address(), second.Address(),
			"默认（包含 query）时，仅 query 不同的两个 key 应选中不同后端")
	})

	t.Run("exclude_query_truncates_at_question_mark", func(t *testing.T) {
		sel := NewURIHash(&URIHashOptions{ExcludeQuery: true})
		first := sel.SelectByHash(backends, []byte("/api/users?page=1"))
		second := sel.SelectByHash(backends, []byte("/api/users?page=2"))
		require.NotNil(t, first)
		require.NotNil(t, second)
		assert.Equal(t, first.Address(), second.Address(),
			"ExcludeQuery=true 时 query 被截断，仅 query 不同的两个 key 必须选中同一后端")
		assert.Equal(t, sel.SelectByHash(backends, []byte("/api/users")).Address(), first.Address(),
			"ExcludeQuery=true 时带 query 的 key 必须与纯路径 key 选中同一后端")
	})
}

// TestP2COptions_DecayGuard 白盒验证 P2COptions.Decay 的守卫逻辑：
// 仅开区间 (0, 1) 内的取值被采纳，nil / 零值 / 边界值 / 越界值一律静默回落到默认 0.9。
//
// 守卫条件见 lb/p2c.go 的 NewP2CWithOptions：opts != nil && opts.Decay > 0 && opts.Decay < 1。
// 这条「零值 == 默认」性质是 URIHashOptions 反命名修复所对齐的同包既有范式，
// 故在此钉住，防止后续重构把守卫放松成 >= 0 或 <= 1。
func TestP2COptions_DecayGuard(t *testing.T) {
	const defaultDecay = 0.9

	tests := []struct {
		name string
		opts *P2COptions
		want float64
	}{
		{name: "nil_opts_falls_back_to_default", opts: nil, want: defaultDecay},
		{name: "zero_value_falls_back_to_default", opts: &P2COptions{}, want: defaultDecay},
		{name: "explicit_zero_falls_back_to_default", opts: &P2COptions{Decay: 0}, want: defaultDecay},
		{name: "decay_one_rejected_by_upper_guard", opts: &P2COptions{Decay: 1}, want: defaultDecay},
		{name: "negative_decay_rejected", opts: &P2COptions{Decay: -0.5}, want: defaultDecay},
		{name: "decay_above_one_rejected", opts: &P2COptions{Decay: 1.5}, want: defaultDecay},
		{name: "decay_inside_open_interval_adopted", opts: &P2COptions{Decay: 0.5}, want: 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := NewP2CWithOptions(tt.opts)
			p, ok := sel.(*p2c)
			require.True(t, ok, "NewP2CWithOptions 应返回 *p2c，实际为 %T", sel)
			assert.Equal(t, tt.want, p.decay, "decay 守卫结果与预期不符")
		})
	}
}

// TestRandomWithSeed_Reproducible 验证种子化随机选择器的可复现性：
// 同一种子两次独立构造必须产出完全相同的选择序列，不同种子必须产出不同序列。
//
// 刻意不断言具体的后端名字序列：math/rand/v2 的 Rand.IntN 在 32 位与 64 位平台
// 走不同的位宽分支，写死名字会隐含 64 位平台假设。
// 本测试只断言「同种子同序列 / 异种子异序列」这两个平台无关的性质。
func TestRandomWithSeed_Reproducible(t *testing.T) {
	backends := newTestBackends("a", "b", "c", "d", "e")
	const steps = 20

	// sequence 采集指定种子下独立构造的选择器的选择序列
	sequence := func(seed int64) []string {
		sel := NewRandomWithSeed(seed)
		out := make([]string, 0, steps)
		for i := 0; i < steps; i++ {
			got := sel.Select(backends)
			require.NotNil(t, got, "第 %d 次选择结果不应为 nil", i)
			out = append(out, got.Address())
		}
		return out
	}

	t.Run("same_seed_yields_same_sequence", func(t *testing.T) {
		assert.Equal(t, sequence(1), sequence(1),
			"同一种子两次独立构造必须产出完全相同的选择序列")
	})

	t.Run("different_seed_yields_different_sequence", func(t *testing.T) {
		assert.NotEqual(t, sequence(1), sequence(2),
			"不同种子必须产出不同的选择序列")
	})
}

// TestARBOptions_BiasGuard 白盒验证 ARBOptions.Bias 的守卫逻辑：
// 仅半开区间 (0, 1] 内的取值被采纳，nil / 零值 / <= 0 / > 1 一律静默回落到默认 1.0。
//
// 守卫条件见 lb/arb.go 的 NewActiveRequestBiasWithOptions：
// opts != nil && opts.Bias > 0 && opts.Bias <= 1。
//
// 历史缺陷（第三轮评估 R3-A2）：旧实现只有下界守卫 Bias > 0，bias=5.0 被静默采纳，
// 违反 ARBOptions godoc、doc.go 与 README 三处「取值区间 (0, 1]」的文档承诺
// （实证：权重 10:1 双后端三次选择 [a b a]，而回落 1.0 应为 [a a a]）。
// 表格模式仿照同包 TestP2COptions_DecayGuard 的既有范式钉住区间边界，
// 防止后续重构把守卫放松回单侧或收紧成开区间 (0, 1)。
func TestARBOptions_BiasGuard(t *testing.T) {
	const defaultBias = 1.0

	tests := []struct {
		name string
		opts *ARBOptions
		want float64
	}{
		{name: "nil_opts_falls_back_to_default", opts: nil, want: defaultBias},
		{name: "zero_value_falls_back_to_default", opts: &ARBOptions{}, want: defaultBias},
		{name: "explicit_zero_falls_back_to_default", opts: &ARBOptions{Bias: 0}, want: defaultBias},
		{name: "negative_bias_rejected", opts: &ARBOptions{Bias: -0.5}, want: defaultBias},
		{name: "bias_above_one_rejected", opts: &ARBOptions{Bias: 5.0}, want: defaultBias},
		{name: "bias_one_accepted_as_inclusive_upper_bound", opts: &ARBOptions{Bias: 1.0}, want: 1.0},
		{name: "bias_inside_interval_adopted", opts: &ARBOptions{Bias: 0.5}, want: 0.5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := NewActiveRequestBiasWithOptions(tt.opts)
			a, ok := sel.(*activeRequestBias)
			require.True(t, ok, "NewActiveRequestBiasWithOptions 应返回 *activeRequestBias，实际为 %T", sel)
			assert.Equal(t, tt.want, a.bias, "bias 守卫结果与预期不符")
		})
	}
}

// TestMaglevOptions_TableSizeGuard 白盒验证 MaglevOptions.TableSize 的守卫逻辑：
// 仅闭区间 [2, maxMaglevTableSize]（1<<23）内的取值被采纳（非质数上修至最近质数），
// nil / 零值 / < 2 / 越上界一律静默回落到 DefaultMaglevTableSize。
//
// 历史缺陷（第三轮评估 R3-A3）：旧实现只有下界守卫 TableSize >= 2，超大取值直达
// nextPrime/isPrime——isPrime 的 i*i <= n 在 n 接近 MaxInt 时溢出为负，循环条件恒真，
// 构造函数永久挂起不返回（已实证：TableSize=math.MaxInt-1 逾 3s 不返回，
// 见 TestMaglevOptions_HugeTableSizeFallsBack）。上界 1<<23 同时把 isPrime 的
// 试除规模钉死在安全域内，越界输入按包约定（doc.go「非法配置一律回落默认值」）处理。
//
// 本表格同时钉住 Q4 构造语义：1024 非质数上修至 1031（README/doc.go 承诺）、
// nil/0/1 回落 DefaultMaglevTableSize，且所有路径产出的 tableSize 恒为质数。
// 表格模式仿照同包 TestP2COptions_DecayGuard 范式。
func TestMaglevOptions_TableSizeGuard(t *testing.T) {
	tests := []struct {
		name string
		opts *MaglevOptions
		want int
	}{
		{name: "nil_opts_falls_back_to_default", opts: nil, want: DefaultMaglevTableSize},
		{name: "zero_value_falls_back_to_default", opts: &MaglevOptions{}, want: DefaultMaglevTableSize},
		{name: "explicit_zero_below_lower_guard", opts: &MaglevOptions{TableSize: 0}, want: DefaultMaglevTableSize},
		{name: "table_size_one_below_lower_guard", opts: &MaglevOptions{TableSize: 1}, want: DefaultMaglevTableSize},
		{name: "negative_table_size_rejected", opts: &MaglevOptions{TableSize: -7}, want: DefaultMaglevTableSize},
		{name: "non_prime_1024_rounded_up_to_1031", opts: &MaglevOptions{TableSize: 1024}, want: 1031},
		// 上界为闭区间：maxMaglevTableSize（1<<23，偶数非质数）被采纳并上修至最近质数
		{name: "max_table_size_accepted_inclusive", opts: &MaglevOptions{TableSize: 1 << 23}, want: nextPrime(1 << 23)},
		// 越上界回落（修复前该取值会被采纳为 nextPrime(1<<23+1)，即本表格的 RED 用例）
		{name: "above_max_rejected", opts: &MaglevOptions{TableSize: (1 << 23) + 1}, want: DefaultMaglevTableSize},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := NewMaglev(tt.opts)
			m, ok := sel.(*maglev)
			require.True(t, ok, "NewMaglev 应返回 *maglev，实际为 %T", sel)
			assert.Equal(t, tt.want, m.tableSize, "tableSize 守卫结果与预期不符")
			assert.True(t, isPrime(m.tableSize), "tableSize 必须为质数（非质数输入应上修），实际 %d", m.tableSize)
		})
	}
}

// TestMaglevOptions_HugeTableSizeFallsBack 钉住 R3-A3 的挂起回归：
// TableSize=math.MaxInt-1 曾使 NewMaglev 陷入 isPrime 的 i*i 溢出死循环，
// 构造函数永久不返回（修复前实证：独立限界程序观测逾 3s 未返回）。
// 守卫落地后该输入越界回落 DefaultMaglevTableSize，构造函数必须立即返回。
//
// 注意：若未来有人移除上界守卫，本测试将挂起并触发 go test 超时 panic（CI 红），
// 这正是其作为回归护栏的存在形式。
func TestMaglevOptions_HugeTableSizeFallsBack(t *testing.T) {
	sel := NewMaglev(&MaglevOptions{TableSize: math.MaxInt - 1})
	m, ok := sel.(*maglev)
	require.True(t, ok, "NewMaglev 应返回 *maglev，实际为 %T", sel)
	assert.Equal(t, DefaultMaglevTableSize, m.tableSize,
		"越上界的 TableSize 必须回落 DefaultMaglevTableSize，而非进入 nextPrime 溢出死循环")
	assert.True(t, isPrime(m.tableSize), "tableSize 必须为质数")
}

// TestRingHashOptions_VirtualNodesGuard 白盒验证 RingHashOptions.VirtualNodes 的守卫逻辑：
// 仅闭区间 [1, maxVirtualNodes]（1<<20）内的取值被采纳，nil / 零值 / <= 0 / 越上界
// 一律静默回落到 DefaultVirtualNodes。
//
// 历史缺陷（第三轮评估 R3-A3）：旧实现只有下界守卫 VirtualNodes > 0，超大取值使
// buildRing 的 total := len(backends)*virtualNodes 溢出为负，首次 SelectByHash 时
// make 负容量 panic——崩溃点（运行期首次选择）与配置点（构造期）分离，极难排查。
// 上界 1<<20 把 total 钉死在 len(backends) × 1M 的安全规模内，越界按包约定回落默认值。
//
// 本表格同时钉住 Q4 构造语义：NewRingHash(nil) → DefaultVirtualNodes；
// 废弃字段 RingSize 被忽略、仅 VirtualNodes 生效（README 承诺）。
// 表格模式仿照同包 TestP2COptions_DecayGuard 范式。
func TestRingHashOptions_VirtualNodesGuard(t *testing.T) {
	tests := []struct {
		name string
		opts *RingHashOptions
		want int
	}{
		{name: "nil_opts_falls_back_to_default", opts: nil, want: DefaultVirtualNodes},
		{name: "zero_value_falls_back_to_default", opts: &RingHashOptions{}, want: DefaultVirtualNodes},
		{name: "explicit_zero_falls_back_to_default", opts: &RingHashOptions{VirtualNodes: 0}, want: DefaultVirtualNodes},
		{name: "negative_virtual_nodes_rejected", opts: &RingHashOptions{VirtualNodes: -3}, want: DefaultVirtualNodes},
		// 废弃字段 RingSize 被忽略，仅 VirtualNodes 生效（README「RingSize is ignored」承诺）
		{name: "deprecated_ring_size_ignored_virtual_nodes_adopted", opts: &RingHashOptions{RingSize: 1024, VirtualNodes: 50}, want: 50},
		// 上界为闭区间：maxVirtualNodes（1<<20）被采纳（仅断言字段，不触发建环）
		{name: "max_virtual_nodes_accepted_inclusive", opts: &RingHashOptions{VirtualNodes: 1 << 20}, want: 1 << 20},
		// 越上界回落（修复前该取值被原样采纳，即本表格的 RED 用例）
		{name: "above_max_rejected", opts: &RingHashOptions{VirtualNodes: (1 << 20) + 1}, want: DefaultVirtualNodes},
		// 溢出级取值回落（修复前构造成功但首次 SelectByHash panic，见下方 not-panic 子测试）
		{name: "huge_virtual_nodes_falls_back", opts: &RingHashOptions{VirtualNodes: math.MaxInt / 2}, want: DefaultVirtualNodes},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sel := NewRingHash(tt.opts)
			r, ok := sel.(*ringHash)
			require.True(t, ok, "NewRingHash 应返回 *ringHash，实际为 %T", sel)
			assert.Equal(t, tt.want, r.virtualNodes, "virtualNodes 守卫结果与预期不符")
		})
	}

	// 回归钉住「崩溃点与配置点分离」缺陷：修复前构造成功、首次 SelectByHash 时
	// buildRing 的 total = 3 × (MaxInt/2) 溢出为负 → make 负容量 panic。
	// 守卫落地后越界输入回落默认值，首次选择必须安全返回。
	t.Run("huge_virtual_nodes_select_by_hash_does_not_panic", func(t *testing.T) {
		sel := NewRingHash(&RingHashOptions{VirtualNodes: math.MaxInt / 2})
		r, ok := sel.(*ringHash)
		require.True(t, ok, "NewRingHash 应返回 *ringHash，实际为 %T", sel)
		require.Equal(t, DefaultVirtualNodes, r.virtualNodes, "越界 VirtualNodes 必须回落默认值")

		backends := newTestBackends("a", "b", "c")
		var got Backend
		assert.NotPanics(t, func() {
			got = sel.SelectByHash(backends, []byte("route-key"))
		}, "越界 VirtualNodes 回落后，首次 SelectByHash 不得 panic（buildRing total 溢出缺陷）")
		require.NotNil(t, got, "首次 SelectByHash 应返回非 nil 后端")
		assert.Contains(t, []string{"a", "b", "c"}, got.Address(), "选中后端必须来自候选集")
	})
}
