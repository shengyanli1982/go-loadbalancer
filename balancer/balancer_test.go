package balancer_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/balancer"
	"github.com/shengyanli1982/go-loadbalancer/config"
	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/objective"
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

type panicSink struct{}

// OnEvent 模拟 telemetry sink panic，验证主流程隔离。
func (panicSink) OnEvent(_ telemetry.TelemetryEvent) {
	panic("sink panic")
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

// TestRouteNoHealthyNodes 验证无健康节点时返回预期错误。
func TestRouteNoHealthyNodes(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(cfg)
	require.NoError(t, err)

	_, routeErr := b.Route(context.Background(), types.RequestContext{RouteClass: types.RouteGeneric}, []types.NodeSnapshot{{NodeID: "n1", Healthy: false}})
	require.Error(t, routeErr)
	assert.ErrorIs(t, routeErr, lberrors.ErrNoHealthyNodes)
}

// TestRouteFallbackOnPolicyError 验证策略执行失败会触发回退链。
func TestRouteFallbackOnPolicyError(t *testing.T) {
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
