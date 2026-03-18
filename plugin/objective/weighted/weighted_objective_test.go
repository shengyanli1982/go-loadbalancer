package weighted

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
