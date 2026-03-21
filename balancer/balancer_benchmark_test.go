package balancer_test

import (
	"context"
	"errors"
	"sort"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shengyanli1982/go-loadbalancer/balancer"
	"github.com/shengyanli1982/go-loadbalancer/config"
	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const (
	benchmarkMetadataMaxInflightKey = "tenant_quota_max_inflight"
	benchmarkMetadataMaxQueueKey    = "tenant_quota_max_queue"
	benchmarkAlgorithmEmpty         = "bench_empty_candidates"
	benchmarkAlgorithmError         = "bench_error_candidates"
	benchmarkPolicyEmpty            = "bench_empty_policy"
	benchmarkObjectiveSleep         = "bench_sleep_objective"
)

type benchmarkEmptyCandidateAlgorithm struct{}

func (benchmarkEmptyCandidateAlgorithm) Name() string {
	return benchmarkAlgorithmEmpty
}

func (benchmarkEmptyCandidateAlgorithm) SelectCandidates(_ types.RequestContext, _ []types.NodeSnapshot, _ int) ([]types.Candidate, error) {
	return nil, nil
}

type benchmarkErrorCandidateAlgorithm struct{}

func (benchmarkErrorCandidateAlgorithm) Name() string {
	return benchmarkAlgorithmError
}

func (benchmarkErrorCandidateAlgorithm) SelectCandidates(_ types.RequestContext, _ []types.NodeSnapshot, _ int) ([]types.Candidate, error) {
	return nil, lberrors.ErrNoCandidate
}

type benchmarkSleepObjective struct{}

func (benchmarkSleepObjective) Name() string {
	return benchmarkObjectiveSleep
}

func (benchmarkSleepObjective) Choose(_ types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	time.Sleep(300 * time.Microsecond)
	if len(candidates) == 0 {
		return types.Candidate{}, lberrors.ErrNoCandidate
	}
	return candidates[0], nil
}

type benchmarkEmptyPolicy struct{}

func (benchmarkEmptyPolicy) Name() string {
	return benchmarkPolicyEmpty
}

func (benchmarkEmptyPolicy) ReRank(_ types.RequestContext, _ []types.Candidate) ([]types.Candidate, error) {
	return nil, nil
}

func init() {
	registry.MustRegisterAlgorithm(benchmarkEmptyCandidateAlgorithm{})
	registry.MustRegisterAlgorithm(benchmarkErrorCandidateAlgorithm{})
	registry.MustRegisterObjective(benchmarkSleepObjective{})
	registry.MustRegisterPolicy(benchmarkEmptyPolicy{})
}

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
	b.Run("serial_default_config_nodes_256", func(b *testing.B) {
		benchmarkRouteSerialDefaultConfig(b, 256)
	})
	b.Run("serial_objective_enabled_nodes_256", func(b *testing.B) {
		benchmarkRouteSerialObjectiveEnabled(b, 256)
	})
	b.Run("serial_fallback_policy_ranked_nodes_256", func(b *testing.B) {
		benchmarkRouteSerialFallbackPolicyRanked(b, 256)
	})
	b.Run("parallel_objective_guard_max_concurrent_1_nodes_256", func(b *testing.B) {
		benchmarkRouteParallelObjectiveGuard(b, 256, 1)
	})
	b.Run("parallel_objective_guard_max_concurrent_64_nodes_256", func(b *testing.B) {
		benchmarkRouteParallelObjectiveGuard(b, 256, 64)
	})
}

func BenchmarkRouteObjectiveGuardLatency(b *testing.B) {
	b.Run("max_concurrent_1_nodes_256", func(b *testing.B) {
		benchmarkRouteParallelObjectiveGuardWithLatency(b, 256, 1)
	})
	b.Run("max_concurrent_64_nodes_256", func(b *testing.B) {
		benchmarkRouteParallelObjectiveGuardWithLatency(b, 256, 64)
	})
}

func BenchmarkRouteFailurePaths(b *testing.B) {
	b.Run("serial_no_healthy_nodes", func(b *testing.B) {
		lb := benchmarkNewBalancer(b)
		req := benchmarkRouteRequest()
		nodes := []types.NodeSnapshot{{NodeID: "n0", Healthy: false}}
		benchmarkRouteSerialExpectError(b, lb, req, nodes, lberrors.ErrNoHealthyNodes)
	})
	b.Run("serial_empty_candidates", func(b *testing.B) {
		lb := benchmarkMustNewBalancer(
			b,
			config.WithAlgorithm(types.RouteGeneric, benchmarkAlgorithmEmpty),
			config.WithPolicies(),
			config.WithFallback(benchmarkAlgorithmEmpty),
		)
		req := benchmarkRouteRequest()
		nodes := benchmarkRouteNodes(256)
		benchmarkRouteSerialExpectError(b, lb, req, nodes, lberrors.ErrNoCandidate)
	})
	b.Run("serial_algorithm_error", func(b *testing.B) {
		lb := benchmarkMustNewBalancer(
			b,
			config.WithAlgorithm(types.RouteGeneric, benchmarkAlgorithmError),
			config.WithPolicies(),
			config.WithFallback(benchmarkAlgorithmError),
		)
		req := benchmarkRouteRequest()
		nodes := benchmarkRouteNodes(256)
		benchmarkRouteSerialExpectError(b, lb, req, nodes, lberrors.ErrNoCandidate)
	})
}

