package balancer_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/balancer"
	"github.com/shengyanli1982/go-loadbalancer/config"
	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/plugin/objective"
	"github.com/shengyanli1982/go-loadbalancer/plugin/policy"
	"github.com/shengyanli1982/go-loadbalancer/plugin/policy/llmkvaffinity"
	"github.com/shengyanli1982/go-loadbalancer/plugin/policy/tenantquota"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/telemetry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

type routeContextKey string

const routeContextMarkerKey routeContextKey = "route-marker"

type slowObjective struct{}

// Name 返回测试用慢目标函数的插件名。
func (slowObjective) Name() string { return "slow_objective" }

// Choose 模拟慢执行目标函数，用于覆盖超时回退分支。
func (slowObjective) Choose(_ types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	time.Sleep(30 * time.Millisecond)
	if len(candidates) == 0 {
		return types.Candidate{}, lberrors.ErrNoCandidate
	}
	return candidates[0], nil
}

type mediumObjective struct{}

func (mediumObjective) Name() string { return "medium_objective" }

func (mediumObjective) Choose(_ types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	time.Sleep(20 * time.Millisecond)
	if len(candidates) == 0 {
		return types.Candidate{}, lberrors.ErrNoCandidate
	}
	return candidates[0], nil
}

type concurrencyProbeObjective struct {
	name    string
	sleep   time.Duration
	current *atomic.Int32
	maxSeen *atomic.Int32
}

func (o *concurrencyProbeObjective) Name() string { return o.name }

func (o *concurrencyProbeObjective) Choose(_ types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	active := o.current.Add(1)
	for {
		prev := o.maxSeen.Load()
		if active <= prev || o.maxSeen.CompareAndSwap(prev, active) {
			break
		}
	}
	defer o.current.Add(-1)

	time.Sleep(o.sleep)
	if len(candidates) == 0 {
		return types.Candidate{}, lberrors.ErrNoCandidate
	}
	return candidates[0], nil
}

type panicSink struct{}

type panicObjective struct{}

func (panicObjective) Name() string { return "panic_objective" }

func (panicObjective) Choose(_ types.RequestContext, _ []types.Candidate) (types.Candidate, error) {
	panic("objective panic")
}

type softFailPolicy struct{}

func (softFailPolicy) Name() string { return "soft_fail_policy" }

func (softFailPolicy) ReRank(_ types.RequestContext, _ []types.Candidate) ([]types.Candidate, error) {
	return nil, errors.New("soft policy failure")
}

// OnEvent 模拟 telemetry sink panic，验证主流程隔离。
func (panicSink) OnEvent(_ telemetry.TelemetryEvent) {
	panic("sink panic")
}

type captureSink struct {
	mu     sync.Mutex
	events []telemetry.TelemetryEvent
}

func (s *captureSink) OnEvent(e telemetry.TelemetryEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *captureSink) eventsByType(t telemetry.EventType) []telemetry.TelemetryEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]telemetry.TelemetryEvent, 0, len(s.events))
	for _, e := range s.events {
		if e.Type == t {
			out = append(out, e)
		}
	}
	return out
}

type plainObjective struct{}

func (plainObjective) Name() string { return "plain_objective" }

func (plainObjective) Choose(_ types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	if len(candidates) == 0 {
		return types.Candidate{}, lberrors.ErrNoCandidate
	}
	return candidates[0], nil
}

type contextObjective struct{}

func (contextObjective) Name() string { return "context_objective" }

func (contextObjective) Choose(_ types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	if len(candidates) == 0 {
		return types.Candidate{}, lberrors.ErrNoCandidate
	}
	return candidates[0], nil
}

func (contextObjective) ChooseWithContext(ctx context.Context, req types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	select {
	case <-ctx.Done():
		return types.Candidate{}, ctx.Err()
	default:
		return plainObjective{}.Choose(req, candidates)
	}
}

type factoryRRAlgorithm struct {
	next int
}

func (*factoryRRAlgorithm) Name() string { return "factory_rr" }

func (p *factoryRRAlgorithm) SelectCandidates(_ types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if topK <= 0 {
		return nil, lberrors.ErrPluginMisconfigured
	}
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	limit := topK
	if limit > len(nodes) {
		limit = len(nodes)
	}
	start := p.next % len(nodes)
	p.next++

	out := make([]types.Candidate, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (start + i) % len(nodes)
		out = append(out, types.Candidate{Node: nodes[idx], Reason: []string{"algorithm=factory_rr"}})
	}
	return out, nil
}

