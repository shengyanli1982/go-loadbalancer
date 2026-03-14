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
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
	)
	if err != nil {
		log.Fatalf("create balancer: %v", err)
	}
	defer func() {
		_ = lb.Close(context.Background())
	}()

	req := types.RequestContext{
		RequestID:  "req-basic-1",
		TenantID:   "team-a",
		SessionID:  "session-a",
		RouteClass: types.RouteGeneric,
		Model:      "model-a",
	}

	nodes := []types.NodeSnapshot{
		{
			NodeID:       "node-a",
			Healthy:      true,
			Inflight:     8,
			QueueDepth:   4,
			P95LatencyMs: 35,
			ErrorRate:    0.003,
			ModelAvailability: map[string]bool{
				"model-a": true,
			},
		},
		{
			NodeID:       "node-b",
			Healthy:      true,
			Inflight:     3,
			QueueDepth:   1,
			P95LatencyMs: 18,
			ErrorRate:    0.001,
			ModelAvailability: map[string]bool{
				"model-a": true,
			},
		},
	}

	chosen, err := lb.Route(context.Background(), req, nodes)
	if err != nil {
		log.Fatalf("route failed: %v", err)
	}
	fmt.Printf("chosen=%s score=%.2f reason=%v\n", chosen.Node.NodeID, chosen.Score, chosen.Reason)
}

