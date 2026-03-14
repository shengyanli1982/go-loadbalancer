package rr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// TestSelectCandidatesRotation 验证 rr 会按调用轮次轮转起点。
func TestSelectCandidatesRotation(t *testing.T) {
	plugin := &Plugin{}
	nodes := []types.NodeSnapshot{
		{NodeID: "n1"},
		{NodeID: "n2"},
		{NodeID: "n3"},
	}

	got1, err := plugin.SelectCandidates(types.RequestContext{}, nodes, 2)
	require.NoError(t, err)
	require.Len(t, got1, 2)
	assert.Equal(t, "n1", got1[0].Node.NodeID)
	assert.Equal(t, "n2", got1[1].Node.NodeID)

	got2, err := plugin.SelectCandidates(types.RequestContext{}, nodes, 2)
	require.NoError(t, err)
	require.Len(t, got2, 2)
	assert.Equal(t, "n2", got2[0].Node.NodeID)
	assert.Equal(t, "n3", got2[1].Node.NodeID)

	got3, err := plugin.SelectCandidates(types.RequestContext{}, nodes, 2)
	require.NoError(t, err)
	require.Len(t, got3, 2)
	assert.Equal(t, "n3", got3[0].Node.NodeID)
	assert.Equal(t, "n1", got3[1].Node.NodeID)
}

// TestSelectCandidatesInvalidInput 验证 rr 的参数边界。
func TestSelectCandidatesInvalidInput(t *testing.T) {
	plugin := &Plugin{}

	_, err := plugin.SelectCandidates(types.RequestContext{}, nil, 1)
	require.ErrorIs(t, err, lberrors.ErrNoCandidate)

	_, err = plugin.SelectCandidates(types.RequestContext{}, []types.NodeSnapshot{{NodeID: "n1"}}, 0)
	require.Error(t, err)
	assert.ErrorIs(t, err, lberrors.ErrPluginMisconfigured)
}
