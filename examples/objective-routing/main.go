package main

import (
	"context"
	"fmt"
	"log"

	"github.com/shengyanli1982/go-loadbalancer/balancer"
	"github.com/shengyanli1982/go-loadbalancer/config"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

func main() {
	lb, err := balancer.New(
		config.DefaultConfig(),
		config.WithAlgorithm(types.RouteLLMPrefill, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithObjective(config.ObjectiveWeighted, 3, true),
	)
	if err != nil {
		log.Fatalf("create balancer: %v", err)
	}
	defer func() {
		_ = lb.Close(context.Background())
	}()

	req := types.RequestContext{
		RequestID:  "req-llm-1",
		TenantID:   "team-llm",
		SessionID:  "session-llm",
		RouteClass: types.RouteLLMPrefill,
	}

	nodes := []types.NodeSnapshot{
		{
			NodeID:         "node-p1",
			Healthy:        true,
			Inflight:       4,
			QueueDepth:     3,
			P95LatencyMs:   35,
			ErrorRate:      0.004,
			TTFTms:         120,
			TPOTms:         16,
			KVCacheHitRate: 0.72,
		},
		{
			NodeID:         "node-p2",
			Healthy:        true,
			Inflight:       6,
			QueueDepth:     1,
			P95LatencyMs:   22,
			ErrorRate:      0.002,
			TTFTms:         90,
			TPOTms:         19,
			KVCacheHitRate: 0.81,
		},
	}

	chosen, err := lb.Route(context.Background(), req, nodes)
	if err != nil {
		log.Fatalf("route failed: %v", err)
	}
	fmt.Printf("chosen=%s score=%.2f reason=%v\n", chosen.Node.NodeID, chosen.Score, chosen.Reason)
}
