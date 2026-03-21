package consistenthash

import (
	"strconv"
	"testing"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

func BenchmarkSelectCandidates(b *testing.B) {
	plugin := &Plugin{}
	req := types.RequestContext{SessionID: "session-a"}
	cases := []struct {
		name      string
		nodeCount int
		topK      int
	}{
		{name: "nodes_32_topk_1", nodeCount: 32, topK: 1},
		{name: "nodes_32_topk_8", nodeCount: 32, topK: 8},
		{name: "nodes_256_topk_8", nodeCount: 256, topK: 8},
		{name: "nodes_1024_topk_8", nodeCount: 1024, topK: 8},
		{name: "nodes_1024_topk_32", nodeCount: 1024, topK: 32},
	}

	for _, tc := range cases {
		nodes := benchmarkNodes(tc.nodeCount)
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = plugin.SelectCandidates(req, nodes, tc.topK)
			}
		})
	}
}

func benchmarkNodes(n int) []types.NodeSnapshot {
	nodes := make([]types.NodeSnapshot, 0, n)
	for i := 0; i < n; i++ {
		nodes = append(nodes, types.NodeSnapshot{
			NodeID:       "n" + strconv.Itoa(i),
			Healthy:      true,
			StaticWeight: (i % 10) + 1,
		})
	}
	return nodes
}