type errorAlgorithm struct{}

func (errorAlgorithm) Name() string { return "error_algorithm" }

func (errorAlgorithm) SelectCandidates(_ types.RequestContext, _ []types.NodeSnapshot, _ int) ([]types.Candidate, error) {
	return nil, errors.New("algorithm exploded")
}

type contextAwareAlgorithm struct{}

func (contextAwareAlgorithm) Name() string { return "ctx_algorithm" }

func (contextAwareAlgorithm) SelectCandidates(_ types.RequestContext, _ []types.NodeSnapshot, _ int) ([]types.Candidate, error) {
	return nil, errors.New("legacy algorithm path should not be used")
}

func (contextAwareAlgorithm) SelectCandidatesWithContext(ctx context.Context, _ types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if got := ctx.Value(routeContextMarkerKey); got != "algorithm" {
		return nil, errors.New("missing algorithm context marker")
	}
	if topK <= 0 || len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}
	return []types.Candidate{{Node: nodes[0], Reason: []string{"algorithm=context_aware"}}}, nil
}

type contextAwareHardPolicy struct{}

func (contextAwareHardPolicy) Name() string { return "ctx_hard_policy" }

func (contextAwareHardPolicy) ReRank(_ types.RequestContext, _ []types.Candidate) ([]types.Candidate, error) {
	return nil, errors.New("legacy policy path should not be used")
}

func (contextAwareHardPolicy) ReRankWithContext(ctx context.Context, _ types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error) {
	if got := ctx.Value(routeContextMarkerKey); got != "policy" {
		return nil, errors.New("missing policy context marker")
	}
	if len(candidates) == 0 {
		return nil, lberrors.ErrNoCandidate
	}
	out := append([]types.Candidate(nil), candidates...)
	out[0].Reason = append(out[0].Reason, "policy=context_aware")
	return out, nil
}

func (contextAwareHardPolicy) IsHardConstraint() bool { return true }

// TestRouteSuccess 验证基础路由成功场景。
func TestRouteSuccess(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(cfg, config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest))
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true, Inflight: 10, QueueDepth: 5},
		{NodeID: "n2", Healthy: true, Inflight: 1, QueueDepth: 1},
	}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n2", candidate.Node.NodeID)
}

func TestRouteUsesContextAwareAlgorithmWhenAvailable(t *testing.T) {
	if err := registry.RegisterAlgorithm(contextAwareAlgorithm{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, "ctx_algorithm"),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithFallback("ctx_algorithm"),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 1, QueueDepth: 1}}

	ctx := context.WithValue(context.Background(), routeContextMarkerKey, "algorithm")
	candidate, routeErr := b.Route(ctx, req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
	assert.True(t, containsReason(candidate.Reason, "algorithm=context_aware"))
}

func TestRouteUsesContextAwarePolicyWhenAvailable(t *testing.T) {
	if err := registry.RegisterPolicy(contextAwareHardPolicy{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies("ctx_hard_policy"),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true, Inflight: 2, QueueDepth: 1},
		{NodeID: "n2", Healthy: true, Inflight: 1, QueueDepth: 1},
	}

	ctx := context.WithValue(context.Background(), routeContextMarkerKey, "policy")
	candidate, routeErr := b.Route(ctx, req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n2", candidate.Node.NodeID)
	assert.True(t, containsReason(candidate.Reason, "policy=context_aware"))
}

func TestRouteStageAwarePolicyRequiresExplicitOptIn(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithPolicies(config.PolicyHealthGate, config.PolicyLLMStageAware),
	)
	require.NoError(t, err)

	nodes := []types.NodeSnapshot{
		{
			NodeID:         "n-prefill",
			Healthy:        true,
			Inflight:       2,
			QueueDepth:     2,
			P95LatencyMs:   10,
			ErrorRate:      0.01,
			TTFTms:         20,
			TPOTms:         50,
			KVCacheHitRate: 0.50,
		},
		{
			NodeID:         "n-decode",
			Healthy:        true,
			Inflight:       2,
			QueueDepth:     2,
			P95LatencyMs:   10,
			ErrorRate:      0.01,
			TTFTms:         100,
			TPOTms:         5,
			KVCacheHitRate: 0.50,
		},
	}

	prefillReq := types.RequestContext{
		RouteClass:     types.RouteLLMPrefill,
		PromptTokens:   1024,
		ExpectedTokens: 256,
	}
	prefillCandidate, err := b.Route(context.Background(), prefillReq, nodes)
	require.NoError(t, err)
	assert.Equal(t, "n-prefill", prefillCandidate.Node.NodeID)
	assert.True(t, containsReason(prefillCandidate.Reason, "llm_stage_aware_prefill"))

	decodeReq := types.RequestContext{
		RouteClass:     types.RouteLLMDecode,
		PromptTokens:   1024,
		ExpectedTokens: 256,
	}
	decodeCandidate, err := b.Route(context.Background(), decodeReq, nodes)
	require.NoError(t, err)
	assert.Equal(t, "n-decode", decodeCandidate.Node.NodeID)
	assert.True(t, containsReason(decodeCandidate.Reason, "llm_stage_aware_decode"))
}

