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
	nodes := []types.NodeSnapshot{
		{
			NodeID:       "node-a",
			Healthy:      true,
			StaticWeight: 1,
			Inflight:     6,
			QueueDepth:   2,
			P95LatencyMs: 24,
			ErrorRate:    0.004,
		},
		{
			NodeID:       "node-b",
			Healthy:      true,
			StaticWeight: 4,
			Inflight:     3,
			QueueDepth:   1,
			P95LatencyMs: 16,
			ErrorRate:    0.001,
		},
		{
			NodeID:       "node-c",
			Healthy:      true,
			StaticWeight: 2,
			Inflight:     8,
			QueueDepth:   4,
			P95LatencyMs: 33,
			ErrorRate:    0.008,
		},
	}

	baseReq := types.RequestContext{RouteClass: types.RouteGeneric}

	runRR(baseReq, nodes)
	runOneShot(baseReq, nodes, config.AlgorithmWeightedRoundRobin)
	runP2CIgnoresRequestSemantics(baseReq, nodes)
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
		printChosen(lb, baseReq, nodes, i)
	}
}

func runOneShot(baseReq types.RequestContext, nodes []types.NodeSnapshot, algorithm string) {
	lb := mustNewBalancer(algorithm)
	defer func() {
		_ = lb.Close(context.Background())
	}()

	fmt.Printf("algorithm=%s\n", algorithm)
	printChosen(lb, baseReq, nodes, 1)
}

func runP2CIgnoresRequestSemantics(baseReq types.RequestContext, nodes []types.NodeSnapshot) {
	fmt.Println("algorithm=p2c (request semantics ignored)")

	reqA := baseReq
	reqA.RequestID = "req-a"
	reqA.SessionID = "session-a"
	reqA.TenantID = "tenant-a"

	reqB := baseReq
	reqB.RequestID = "req-b"
	reqB.SessionID = "session-b"
	reqB.TenantID = "tenant-b"

	lbA := mustNewBalancer(config.AlgorithmP2C)
	defer func() {
		_ = lbA.Close(context.Background())
	}()
	printChosen(lbA, reqA, nodes, 1)

	lbB := mustNewBalancer(config.AlgorithmP2C)
	defer func() {
		_ = lbB.Close(context.Background())
	}()
	printChosen(lbB, reqB, nodes, 2)
}

func runConsistentHash(baseReq types.RequestContext, nodes []types.NodeSnapshot) {
	lb := mustNewBalancer(config.AlgorithmConsistentHash)
	defer func() {
		_ = lb.Close(context.Background())
	}()

	fmt.Println("algorithm=ch (explicit SessionID required)")
	sessionIDs := []string{"sticky-session", "sticky-session", "another-session"}
	for i, sessionID := range sessionIDs {
		req := baseReq
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
