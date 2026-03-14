package leastrequest

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// TestSelectCandidatesOrder 验证 lr 的候选排序结果。
func TestSelectCandidatesOrder(t *testing.T) {
	plugin := Plugin{}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Inflight: 5, QueueDepth: 2},
		{NodeID: "n2", Inflight: 1, QueueDepth: 5},
		{NodeID: "n3", Inflight: 1, QueueDepth: 1},
	}

	candidates, err := plugin.SelectCandidates(types.RequestContext{}, nodes, 2)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	assert.Equal(t, "n3", candidates[0].Node.NodeID)
	assert.Equal(t, "n2", candidates[1].Node.NodeID)
}