func TestRouteKVAffinityPolicyRequiresExplicitOptIn(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithPolicies(config.PolicyHealthGate, config.PolicyLLMKVAffinity),
	)
	require.NoError(t, err)

	nodes := []types.NodeSnapshot{
		{
			NodeID:         "n1",
			Healthy:        true,
			Inflight:       1,
			QueueDepth:     1,
			P95LatencyMs:   10,
			ErrorRate:      0.01,
			TTFTms:         30,
			TPOTms:         30,
			KVCacheHitRate: 0.10,
		},
		{
			NodeID:         "n2",
			Healthy:        true,
			Inflight:       1,
			QueueDepth:     1,
			P95LatencyMs:   10,
			ErrorRate:      0.01,
			TTFTms:         30,
			TPOTms:         30,
			KVCacheHitRate: 0.95,
		},
	}

	req := types.RequestContext{
		RouteClass:     types.RouteLLMPrefill,
		PromptTokens:   512,
		ExpectedTokens: 128,
		Metadata: map[string]string{
			llmkvaffinity.MetadataPreferredNodesKey: "n1",
		},
	}
	candidate, err := b.Route(context.Background(), req, nodes)
	require.NoError(t, err)
	assert.Equal(t, "n1", candidate.Node.NodeID)
	assert.True(t, containsReason(candidate.Reason, "llm_kv_affinity_request_hint"))
}

func TestRouteProfileOverrideLegacyAlgorithm(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithRouteProfile(types.RouteGeneric, config.RouteProfile{
			Algorithm: config.AlgorithmRoundRobin,
			Policies:  []string{config.PolicyHealthGate},
		}),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true, Inflight: 100, QueueDepth: 100},
		{NodeID: "n2", Healthy: true, Inflight: 1, QueueDepth: 1},
	}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
}

func TestRouteProfileOverrideLegacyPolicies(t *testing.T) {
	if err := registry.RegisterPolicy(softFailPolicy{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies("soft_fail_policy"),
		config.WithRouteProfile(types.RouteGeneric, config.RouteProfile{
			Algorithm: config.AlgorithmLeastRequest,
			Policies:  []string{config.PolicyHealthGate},
		}),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 1}}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
	assert.False(t, containsReason(candidate.Reason, "fallback="))
}

func TestRouteSessionAffinityFromPrefillToDecode(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteLLMPrefill, config.AlgorithmRoundRobin),
		config.WithAlgorithm(types.RouteLLMDecode, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate, config.PolicyLLMSessionAffinity),
	)
	require.NoError(t, err)

	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true, Inflight: 10, QueueDepth: 10},
		{NodeID: "n2", Healthy: true, Inflight: 1, QueueDepth: 1},
	}
	prefillReq := types.RequestContext{
		RouteClass:     types.RouteLLMPrefill,
		SessionID:      "s-1",
		PromptTokens:   128,
		ExpectedTokens: 64,
	}
	prefillCandidate, err := b.Route(context.Background(), prefillReq, nodes)
	require.NoError(t, err)
	assert.Equal(t, "n1", prefillCandidate.Node.NodeID)

	decodeReq := types.RequestContext{
		RouteClass:     types.RouteLLMDecode,
		SessionID:      "s-1",
		PromptTokens:   128,
		ExpectedTokens: 64,
	}
	decodeCandidate, err := b.Route(context.Background(), decodeReq, nodes)
	require.NoError(t, err)
	assert.Equal(t, "n1", decodeCandidate.Node.NodeID)
	assert.True(t, containsReason(decodeCandidate.Reason, "llm_session_affinity_hit"))
}

