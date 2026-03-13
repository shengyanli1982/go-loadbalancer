package weighted

import (
	"strconv"
	"testing"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// BenchmarkChoose 基准测试加权目标函数的择优性能。
func BenchmarkChoose(b *testing.B) {
	plugin := Plugin{}
	candidates := benchmarkCandidates(64)
	req := types.RequestContext{RouteClass: types.RouteLLMPrefill}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = plugin.Choose(req, candidates)
	}
}

// benchmarkCandidates 生成基准测试候选样本。
func benchmarkCandidates(n int) []types.Candidate {
	out := make([]types.Candidate, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, types.Candidate{
			Node: types.NodeSnapshot{
				NodeID:         "n" + strconv.Itoa(i),
				QueueDepth:     (i*13 + 3) % 200,
				P95LatencyMs:   float64((i*11)%150) + 1,
				ErrorRate:      float64((i*5)%30) / 1000.0,
				TTFTMs:         float64((i*7)%180) + 1,
				TPOTMs:         float64((i*17)%120) + 1,
				KVCacheHitRate: float64((i*19)%100) / 100.0,
			},
		})
	}
	return out
}
