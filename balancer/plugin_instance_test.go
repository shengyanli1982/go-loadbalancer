package balancer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/config"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

func TestNewInstantiatesIndependentStatefulAlgorithmPlugins(t *testing.T) {
	cfg := config.DefaultConfig()

	b1Any, err := New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmRoundRobin),
		config.WithAlgorithm(types.RouteLLMPrefill, config.AlgorithmWeightedRoundRobin),
		config.WithAlgorithm(types.RouteLLMDecode, config.AlgorithmConsistentHash),
	)
	require.NoError(t, err)

	b2Any, err := New(
		cfg,
		config.WithAlgorithm(types.RouteGeneric, config.AlgorithmRoundRobin),
		config.WithAlgorithm(types.RouteLLMPrefill, config.AlgorithmWeightedRoundRobin),
		config.WithAlgorithm(types.RouteLLMDecode, config.AlgorithmConsistentHash),
	)
	require.NoError(t, err)

	b1 := b1Any.(*a2xBalancer)
	b2 := b2Any.(*a2xBalancer)

	require.NotSame(t, b1.routeAlgorithms[types.RouteGeneric], b2.routeAlgorithms[types.RouteGeneric])
	require.NotSame(t, b1.routeAlgorithms[types.RouteLLMPrefill], b2.routeAlgorithms[types.RouteLLMPrefill])
	require.NotSame(t, b1.routeAlgorithms[types.RouteLLMDecode], b2.routeAlgorithms[types.RouteLLMDecode])
}