func TestRouteDegradeChainUsesRouteProfileOverride(t *testing.T) {
	if err := registry.RegisterPolicy(softFailPolicy{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteLLMDecode, config.AlgorithmLeastRequest),
		config.WithPolicies("soft_fail_policy"),
		config.WithFallback(config.AlgorithmLeastRequest),
		config.WithRouteProfile(types.RouteLLMDecode, config.RouteProfile{
			DegradeChain: []string{config.AlgorithmRoundRobin},
		}),
	)
	require.NoError(t, err)

	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true, Inflight: 20, QueueDepth: 20},
		{NodeID: "n2", Healthy: true, Inflight: 1, QueueDepth: 1},
	}
	req := types.RequestContext{
		RouteClass:     types.RouteLLMDecode,
		PromptTokens:   64,
		ExpectedTokens: 64,
	}
	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
	assert.True(t, containsReason(candidate.Reason, "fallback=rr"))
}

func TestRouteReliabilityPilotPrefersPrimaryPool(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithReliabilityPilot(true),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
	)
	require.NoError(t, err)

	req := types.RequestContext{
		RouteClass:     types.RouteGeneric,
		PrimaryPool:    "pool-a",
		SecondaryPools: []string{"pool-b"},
	}
	nodes := []types.NodeSnapshot{
		{NodeID: "a1", Pool: "pool-a", Healthy: true, Inflight: 10, QueueDepth: 10},
		{NodeID: "b1", Pool: "pool-b", Healthy: true, Inflight: 1, QueueDepth: 1},
	}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "a1", candidate.Node.NodeID)
}

func TestRouteReliabilityPilotDegradesToSecondaryPoolOnOutlier(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithReliabilityPilot(true),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
	)
	require.NoError(t, err)

	req := types.RequestContext{
		RouteClass:     types.RouteGeneric,
		PrimaryPool:    "pool-a",
		SecondaryPools: []string{"pool-b"},
	}
	nodes := []types.NodeSnapshot{
		{NodeID: "a1", Pool: "pool-a", Outlier: true, Healthy: true, Inflight: 1, QueueDepth: 1},
		{NodeID: "b1", Pool: "pool-b", Healthy: true, Inflight: 2, QueueDepth: 1},
	}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "b1", candidate.Node.NodeID)
}

func TestRouteReliabilityPilotTelemetryContainsPoolDegradeReason(t *testing.T) {
	cfg := config.DefaultConfig()
	sink := &captureSink{}
	b, err := balancer.New(
		cfg,
		config.WithReliabilityPilot(true),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithTelemetrySink(sink),
	)
	require.NoError(t, err)

	req := types.RequestContext{
		RouteClass:     types.RouteGeneric,
		PrimaryPool:    "pool-a",
		SecondaryPools: []string{"pool-b"},
	}
	nodes := []types.NodeSnapshot{
		{NodeID: "a1", Pool: "pool-a", Outlier: true, Healthy: true, Inflight: 1, QueueDepth: 1},
		{NodeID: "b1", Pool: "pool-b", Healthy: true, Inflight: 2, QueueDepth: 1},
	}

	_, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)

	events := sink.eventsByType(telemetry.EventRouteDecision)
	require.NotEmpty(t, events)
	found := false
	for _, event := range events {
		if strings.Contains(event.Reason, "pool_degrade=pool-a->pool-b") {
			found = true
			break
		}
	}
	assert.True(t, found)
}

// TestRouteNoHealthyNodes 验证无健康节点时返回预期错误。
func TestRouteNoHealthyNodes(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(cfg)
	require.NoError(t, err)

	_, routeErr := b.Route(context.Background(), types.RequestContext{RouteClass: types.RouteGeneric}, []types.NodeSnapshot{{NodeID: "n1", Healthy: false}})
	require.Error(t, routeErr)
	assert.ErrorIs(t, routeErr, lberrors.ErrNoHealthyNodes)
}

func TestRouteInputGuardRejectsInvalidRequest(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithInputGuard(true),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
	)
	require.NoError(t, err)

	_, routeErr := b.Route(context.Background(), types.RequestContext{}, []types.NodeSnapshot{{NodeID: "n1", Healthy: true}})
	require.Error(t, routeErr)
	assert.ErrorIs(t, routeErr, lberrors.ErrInputGuardRejectedRequest)
	assert.Contains(t, routeErr.Error(), lberrors.CodeInputGuardRejectedRequest)
}

