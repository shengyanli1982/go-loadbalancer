package tenantquota

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

func TestReRankWithQuota(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{
		Metadata: map[string]string{
			"tenant_quota_max_inflight": "3",
		},
	}
	candidates := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1", Inflight: 2}},
		{Node: types.NodeSnapshot{NodeID: "n2", Inflight: 5}},
	}
	out, err := plugin.ReRank(req, candidates)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "n1", out[0].Node.NodeID)
}

func TestReRankInvalidQuota(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{
		Metadata: map[string]string{"tenant_quota_max_queue": "bad"},
	}
	_, err := plugin.ReRank(req, []types.Candidate{{Node: types.NodeSnapshot{NodeID: "n1"}}})
	require.Error(t, err)
}
