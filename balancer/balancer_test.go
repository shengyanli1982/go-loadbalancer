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
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/telemetry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

type slowObjective struct{}

func (slowObjective) Name() string { return "slow_objective" }
func (slowObjective) Choose(_ types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	time.Sleep(30 * time.Millisecond)
	if len(candidates) == 0 {
		return types.Candidate{}, lberrors.ErrNoCandidate
	}
	return candidates[0], nil
}

type panicSink struct{}

func (panicSink) OnEvent(_ telemetry.TelemetryEvent) {
	panic("sink panic")
}

func TestRouteSuccess(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(cfg, config.WithAlgorithm(string(types.RouteGeneric), "least_request"))
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

func TestRouteNoHealthyNodes(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(cfg)
	require.NoError(t, err)

	_, routeErr := b.Route(context.Background(), types.RequestContext{RouteClass: types.RouteGeneric}, []types.NodeSnapshot{{NodeID: "n1", Healthy: false}})
	require.Error(t, routeErr)
	assert.ErrorIs(t, routeErr, lberrors.ErrNoHealthyNodes)
}

func TestRouteFallbackOnPolicyError(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(string(types.RouteGeneric), "least_request"),
		config.WithPolicies("tenant_quota"),
	)
	require.NoError(t, err)

	req := types.RequestContext{
		RouteClass: types.RouteGeneric,
		Metadata: map[string]string{
			"tenant_quota_max_inflight": "not-an-int",
		},
	}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 2}}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
	assert.True(t, containsReason(candidate.Reason, "fallback="))
}

func TestRouteFallbackOnObjectiveTimeout(t *testing.T) {
	if err := registry.RegisterObjective(slowObjective{}); err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(string(types.RouteGeneric), "least_request"),
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

func TestRouteTelemetryPanicDoesNotBreakFlow(t *testing.T) {
	cfg := config.DefaultConfig()
	b, err := balancer.New(
		cfg,
		config.WithAlgorithm(string(types.RouteGeneric), "least_request"),
		config.WithTelemetrySink(panicSink{}),
	)
	require.NoError(t, err)

	req := types.RequestContext{RouteClass: types.RouteGeneric}
	nodes := []types.NodeSnapshot{{NodeID: "n1", Healthy: true, Inflight: 1}}

	candidate, routeErr := b.Route(context.Background(), req, nodes)
	require.NoError(t, routeErr)
	assert.Equal(t, "n1", candidate.Node.NodeID)
}

func containsReason(reasons []string, prefix string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, prefix) {
			return true
		}
	}
	return false
}

var _ objective.Plugin = slowObjective{}
