package p2c

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

func TestSelectCandidatesTopKAndNoMutation(t *testing.T) {
	plugin := &Plugin{}
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

func TestSelectCandidatesIgnoresRequestSemantics(t *testing.T) {
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", Inflight: 5, QueueDepth: 10},
		{NodeID: "n2", Inflight: 1, QueueDepth: 2},
		{NodeID: "n3", Inflight: 3, QueueDepth: 4},
	}

	pluginA := &Plugin{}
	pluginB := &Plugin{}

	first, err := pluginA.SelectCandidates(types.RequestContext{
		RequestID: "req-a",
		SessionID: "session-a",
		TenantID:  "tenant-a",
	}, nodes, 2)
	require.NoError(t, err)
	require.Len(t, first, 2)

	second, err := pluginB.SelectCandidates(types.RequestContext{
		RequestID: "req-b",
		SessionID: "session-b",
		TenantID:  "tenant-b",
	}, nodes, 2)
	require.NoError(t, err)
	require.Len(t, second, 2)

	assert.Equal(t, first[0].Node.NodeID, second[0].Node.NodeID)
	assert.Equal(t, first[1].Node.NodeID, second[1].Node.NodeID)
}
