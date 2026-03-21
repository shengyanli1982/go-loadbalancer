package weighted

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/config"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// TestChooseCandidate 验证加权目标函数会选择更优候选。
func TestChooseCandidate(t *testing.T) {
	plugin := &Plugin{}
	candidates := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1", QueueDepth: 10, P95LatencyMs: 20, ErrorRate: 0.05}},
		{Node: types.NodeSnapshot{NodeID: "n2", QueueDepth: 1, P95LatencyMs: 10, ErrorRate: 0.01}},
	}
	candidate, err := plugin.Choose(types.RequestContext{RouteClass: types.RouteGeneric}, candidates)
	require.NoError(t, err)
	assert.Equal(t, "n2", candidate.Node.NodeID)
}

func TestChooseLLMPrefillDefaultIgnoresKVHit(t *testing.T) {
	plugin := &Plugin{}
	candidates := []types.Candidate{
		{
			Node: types.NodeSnapshot{
				NodeID:         "n-low-ttft",
				QueueDepth:     1,
				P95LatencyMs:   10,
				ErrorRate:      0.01,
				TTFTms:         20,
				TPOTms:         10,
				KVCacheHitRate: 0.10,
			},
		},
		{
			Node: types.NodeSnapshot{
				NodeID:         "n-high-kv",
				QueueDepth:     1,
				P95LatencyMs:   10,
				ErrorRate:      0.01,
				TTFTms:         40,
				TPOTms:         10,
				KVCacheHitRate: 0.99,
			},
		},
	}

	candidate, err := plugin.Choose(types.RequestContext{RouteClass: types.RouteLLMPrefill}, candidates)
	require.NoError(t, err)
	assert.Equal(t, "n-low-ttft", candidate.Node.NodeID)
}

func TestSetRouteWeightsAllowsExplicitKVHitOptIn(t *testing.T) {
	plugin := &Plugin{}
	plugin.SetRouteWeights(map[types.RouteClass]map[string]int{
		types.RouteLLMPrefill: {
			config.MetricKVHit: 10000,
		},
	})

	candidates := []types.Candidate{
		{
			Node: types.NodeSnapshot{
				NodeID:         "n-low-kv",
				QueueDepth:     1,
				P95LatencyMs:   10,
				ErrorRate:      0.01,
				TTFTms:         20,
				TPOTms:         10,
				KVCacheHitRate: 0.10,
			},
		},
		{
			Node: types.NodeSnapshot{
				NodeID:         "n-high-kv",
				QueueDepth:     10,
				P95LatencyMs:   50,
				ErrorRate:      0.10,
				TTFTms:         50,
				TPOTms:         20,
				KVCacheHitRate: 0.99,
			},
		},
	}

	candidate, err := plugin.Choose(types.RequestContext{RouteClass: types.RouteLLMPrefill}, candidates)
	require.NoError(t, err)
	assert.Equal(t, "n-high-kv", candidate.Node.NodeID)
}
