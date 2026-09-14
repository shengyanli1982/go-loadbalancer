package lb

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestLeastTime_MetricsSourceRefreshedOnSameContentNewSlice 钉住指标源重绑不变式：
// computeWeightedFingerprint 只编码 addr+weight，而 latencyBackends 缓存的是
// LatencyBackend 接口值（实例身份+行为）。调用方以同 addr+weight 重建实例并传入
// 新 slice（README Caller contract 明文支持）时指纹不变，若慢路径跳过指标源重绑，
// 选择决策将永久由已被丢弃的旧实例的冻结延迟/连接数驱动，违反
// 「三个评分输入全部来自 backend 对象自身」与 +Inf 哨兵语义。
func TestLeastTime_MetricsSourceRefreshedOnSameContentNewSlice(t *testing.T) {
	// 同 addr+weight、新实例（新延迟数据）—— 期望 leastTime 读取新实例的延迟
	t.Run("SameContentNewInstances", func(t *testing.T) {
		sel := NewLeastTime()

		// 第一代实例: a=10ms(优), b=20ms(劣) → 应选 a
		gen1 := newLatencyBackends([]ltConfig{
			{"a:80", 1, 10.0, 0},
			{"b:80", 1, 20.0, 0},
		})
		for i := 0; i < 3; i++ {
			got := sel.Select(gen1)
			require.Equal(t, "a:80", got.Address(), "gen1 sanity")
		}

		// 第二代实例（服务发现刷新/指标重置）：同 addr+weight，但延迟反转: a=999ms(劣), b=0.1ms(优)
		gen2 := newLatencyBackends([]ltConfig{
			{"a:80", 1, 999.0, 0},
			{"b:80", 1, 0.1, 0},
		})
		for i := 0; i < 10; i++ {
			got := sel.Select(gen2)
			require.Equal(t, "b:80", got.Address(),
				"STALE: gen2 select #%d 仍由 gen1 旧实例的冻结延迟驱动", i)
		}
	})

	// plain Backend 升级为 LatencyBackend（同 addr+weight=1）—— 期望新指标被采纳
	t.Run("PlainUpgradedToInstrumented", func(t *testing.T) {
		sel := NewLeastTime()

		gen1 := []Backend{NewBackend("a:80"), NewBackend("b:80")} // 全 plain → RR 轮转
		seen := map[string]int{}
		for i := 0; i < 10; i++ {
			seen[sel.Select(gen1).Address()]++
		}
		require.Positive(t, seen["a:80"], "gen1 sanity: plain backends should RR, got %v", seen)
		require.Positive(t, seen["b:80"], "gen1 sanity: plain backends should RR, got %v", seen)

		// gen2: b 升级为可观测且延迟极低；a 仍 plain（应 +Inf 恒败）→ 应 100% 选 b
		gen2 := []Backend{
			NewBackend("a:80"),
			&latencyBackend{address: "b:80", weight: 1, latency: 0.1},
		}
		for i := 0; i < 10; i++ {
			got := sel.Select(gen2)
			require.Equal(t, "b:80", got.Address(),
				"STALE: gen2 select #%d — latencyBackends 缓存仍为 [nil,nil]，未感知 b 已升级", i)
		}
	})

	// LatencyBackend 降级为 plain（同 addr+weight）—— 期望按文档取 +Inf 哨兵
	t.Run("InstrumentedDowngradedToPlain", func(t *testing.T) {
		sel := NewLeastTime()

		gen1 := newLatencyBackends([]ltConfig{
			{"a:80", 1, 1.0, 0}, // 优
			{"b:80", 1, 50.0, 0},
		})
		got := sel.Select(gen1)
		require.Equal(t, "a:80", got.Address(), "gen1 sanity")

		// gen2: a 降级为 plain（文档承诺: 无可观测数据 → +Inf 恒败）；b 仍可观测 → 应 100% 选 b
		gen2 := []Backend{
			NewBackend("a:80"),
			&latencyBackend{address: "b:80", weight: 1, latency: 50.0},
		}
		for i := 0; i < 10; i++ {
			got := sel.Select(gen2)
			require.Equal(t, "b:80", got.Address(),
				"STALE: gen2 select #%d — a 的旧实例缓存仍生效(latency=1.0)，违反 +Inf 哨兵语义", i)
		}
	})
}
