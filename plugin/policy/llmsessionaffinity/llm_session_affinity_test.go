package llmsessionaffinity

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

func TestReRankSkipNonDecode(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{RouteClass: types.RouteLLMPrefill}
	in := []types.Candidate{{Node: types.NodeSnapshot{NodeID: "n1"}}}

	out, err := plugin.ReRank(req, in)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Reason, reasonSkippedNonDecode)
}

func TestReRankDecodeAffinityHit(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{
		RouteClass: types.RouteLLMDecode,
		Metadata: map[string]string{
			MetadataAffinityNodeKey: "n2",
		},
	}
	in := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1"}},
		{Node: types.NodeSnapshot{NodeID: "n2"}},
	}

	out, err := plugin.ReRank(req, in)
	require.NoError(t, err)
	require.Len(t, out, 2)
	assert.Equal(t, "n2", out[0].Node.NodeID)
	assert.Contains(t, out[0].Reason, reasonHit)
}

func TestReRankDecodeAffinityMissUsesDegradeOrder(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{
		RouteClass: types.RouteLLMDecode,
		Metadata: map[string]string{
			MetadataAffinityNodeKey: "n-not-found",
			MetadataDegradeNodesKey: "n3,n2",
		},
	}
	in := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1"}},
		{Node: types.NodeSnapshot{NodeID: "n2"}},
		{Node: types.NodeSnapshot{NodeID: "n3"}},
	}

	out, err := plugin.ReRank(req, in)
	require.NoError(t, err)
	require.Len(t, out, 3)
	assert.Equal(t, "n3", out[0].Node.NodeID)
	assert.Equal(t, "n2", out[1].Node.NodeID)
	assert.Contains(t, out[0].Reason, reasonDegrade)
	assert.Contains(t, out[1].Reason, reasonDegrade)
	assert.Contains(t, out[0].Reason, reasonMiss)
}
