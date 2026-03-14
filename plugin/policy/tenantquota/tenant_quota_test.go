package tenantquota

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// TestReRankWithQuota 验证配置配额后会过滤超限节点。
func TestReRankWithQuota(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{
		Metadata: map[string]string{
			MetadataMaxInflightKey: "3",
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

// TestReRankInvalidQuota 验证非法配额值会返回错误。
func TestReRankInvalidQuota(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{
		Metadata: map[string]string{MetadataMaxQueueKey: "bad"},
	}
	_, err := plugin.ReRank(req, []types.Candidate{{Node: types.NodeSnapshot{NodeID: "n1"}}})
	require.Error(t, err)
}
