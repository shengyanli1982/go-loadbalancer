package llmstageaware

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// TestReRankPrefillTTFTFirst 验证 prefill 阶段按 TTFT 优先。
func TestReRankPrefillTTFTFirst(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{RouteClass: types.RouteLLMPrefill}
	candidates := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1", TTFTms: 120}},
		{Node: types.NodeSnapshot{NodeID: "n2", TTFTms: 40}},
		{Node: types.NodeSnapshot{NodeID: "n3", TTFTms: 70}},
	}

	out, err := plugin.ReRank(req, candidates)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "n2", out[0].Node.NodeID)
	assert.Equal(t, "n3", out[1].Node.NodeID)
	assert.Equal(t, "n1", out[2].Node.NodeID)
}

// TestReRankDecodeTPOTFirst 验证 decode 阶段按 TPOT 优先。
func TestReRankDecodeTPOTFirst(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{RouteClass: types.RouteLLMDecode}
	candidates := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1", TPOTms: 8}},
		{Node: types.NodeSnapshot{NodeID: "n2", TPOTms: 3}},
		{Node: types.NodeSnapshot{NodeID: "n3", TPOTms: 5}},
	}

	out, err := plugin.ReRank(req, candidates)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "n2", out[0].Node.NodeID)
	assert.Equal(t, "n3", out[1].Node.NodeID)
	assert.Equal(t, "n1", out[2].Node.NodeID)
}
