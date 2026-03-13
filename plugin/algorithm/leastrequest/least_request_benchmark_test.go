package leastrequest

import (
	"strconv"
	"testing"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// BenchmarkSelectCandidates 基准测试 least_request 选点性能。
func BenchmarkSelectCandidates(b *testing.B) {
	plugin := Plugin{}
	nodes := benchmarkNodes(1024)
	req := types.RequestContext{}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = plugin.SelectCandidates(req, nodes, 8)
	}
}

// benchmarkNodes 生成基准测试节点样本。
func benchmarkNodes(n int) []types.NodeSnapshot {
	nodes := make([]types.NodeSnapshot, 0, n)
	for i := 0; i < n; i++ {
		nodes = append(nodes, types.NodeSnapshot{
			NodeID:       "n" + strconv.Itoa(i),
			Healthy:      true,
			Inflight:     (i*31 + 17) % 200,
			QueueDepth:   (i*23 + 11) % 150,
			P95LatencyMs: float64((i*19)%120) + 1,
			ErrorRate:    float64((i*7)%20) / 1000.0,
		})
	}
	return nodes
}
