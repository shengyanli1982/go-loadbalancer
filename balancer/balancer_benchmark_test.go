package balancer_test

import (
	"context"
	"strconv"
	"testing"

	"github.com/shengyanli1982/go-loadbalancer/balancer"
	"github.com/shengyanli1982/go-loadbalancer/config"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

func BenchmarkRoute(b *testing.B) {
	b.Run("serial_nodes_32", func(b *testing.B) {
		benchmarkRouteSerial(b, 32)
	})
	b.Run("serial_nodes_256", func(b *testing.B) {
		benchmarkRouteSerial(b, 256)
	})
	b.Run("serial_nodes_1024", func(b *testing.B) {
		benchmarkRouteSerial(b, 1024)
	})
	b.Run("parallel_nodes_256", func(b *testing.B) {
		benchmarkRouteParallel(b, 256)
	})
}

func benchmarkRouteSerial(b *testing.B, nodeCount int) {
	lb := benchmarkNewBalancer(b)
	nodes := benchmarkRouteNodes(nodeCount)
	req := benchmarkRouteRequest()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := lb.Route(ctx, req, nodes); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRouteParallel(b *testing.B, nodeCount int) {
	lb := benchmarkNewBalancer(b)
	nodes := benchmarkRouteNodes(nodeCount)
	req := benchmarkRouteRequest()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := lb.Route(ctx, req, nodes); err != nil {
				b.Fatal(err)
			}
		}
	})
}

func benchmarkNewBalancer(b *testing.B) balancer.Balancer {
	b.Helper()
	lb, err := balancer.New(
		config.DefaultConfig(),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
	)
	if err != nil {
		b.Fatalf("create balancer: %v", err)
	}
	return lb
}

func benchmarkRouteRequest() types.RequestContext {
	return types.RequestContext{
		RequestID:  "req-bench",
		SessionID:  "session-bench",
		TenantID:   "tenant-bench",
		RouteClass: types.RouteGeneric,
		Model:      "model-a",
	}
}

func benchmarkRouteNodes(n int) []types.NodeSnapshot {
	nodes := make([]types.NodeSnapshot, 0, n)
	for i := 0; i < n; i++ {
		nodes = append(nodes, types.NodeSnapshot{
			NodeID:       "n" + strconv.Itoa(i),
			Healthy:      true,
			Inflight:     (i*31 + 17) % 200,
			QueueDepth:   (i*23 + 11) % 150,
			P95LatencyMs: float64((i*19)%120) + 1,
			ErrorRate:    float64((i*7)%20) / 1000.0,
			ModelAvailability: map[string]bool{
				"model-a": true,
			},
		})
	}
	return nodes
}
