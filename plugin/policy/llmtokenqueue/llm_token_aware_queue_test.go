package llmtokenqueue

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// TestReRankShortRequestQueueFirst 验证短请求场景优先低队列节点。
func TestReRankShortRequestQueueFirst(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{
		RouteClass:     types.RouteLLMPrefill,
		PromptTokens:   128,
		ExpectedTokens: 128,
	}
	candidates := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1", QueueDepth: 10, Inflight: 1}},
		{Node: types.NodeSnapshot{NodeID: "n2", QueueDepth: 1, Inflight: 5}},
	}

	out, err := plugin.ReRank(req, candidates)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "n2", out[0].Node.NodeID)
}

// TestReRankLongRequestInflightFirst 验证长请求场景优先低 inflight 节点。
func TestReRankLongRequestInflightFirst(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{
		RouteClass:     types.RouteLLMDecode,
		PromptTokens:   8192,
		ExpectedTokens: 4096,
	}
	candidates := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1", QueueDepth: 1, Inflight: 10}},
		{Node: types.NodeSnapshot{NodeID: "n2", QueueDepth: 8, Inflight: 2}},
	}

	out, err := plugin.ReRank(req, candidates)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "n2", out[0].Node.NodeID)
}

// TestReRankSkipNonLLM 验证非 LLM 路由不会重排。
func TestReRankSkipNonLLM(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{RouteClass: types.RouteGeneric}
	candidates := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1", QueueDepth: 100}},
		{Node: types.NodeSnapshot{NodeID: "n2", QueueDepth: 1}},
	}

	out, err := plugin.ReRank(req, candidates)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "n1", out[0].Node.NodeID)
	assert.Equal(t, "n2", out[1].Node.NodeID)
}