func TestRouteInputGuardRejectsInvalidNode(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithInputGuard(true),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	_, routeErr := b.Route(context.Background(), req, []types.NodeSnapshot{{NodeID: "", Healthy: true}})
	require.Error(t, routeErr)
	assert.ErrorIs(t, routeErr, lberrors.ErrInputGuardRejectedNode)
	assert.Contains(t, routeErr.Error(), lberrors.CodeInputGuardRejectedNode)
}

func TestRouteInputGuardDisabledKeepsLegacyBehavior(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithInputGuard(false),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
	)
	require.NoError(t, err)

	_, routeErr := b.Route(context.Background(), types.RequestContext{}, []types.NodeSnapshot{{NodeID: "n1", Healthy: true}})
	require.Error(t, routeErr)
	assert.ErrorIs(t, routeErr, lberrors.ErrPluginMisconfigured)
	assert.NotErrorIs(t, routeErr, lberrors.ErrInputGuardRejectedRequest)
}

func TestRouteInputGuardTelemetryIncludesRejectCode(t *testing.T) {
	cfg := config.DefaultConfig()
	sink := &captureSink{}
	b, err := balancer.New(
		cfg,
		config.WithInputGuard(true),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithTelemetrySink(sink),
	)
	require.NoError(t, err)

	_, routeErr := b.Route(context.Background(), types.RequestContext{}, []types.NodeSnapshot{{NodeID: "n1", Healthy: true}})
	require.Error(t, routeErr)

	events := sink.eventsByType(telemetry.EventRouteDecision)
	require.NotEmpty(t, events)
	last := events[len(events)-1]
	assert.Equal(t, "filter", last.Stage)
	assert.Equal(t, "failed", last.Outcome)
	assert.Contains(t, last.Reason, lberrors.CodeInputGuardRejectedRequest)
}

func TestRouteLLMBudgetGateRejectsOverTotalTokenBudget(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteLLMPrefill, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyLLMBudgetGate),
	)
	require.NoError(t, err)

	req := types.RequestContext{
		RouteClass:           types.RouteLLMPrefill,
		PromptTokens:         1024,
		ExpectedTokens:       1024,
		BudgetMaxTotalTokens: 512,
	}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 1, QueueDepth: 1}}

	_, routeErr := b.Route(context.Background(), req, nodes)
	require.Error(t, routeErr)
	assert.ErrorIs(t, routeErr, lberrors.ErrNoCandidate)
	assert.Contains(t, routeErr.Error(), "reject=total_tokens")
}

func TestRouteLLMBudgetGateHardRejectEmitsTelemetry(t *testing.T) {
	cfg := config.DefaultConfig()
	sink := &captureSink{}
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteLLMPrefill, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyLLMBudgetGate),
		config.WithTelemetrySink(sink),
	)
	require.NoError(t, err)

	req := types.RequestContext{
		RouteClass:          types.RouteLLMPrefill,
		BudgetMaxInflight:   1,
		BudgetMaxQueueDepth: 1,
	}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 2, QueueDepth: 2}}

	_, routeErr := b.Route(context.Background(), req, nodes)
	require.Error(t, routeErr)
	assert.ErrorIs(t, routeErr, lberrors.ErrNoCandidate)

	events := sink.eventsByType(telemetry.EventRouteDecision)
	require.NotEmpty(t, events)
	last := events[len(events)-1]
	assert.Equal(t, "policy", last.Stage)
	assert.Equal(t, "failed", last.Outcome)
	assert.Equal(t, config.PolicyLLMBudgetGate, last.Plugin)
	assert.Contains(t, last.Reason, "reject=node_budget")
}

// TestRouteHardPolicyErrorFailsClosed 验证硬约束策略失败时不会被 fallback 绕过。
func TestRouteHardPolicyErrorFailsClosed(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyTenantQuota),
	)
	require.NoError(t, err)

	req := types.RequestContext{
		RouteClass: types.RouteGeneric,
		Metadata: map[string]string{
			tenantquota.MetadataMaxInflightKey: "not-an-int",
		},
	}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 2}}

	_, routeErr := b.Route(context.Background(), req, nodes)
	require.Error(t, routeErr)
	assert.ErrorIs(t, routeErr, lberrors.ErrPluginMisconfigured)
}