func benchmarkRouteSerial(b *testing.B, nodeCount int) {
	lb := benchmarkNewBalancer(b)
	nodes := benchmarkRouteNodes(nodeCount)
	req := benchmarkRouteRequest()
	benchmarkRouteSerialRun(b, lb, req, nodes)
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
	return benchmarkMustNewBalancer(
		b,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
	)
}

func benchmarkRouteSerialDefaultConfig(b *testing.B, nodeCount int) {
	lb := benchmarkMustNewBalancer(b)
	nodes := benchmarkRouteNodes(nodeCount)
	req := benchmarkRouteRequest()
	benchmarkRouteSerialRun(b, lb, req, nodes)
}

func benchmarkRouteSerialObjectiveEnabled(b *testing.B, nodeCount int) {
	lb := benchmarkMustNewBalancer(
		b,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithObjective(config.ObjectiveWeighted, 3, true),
	)
	nodes := benchmarkRouteNodes(nodeCount)
	req := benchmarkRouteRequest()
	benchmarkRouteSerialRun(b, lb, req, nodes)
}

func benchmarkRouteSerialFallbackPolicyRanked(b *testing.B, nodeCount int) {
	lb := benchmarkMustNewBalancer(
		b,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(benchmarkPolicyEmpty),
		config.WithFallback(config.FallbackPolicyRanked, config.AlgorithmLeastRequest),
	)
	nodes := benchmarkRouteNodes(nodeCount)
	req := benchmarkRouteRequest()
	benchmarkRouteSerialRun(b, lb, req, nodes)
}

func benchmarkRouteParallelObjectiveGuard(b *testing.B, nodeCount int, maxConcurrent int) {
	lb := benchmarkMustNewBalancer(
		b,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithObjective(benchmarkObjectiveSleep, 200, true),
		config.WithObjectiveMaxConcurrent(maxConcurrent),
	)
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

func benchmarkRouteParallelObjectiveGuardWithLatency(b *testing.B, nodeCount int, maxConcurrent int) {
	lb := benchmarkMustNewBalancer(
		b,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithObjective(benchmarkObjectiveSleep, 200, true),
		config.WithObjectiveMaxConcurrent(maxConcurrent),
	)
	nodes := benchmarkRouteNodes(nodeCount)
	req := benchmarkRouteRequest()
	ctx := context.Background()
	latencies := make([]int64, b.N)
	var idx atomic.Int64

	started := time.Now()
	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			routeStarted := time.Now()
			if _, err := lb.Route(ctx, req, nodes); err != nil {
				b.Fatal(err)
			}
			n := idx.Add(1) - 1
			if n >= int64(len(latencies)) {
				continue
			}
			latencies[n] = time.Since(routeStarted).Nanoseconds()
		}
	})
	b.StopTimer()

	total := int(idx.Load())
	if total <= 0 {
		return
	}

	sampled := latencies[:total]
	sort.Slice(sampled, func(i, j int) bool { return sampled[i] < sampled[j] })
	p95 := percentileNs(sampled, 95)
	p99 := percentileNs(sampled, 99)
	elapsed := time.Since(started)

	b.ReportMetric(float64(total)/elapsed.Seconds(), "req/s")
	b.ReportMetric(float64(p95)/1e6, "p95_ms")
	b.ReportMetric(float64(p99)/1e6, "p99_ms")
}

func benchmarkMustNewBalancer(b *testing.B, opts ...config.Option) balancer.Balancer {
	b.Helper()
	lb, err := balancer.New(
		config.DefaultConfig(),
		opts...,
	)
	if err != nil {
		b.Fatalf("create balancer: %v", err)
	}
	b.Cleanup(func() {
		_ = lb.Close(context.Background())
	})
	return lb
}

func benchmarkRouteSerialRun(b *testing.B, lb balancer.Balancer, req types.RequestContext, nodes []types.NodeSnapshot) {
	b.Helper()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := lb.Route(ctx, req, nodes); err != nil {
			b.Fatal(err)
		}
	}
}

func benchmarkRouteSerialExpectError(b *testing.B, lb balancer.Balancer, req types.RequestContext, nodes []types.NodeSnapshot, target error) {
	b.Helper()
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := lb.Route(ctx, req, nodes)
		if err == nil {
			b.Fatal("expect route error")
		}
		if target != nil && !errors.Is(err, target) {
			b.Fatalf("expect error %v, got %v", target, err)
		}
	}
}

func percentileNs(values []int64, percentile int) int64 {
	if len(values) == 0 {
		return 0
	}
	if percentile <= 0 {
		return values[0]
	}
	if percentile >= 100 {
		return values[len(values)-1]
	}
	rank := (len(values) - 1) * percentile / 100
	return values[rank]
}

func benchmarkRouteRequest() types.RequestContext {
	return types.RequestContext{
		RequestID:  "req-bench",
		SessionID:  "session-bench",
		TenantID:   "tenant-bench",
		RouteClass: types.RouteGeneric,
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
		})
	}
	return nodes
}

func benchmarkRouteHighLoadNodes(n int) []types.NodeSnapshot {
	nodes := make([]types.NodeSnapshot, 0, n)
	for i := 0; i < n; i++ {
		nodes = append(nodes, types.NodeSnapshot{
			NodeID:       "h" + strconv.Itoa(i),
			Healthy:      true,
			Inflight:     10 + (i % 50),
			QueueDepth:   10 + (i % 50),
			P95LatencyMs: float64((i % 80) + 20),
			ErrorRate:    float64((i%20)+1) / 1000.0,
		})
	}
	return nodes
}
