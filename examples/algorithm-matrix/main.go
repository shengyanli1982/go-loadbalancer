package main

import (
	"context"
	"fmt"
	"log"

	"github.com/shengyanli1982/go-loadbalancer/balancer"
	"github.com/shengyanli1982/go-loadbalancer/config"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const demoTopK = 3

func main() {
	modelASet := types.NewModelCapabilitySet(map[string]bool{"model-a": true})
	nodes := []types.NodeSnapshot{
		{
			NodeID:          "node-a",
			Healthy:         true,
			StaticWeight:    1,
			Inflight:        6,
			QueueDepth:      2,
			P95LatencyMs:    24,
			ErrorRate:       0.004,
			ModelCapability: modelASet,
		},
		{
			NodeID:          "node-b",
			Healthy:         true,
			StaticWeight:    4,
			Inflight:        3,
			QueueDepth:      1,
			P95LatencyMs:    16,
			ErrorRate:       0.001,
			ModelCapability: modelASet,
		},
		{
			NodeID:          "node-c",
			Healthy:         true,
			StaticWeight:    2,
			Inflight:        8,
			QueueDepth:      4,
			P95LatencyMs:    33,
			ErrorRate:       0.008,
			ModelCapability: modelASet,
		},
	}

	baseReq := types.RequestContext{
		TenantID:   "team-algorithm-demo",
		RouteClass: types.RouteGeneric,
		Model:      "model-a",
	}

	runRR(baseReq, nodes)
	runOneShot(baseReq, nodes, config.AlgorithmWeightedRoundRobin)
	runOneShot(baseReq, nodes, config.AlgorithmP2C)
	runOneShot(baseReq, nodes, config.AlgorithmLeastRequest)
	runConsistentHash(baseReq, nodes)
}

func runRR(baseReq types.RequestContext, nodes []types.NodeSnapshot) {
	lb := mustNewBalancer(config.AlgorithmRoundRobin)
	defer func() {
		_ = lb.Close(context.Background())
	}()

	fmt.Println("algorithm=rr")
	for i := 1; i <= 3; i++ {
		req := baseReq
		req.RequestID = fmt.Sprintf("rr-%d", i)
		req.SessionID = fmt.Sprintf("rr-session-%d", i)
		printChosen(lb, req, nodes, i)
	}
}

func runOneShot(baseReq types.RequestContext, nodes []types.NodeSnapshot, algorithm string) {
	lb := mustNewBalancer(algorithm)
	defer func() {
		_ = lb.Close(context.Background())
	}()

	fmt.Printf("algorithm=%s\n", algorithm)
	req := baseReq
	req.RequestID = "req-" + algorithm
	req.SessionID = "session-" + algorithm
	printChosen(lb, req, nodes, 1)
}

func runConsistentHash(baseReq types.RequestContext, nodes []types.NodeSnapshot) {
	lb := mustNewBalancer(config.AlgorithmConsistentHash)
	defer func() {
		_ = lb.Close(context.Background())
	}()

	fmt.Println("algorithm=ch")
	sessionIDs := []string{"sticky-session", "sticky-session", "another-session"}
	for i, sessionID := range sessionIDs {
		req := baseReq
		req.RequestID = fmt.Sprintf("ch-%d", i+1)
		req.SessionID = sessionID
		printChosen(lb, req, nodes, i+1)
	}
}

func mustNewBalancer(algorithm string) balancer.Balancer {
	lb, err := balancer.New(
		config.DefaultConfig(),
		config.WithTopK(demoTopK),
		config.WithAlgorithm(types.RouteGeneric, algorithm),
		config.WithPolicies(config.PolicyHealthGate),
	)
	if err != nil {
		log.Fatalf("create balancer for algorithm=%s: %v", algorithm, err)
	}
	return lb
}

func printChosen(lb balancer.Balancer, req types.RequestContext, nodes []types.NodeSnapshot, run int) {
	chosen, err := lb.Route(context.Background(), req, nodes)
	if err != nil {
		log.Fatalf("route failed: %v", err)
	}
	fmt.Printf("  run=%d session=%s chosen=%s score=%.2f reason=%v\n",
		run, req.SessionID, chosen.Node.NodeID, chosen.Score, chosen.Reason)
}