// TestRouteFallbackOnSoftPolicyError 验证软策略失败仍可触发 fallback。
func TestRouteFallbackOnSoftPolicyError(t *testing.T) {
	if err := registry.RegisterPolicy(softFailPolicy{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies("soft_fail_policy"),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 2}}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
	assert.True(t, containsReason(candidate.Reason, "fallback="))
	assert.True(t, containsReason(candidate.Reason, "cause=policy_reject"))
}

// TestRouteFallbackOnObjectiveTimeout 验证目标函数超时会触发回退链。
func TestRouteFallbackOnObjectiveTimeout(t *testing.T) {
	if err := registry.RegisterObjective(slowObjective{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithObjective("slow_objective", 1, true),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 1}}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
	assert.True(t, containsReason(candidate.Reason, "fallback="))
	assert.True(t, containsReason(candidate.Reason, "cause=objective_timeout"))
}

func TestRouteFallbackOnAlgorithmErrorAnnotatesFailureReason(t *testing.T) {
	if err := registry.RegisterAlgorithm(errorAlgorithm{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, "error_algorithm"),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithFallback(config.AlgorithmLeastRequest),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 1, QueueDepth: 1}}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
	assert.True(t, containsReason(candidate.Reason, "fallback=lr"))
	assert.True(t, containsReason(candidate.Reason, "cause=algorithm_error"))
}

func TestRouteObjectiveTimeoutTierForPrefill(t *testing.T) {
	if err := registry.RegisterObjective(mediumObjective{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithAlgorithm(types.RouteLLMPrefill, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithObjective("medium_objective", 15, true),
	)
	require.NoError(t, err)

	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 1}}

	prefillReq := types.RequestContext{
		RouteClass:     types.RouteLLMPrefill,
		PromptTokens:   4096,
		ExpectedTokens: 256,
	}
	prefillCandidate, err := b.Route(context.Background(), prefillReq, nodes)
	require.NoError(t, err)
	assert.True(t, containsReason(prefillCandidate.Reason, "selected_by=objective"))
	assert.False(t, containsReason(prefillCandidate.Reason, "fallback="))

	genericReq := types.RequestContext{RouteClass: types.RouteGeneric}
	genericCandidate, err := b.Route(context.Background(), genericReq, nodes)
	require.NoError(t, err)
	assert.True(t, containsReason(genericCandidate.Reason, "fallback="))
}

func TestRouteObjectiveConcurrencyGuard(t *testing.T) {
	name := "objective_concurrency_guard_" + strings.ReplaceAll(t.Name(), "/", "_")
	var current atomic.Int32
	var maxSeen atomic.Int32

	err := registry.RegisterObjectiveFactory(name, func() objective.Plugin {
		return &concurrencyProbeObjective{
			name:    name,
			sleep:   30 * time.Millisecond,
			current: &current,
			maxSeen: &maxSeen,
		}
	})
	if err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithObjective(name, 150, true),
		config.WithObjectiveMaxConcurrent(1),
	)
	require.NoError(t, err)

	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 1}}
	req := types.RequestContext{RouteClass: types.RouteGeneric}

	const workers = 3
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			candidate, routeErr := b.Route(context.Background(), req, nodes)
			if routeErr != nil {
				errCh <- routeErr
				return
			}
			if candidate.Node.NodeID != "n1" {
				errCh <- errors.New("unexpected candidate")
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for routeErr := range errCh {
		require.NoError(t, routeErr)
	}

	assert.Equal(t, int32(1), maxSeen.Load())
}

// TestRouteFallbackOnObjectivePanic 验证 objective panic 时可降级回退。
func TestRouteFallbackOnObjectivePanic(t *testing.T) {
	if err := registry.RegisterObjective(panicObjective{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithObjective("panic_objective", 5, true),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 1}}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
	assert.True(t, containsReason(candidate.Reason, "fallback="))
}

// TestRoundRobinStateIsolationBetweenBalancers 验证不同 balancer 的 rr 状态彼此独立。
func TestRoundRobinStateIsolationBetweenBalancers(t *testing.T) {
	cfg := config.DefaultConfig()
	b1, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmRoundRobin),
		config.WithPolicies(config.PolicyHealthGate),
	)
	require.NoError(t, err)
	b2, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmRoundRobin),
		config.WithPolicies(config.PolicyHealthGate),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true},
		{NodeID: "n2", Healthy: true},
		{NodeID: "n3", Healthy: true},
	}

	_, err = b1.Route(context.Background(), req, nodes)
	require.NoError(t, err)
	_, err = b1.Route(context.Background(), req, nodes)
	require.NoError(t, err)

	candidate, err := b2.Route(context.Background(), req, nodes)
	require.NoError(t, err)
	assert.Equal(t, "n1", candidate.Node.NodeID)
}

