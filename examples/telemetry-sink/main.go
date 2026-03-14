package main

import (
	"context"
	"fmt"
	"log"
	"sync"

	"github.com/shengyanli1982/go-loadbalancer/balancer"
	"github.com/shengyanli1982/go-loadbalancer/config"
	"github.com/shengyanli1982/go-loadbalancer/telemetry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

type printSink struct {
	mu sync.Mutex
}

func (s *printSink) OnEvent(e telemetry.TelemetryEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Printf("[telemetry] type=%s stage=%s outcome=%s plugin=%s reason=%s duration_ms=%d\n",
		e.Type, e.Stage, e.Outcome, e.Plugin, e.Reason, e.DurationMs)
}

func main() {
	sink := &printSink{}
	lb, err := balancer.New(
		config.DefaultConfig(),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithObjective(config.ObjectiveWeighted, 3, true),
		config.WithTelemetrySink(sink),
	)
	if err != nil {
		log.Fatalf("create balancer: %v", err)
	}
	defer func() {
		_ = lb.Close(context.Background())
	}()

	req := types.RequestContext{
		RequestID:  "req-tel-1",
		TenantID:   "team-observe",
		SessionID:  "session-observe",
		RouteClass: types.RouteGeneric,
		Model:      "model-a",
	}
	modelASet := types.NewModelCapabilitySet(map[string]bool{"model-a": true})

	nodes := []types.NodeSnapshot{
		{
			NodeID:          "node-d1",
			Healthy:         true,
			Inflight:        5,
			QueueDepth:      2,
			P95LatencyMs:    20,
			ErrorRate:       0.001,
			ModelCapability: modelASet,
		},
		{
			NodeID:          "node-d2",
			Healthy:         true,
			Inflight:        8,
			QueueDepth:      3,
			P95LatencyMs:    26,
			ErrorRate:       0.004,
			ModelCapability: modelASet,
		},
	}

	chosen, err := lb.Route(context.Background(), req, nodes)
	if err != nil {
		log.Fatalf("route failed: %v", err)
	}
	fmt.Printf("chosen=%s score=%.2f reason=%v\n", chosen.Node.NodeID, chosen.Score, chosen.Reason)
}
