package main

import (
	"context"
	"fmt"
	"log"

	"github.com/shengyanli1982/go-loadbalancer/balancer"
	"github.com/shengyanli1982/go-loadbalancer/config"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const (
	metadataMaxInflightKey = "tenant_quota_max_inflight"
	metadataMaxQueueKey    = "tenant_quota_max_queue"
)

func main() {
	lb, err := balancer.New(
		config.DefaultConfig(),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyTenantQuota),
		config.WithFallback(config.FallbackPolicyRanked, config.AlgorithmLeastRequest),
	)
	if err != nil {
		log.Fatalf("create balancer: %v", err)
	}
	defer func() {
		_ = lb.Close(context.Background())
	}()

	req := types.RequestContext{
		RequestID:  "req-fallback-1",
		TenantID:   "team-b",
		SessionID:  "session-b",
		RouteClass: types.RouteGeneric,
		Model:      "model-a",
		Metadata: map[string]string{
			metadataMaxInflightKey: "1",
			metadataMaxQueueKey:    "1",
		},
	}
	modelASet := types.NewModelCapabilitySet(map[string]bool{"model-a": true})

	nodes := []types.NodeSnapshot{
		{
			NodeID:          "node-c1",
			Healthy:         true,
			Inflight:        10,
			QueueDepth:      5,
			P95LatencyMs:    40,
			ErrorRate:       0.003,
			ModelCapability: modelASet,
		},
		{
			NodeID:          "node-c2",
			Healthy:         true,
			Inflight:        9,
			QueueDepth:      4,
			P95LatencyMs:    30,
			ErrorRate:       0.002,
			ModelCapability: modelASet,
		},
	}

	chosen, err := lb.Route(context.Background(), req, nodes)
	if err != nil {
		log.Fatalf("route failed: %v", err)
	}
	fmt.Printf("chosen=%s score=%.2f reason=%v\n", chosen.Node.NodeID, chosen.Score, chosen.Reason)
}