// TestRoundRobinStateIsolationConcurrent 验证并发场景下多实例 rr 仍保持各自轮转序列。
func TestRoundRobinStateIsolationConcurrent(t *testing.T) {
	cfg := config.DefaultConfig()
	b1, err := balancer.New(cfg, config.WithAlgorithm(types.RouteGeneric, config.AlgorithmRoundRobin))
	require.NoError(t, err)
	b2, err := balancer.New(cfg, config.WithAlgorithm(types.RouteGeneric, config.AlgorithmRoundRobin))
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true},
		{NodeID: "n2", Healthy: true},
	}

	const iterations = 50
	run := func(lb balancer.Balancer, got chan<- string, errCh chan<- error) {
		for i := 0; i < iterations; i++ {
			candidate, routeErr := lb.Route(context.Background(), req, nodes)
			if routeErr != nil {
				errCh <- routeErr
				return
			}
			got <- candidate.Node.NodeID
		}
	}

	results1 := make(chan string, iterations)
	results2 := make(chan string, iterations)
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		run(b1, results1, errCh)
	}()
	go func() {
		defer wg.Done()
		run(b2, results2, errCh)
	}()
	wg.Wait()
	close(results1)
	close(results2)
	close(errCh)

	for routeErr := range errCh {
		require.NoError(t, routeErr)
	}

	first1 := <-results1
	first2 := <-results2
	assert.Equal(t, "n1", first1)
	assert.Equal(t, "n1", first2)
}

// TestFactoryAlgorithmStateIsolationBetweenBalancers 验证工厂路径下状态型算法按 balancer 实例隔离。
func TestFactoryAlgorithmStateIsolationBetweenBalancers(t *testing.T) {
	err := registry.RegisterAlgorithmFactory("factory_rr", func() algorithm.Plugin { return &factoryRRAlgorithm{} })
	if err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b1, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, "factory_rr"),
		config.WithPolicies(config.PolicyHealthGate),
	)
	require.NoError(t, err)
	b2, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, "factory_rr"),
		config.WithPolicies(config.PolicyHealthGate),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true},
		{NodeID: "n2", Healthy: true},
		{NodeID: "n3", Healthy: true},
	}

	_, err = b1.Route(context.Background(), req, nodes)
	require.NoError(t, err)
	_, err = b1.Route(context.Background(), req, nodes)
	require.NoError(t, err)

	candidate, err := b2.Route(context.Background(), req, nodes)
	require.NoError(t, err)
	assert.Equal(t, "n1", candidate.Node.NodeID)
}

// TestWeightedObjectiveUsesConfigWeights 验证 Config.Weights 会影响 runtime objective 决策。
func TestWeightedObjectiveUsesConfigWeights(t *testing.T) {
	cfg := config.DefaultConfig()
	defaultLB, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithObjective(config.ObjectiveWeighted, 5, true),
		config.WithPolicies(config.PolicyHealthGate),
	)
	require.NoError(t, err)
	queueFirstLB, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithObjective(config.ObjectiveWeighted, 5, true),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithWeight(types.RouteGeneric, config.MetricQueue, 9000),
		config.WithWeight(types.RouteGeneric, config.MetricP95Latency, 1000),
		config.WithWeight(types.RouteGeneric, config.MetricErrorRate, 0),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true, Inflight: 1, QueueDepth: 1, P95LatencyMs: 100, ErrorRate: 0},
		{NodeID: "n2", Healthy: true, Inflight: 1, QueueDepth: 20, P95LatencyMs: 10, ErrorRate: 0},
	}

	defaultCandidate, err := defaultLB.Route(context.Background(), req, nodes)
	require.NoError(t, err)
	queueFirstCandidate, err := queueFirstLB.Route(context.Background(), req, nodes)
	require.NoError(t, err)

	assert.Equal(t, "n2", defaultCandidate.Node.NodeID)
	assert.Equal(t, "n1", queueFirstCandidate.Node.NodeID)
}

