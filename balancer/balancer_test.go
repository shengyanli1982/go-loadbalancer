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

func TestRouteDefaultConfigDifferentiatesLLMStages(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(cfg)
	require.NoError(t, err)

	nodes := []types.NodeSnapshot{
		{
			NodeID:         "n-prefill",
			Healthy:        true,
			Inflight:       2,
			QueueDepth:     2,
			P95LatencyMs:   10,
			ErrorRate:      0.01,
			TTFTMs:         20,
			TPOTMs:         50,
			KVCacheHitRate: 0.50,
		},
		{
			NodeID:         "n-decode",
			Healthy:        true,
			Inflight:       2,
			QueueDepth:     2,
			P95LatencyMs:   10,
			ErrorRate:      0.01,
			TTFTMs:         100,
			TPOTMs:         5,
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

func TestRouteDefaultConfigHonorsRequestKVHint(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(cfg)
	require.NoError(t, err)

	nodes := []types.NodeSnapshot{
		{
			NodeID:         "n1",
			Healthy:        true,
			Inflight:       1,
			QueueDepth:     1,
			P95LatencyMs:   10,
			ErrorRate:      0.01,
			TTFTMs:         30,
			TPOTMs:         30,
			KVCacheHitRate: 0.10,
		},
		{
			NodeID:         "n2",
			Healthy:        true,
			Inflight:       1,
			QueueDepth:     1,
			P95LatencyMs:   10,
			ErrorRate:      0.01,
			TTFTMs:         30,
			TPOTMs:         30,
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

// TestRouteNoHealthyNodes 验证无健康节点时返回预期错误。
func TestRouteNoHealthyNodes(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(cfg)
	require.NoError(t, err)

	_, routeErr := b.Route(context.Background(), types.RequestContext{RouteClass: types.RouteGeneric}, []types.NodeSnapshot{{NodeID: "n1", Healthy: false}})
	require.Error(t, routeErr)
	assert.ErrorIs(t, routeErr, lberrors.ErrNoHealthyNodes)
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
