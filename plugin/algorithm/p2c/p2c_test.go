package p2c

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// TestSelectCandidatesTopKAndNoMutation 验证 topK 生效且输入切片不被修改。
func TestSelectCandidatesTopKAndNoMutation(t *testing.T) {
	plugin := Plugin{}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Inflight: 5, QueueDepth: 10},
		{NodeID: "n2", Inflight: 1, QueueDepth: 2},
		{NodeID: "n3", Inflight: 3, QueueDepth: 4},
	}
	origin := append([]types.NodeSnapshot(nil), nodes...)

	candidates, err := plugin.SelectCandidates(types.RequestContext{RequestID: "req-1"}, nodes, 2)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	assert.Equal(t, origin, nodes)
}