// TestRouteSnapshotTTLGuardEnabled 验证开启 TTL 防护时会过滤过期快照节点。
func TestRouteSnapshotTTLGuardEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithSnapshotTTLGuard(true),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true, FreshnessTTLms: 0, Inflight: 1, QueueDepth: 1},
		{NodeID: "n2", Healthy: true, FreshnessTTLms: 5000, Inflight: 10, QueueDepth: 1},
	}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n2", candidate.Node.NodeID)
}

// TestRouteSnapshotTTLGuardDisabled 验证关闭 TTL 防护时保持历史兼容行为。
func TestRouteSnapshotTTLGuardDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithSnapshotTTLGuard(false),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true, FreshnessTTLms: 0, Inflight: 1, QueueDepth: 1},
		{NodeID: "n2", Healthy: true, FreshnessTTLms: 5000, Inflight: 10, QueueDepth: 1},
	}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
}

// TestFallbackUsesMultiCandidateHardPolicyCheck 验证 fallback 算法步骤会逐个候选执行硬约束复检。
func TestFallbackUsesMultiCandidateHardPolicyCheck(t *testing.T) {
	if err := registry.RegisterPolicy(softFailPolicy{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithTopK(2),
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmRoundRobin),
		config.WithPolicies("soft_fail_policy", config.PolicyTenantQuota),
		config.WithFallback(config.AlgorithmRoundRobin),
	)
	require.NoError(t, err)

	req := types.RequestContext{
		RouteClass: types.RouteGeneric,
		Metadata: map[string]string{
			tenantquota.MetadataMaxInflightKey: "5",
		},
	}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Healthy: true, Inflight: 10},
		{NodeID: "n2", Healthy: true, Inflight: 1},
	}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n2", candidate.Node.NodeID)
	assert.True(t, containsReason(candidate.Reason, "fallback=rr"))
}

// TestTelemetryObjectiveCancellableTrue 验证可取消 objective 会上报 cancellable=true。
func TestTelemetryObjectiveCancellableTrue(t *testing.T) {
	if err := registry.RegisterObjective(contextObjective{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	sink := &captureSink{}
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithObjective("context_objective", 5, true),
		config.WithTelemetrySink(sink),
	)
	require.NoError(t, err)

	_, routeErr := b.Route(context.Background(), types.RequestContext{RouteClass: types.RouteGeneric}, []types.NodeSnapshot{{NodeID: "n1", Healthy: true}})
	require.NoError(t, routeErr)

	events := sink.eventsByType(telemetry.EventObjectiveResult)
	require.NotEmpty(t, events)
	assert.True(t, events[len(events)-1].ObjectiveCancellable)
}

// TestTelemetryObjectiveCancellableFalse 验证不可取消 objective 会上报 cancellable=false。
func TestTelemetryObjectiveCancellableFalse(t *testing.T) {
	if err := registry.RegisterObjective(plainObjective{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	sink := &captureSink{}
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithPolicies(config.PolicyHealthGate),
		config.WithObjective("plain_objective", 5, true),
		config.WithTelemetrySink(sink),
	)
	require.NoError(t, err)

	_, routeErr := b.Route(context.Background(), types.RequestContext{RouteClass: types.RouteGeneric}, []types.NodeSnapshot{{NodeID: "n1", Healthy: true}})
	require.NoError(t, routeErr)

	events := sink.eventsByType(telemetry.EventObjectiveResult)
	require.NotEmpty(t, events)
	assert.False(t, events[len(events)-1].ObjectiveCancellable)
}

// TestRouteTelemetryPanicDoesNotBreakFlow 验证 telemetry panic 不影响路由主流程。
func TestRouteTelemetryPanicDoesNotBreakFlow(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmLeastRequest),
		config.WithTelemetrySink(panicSink{}),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 1}}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
}

// containsReason 判断原因列表中是否包含指定片段。
func containsReason(reasons []string, prefix string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, prefix) {
			return true
		}
	}
	return false
}

var _ objective.Plugin = slowObjective{}
var _ objective.Plugin = mediumObjective{}
var _ objective.Plugin = panicObjective{}
var _ objective.Plugin = plainObjective{}
var _ objective.ContextPlugin = contextObjective{}
var _ objective.Plugin = (*concurrencyProbeObjective)(nil)
var _ policy.Plugin = softFailPolicy{}
var _ algorithm.Plugin = (*factoryRRAlgorithm)(nil)
