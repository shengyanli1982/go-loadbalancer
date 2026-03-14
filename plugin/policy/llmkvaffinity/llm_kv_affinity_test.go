package llmkvaffinity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// TestReRankKVHitDesc 验证 LLM 路由会优先高 KV 命中率节点。
func TestReRankKVHitDesc(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{RouteClass: types.RouteLLMPrefill}
	candidates := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1", KVCacheHitRate: 0.2}},
		{Node: types.NodeSnapshot{NodeID: "n2", KVCacheHitRate: 0.8}},
		{Node: types.NodeSnapshot{NodeID: "n3", KVCacheHitRate: 0.5}},
	}

	out, err := plugin.ReRank(req, candidates)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "n2", out[0].Node.NodeID)
	assert.Equal(t, "n3", out[1].Node.NodeID)
	assert.Equal(t, "n1", out[2].Node.NodeID)
}

// TestReRankSkipNonLLM 验证非 LLM 路由不会重排。
func TestReRankSkipNonLLM(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{RouteClass: types.RouteGeneric}
	candidates := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1", KVCacheHitRate: 0.1}},
		{Node: types.NodeSnapshot{NodeID: "n2", KVCacheHitRate: 0.9}},
	}

	out, err := plugin.ReRank(req, candidates)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "n1", out[0].Node.NodeID)
	assert.Equal(t, "n2", out[1].Node.NodeID)
}
