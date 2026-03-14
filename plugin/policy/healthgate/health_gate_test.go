package healthgate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// TestReRankFiltersUnhealthyCandidates 验证健康门策略会过滤不健康节点。
func TestReRankFiltersUnhealthyCandidates(t *testing.T) {
	plugin := Plugin{}
	candidates := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1", Healthy: true}},
		{Node: types.NodeSnapshot{NodeID: "n2", Healthy: false}},
	}
	out, err := plugin.ReRank(types.RequestContext{}, candidates)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "n1", out[0].Node.NodeID)
}
