package wrr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// TestSelectCandidatesWeightedPreference 验证高权重节点会被更频繁选中。
func TestSelectCandidatesWeightedPreference(t *testing.T) {
	plugin := &Plugin{}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", StaticWeight: 5},
		{NodeID: "n2", StaticWeight: 1},
		{NodeID: "n3", StaticWeight: 1},
	}

	count := map[string]int{
		"n1": 0,
		"n2": 0,
		"n3": 0,
	}

	for i := 0; i < 70; i++ {
		candidates, err := plugin.SelectCandidates(types.RequestContext{}, nodes, 1)
		require.NoError(t, err)
		require.Len(t, candidates, 1)
		count[candidates[0].Node.NodeID]++
	}

	assert.Greater(t, count["n1"], count["n2"])
	assert.Greater(t, count["n1"], count["n3"])
}

// TestSelectCandidatesTopKUnique 验证同一次返回的候选节点不重复。
func TestSelectCandidatesTopKUnique(t *testing.T) {
	plugin := &Plugin{}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", StaticWeight: 3},
		{NodeID: "n2", StaticWeight: 2},
		{NodeID: "n3", StaticWeight: 1},
	}

	candidates, err := plugin.SelectCandidates(types.RequestContext{}, nodes, 3)
	require.NoError(t, err)
	require.Len(t, candidates, 3)

	seen := make(map[string]struct{}, 3)
	for _, candidate := range candidates {
		_, exists := seen[candidate.Node.NodeID]
		assert.False(t, exists)
		seen[candidate.Node.NodeID] = struct{}{}
	}
}

// TestSelectCandidatesDefaultWeight 验证权重缺失时按 1 处理。
func TestSelectCandidatesDefaultWeight(t *testing.T) {
	plugin := &Plugin{}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1", StaticWeight: 0},
		{NodeID: "n2", StaticWeight: -3},
	}

	candidates, err := plugin.SelectCandidates(types.RequestContext{}, nodes, 2)
	require.NoError(t, err)
	require.Len(t, candidates, 2)
	assert.Contains(t, []string{"n1", "n2"}, candidates[0].Node.NodeID)
	assert.Contains(t, []string{"n1", "n2"}, candidates[1].Node.NodeID)
}

// TestSelectCandidatesInvalidInput 验证 wrr 的参数边界。
func TestSelectCandidatesInvalidInput(t *testing.T) {
	plugin := &Plugin{}

	_, err := plugin.SelectCandidates(types.RequestContext{}, nil, 1)
	require.ErrorIs(t, err, lberrors.ErrNoCandidate)

	_, err = plugin.SelectCandidates(types.RequestContext{}, []types.NodeSnapshot{{NodeID: "n1", StaticWeight: 1}}, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, lberrors.ErrPluginMisconfigured)
}
