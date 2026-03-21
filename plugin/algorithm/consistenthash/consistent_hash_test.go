package consistenthash

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

func TestSelectCandidatesStableForSameKey(t *testing.T) {
	plugin := &Plugin{}
	req := types.RequestContext{SessionID: "session-a"}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", StaticWeight: 1},
		{NodeID: "n2", StaticWeight: 1},
		{NodeID: "n3", StaticWeight: 1},
	}

	first, err := plugin.SelectCandidates(req, nodes, 1)
	require.NoError(t, err)
	require.Len(t, first, 1)

	for i := 0; i < 10; i++ {
		next, nextErr := plugin.SelectCandidates(req, nodes, 1)
		require.NoError(t, nextErr)
		require.Len(t, next, 1)
		assert.Equal(t, first[0].Node.NodeID, next[0].Node.NodeID)
	}
}

func TestSelectCandidatesTopKUnique(t *testing.T) {
	plugin := &Plugin{}
	req := types.RequestContext{SessionID: "session-b"}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", StaticWeight: 1},
		{NodeID: "n2", StaticWeight: 2},
		{NodeID: "n3", StaticWeight: 3},
	}

	out, err := plugin.SelectCandidates(req, nodes, 3)
	require.NoError(t, err)
	require.Len(t, out, 3)

	seen := make(map[string]struct{}, 3)
	for _, candidate := range out {
		_, exists := seen[candidate.Node.NodeID]
		assert.False(t, exists)
		seen[candidate.Node.NodeID] = struct{}{}
	}
}

func TestSelectCandidatesInvalidInput(t *testing.T) {
	plugin := &Plugin{}
	_, err := plugin.SelectCandidates(types.RequestContext{}, nil, 1)
	require.ErrorIs(t, err, lberrors.ErrNoCandidate)

	_, err = plugin.SelectCandidates(types.RequestContext{}, []types.NodeSnapshot{{NodeID: "n1"}}, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, lberrors.ErrPluginMisconfigured)
}

func TestSelectCandidatesRequireSessionID(t *testing.T) {
	plugin := &Plugin{}
	_, err := plugin.SelectCandidates(types.RequestContext{}, []types.NodeSnapshot{{NodeID: "n1", StaticWeight: 1}}, 1)
	require.ErrorIs(t, err, lberrors.ErrNoCandidate)
}
