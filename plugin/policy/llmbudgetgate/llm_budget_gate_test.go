package llmbudgetgate

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

func TestReRankSkipNonLLM(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{RouteClass: types.RouteGeneric}
	candidates := []types.Candidate{{Node: types.NodeSnapshot{NodeID: "n1"}}}

	out, err := plugin.ReRank(req, candidates)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Reason, reasonSkippedNonLLM)
}

func TestReRankSkipWhenNoBudgetConfigured(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{RouteClass: types.RouteLLMPrefill}
	candidates := []types.Candidate{{Node: types.NodeSnapshot{NodeID: "n1"}}}

	out, err := plugin.ReRank(req, candidates)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Contains(t, out[0].Reason, reasonSkippedNoRule)
}

func TestReRankRejectByTotalTokenBudget(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{
		RouteClass:           types.RouteLLMPrefill,
		PromptTokens:         1024,
		ExpectedTokens:       1024,
		BudgetMaxTotalTokens: 512,
	}
	candidates := []types.Candidate{{Node: types.NodeSnapshot{NodeID: "n1"}}}

	out, err := plugin.ReRank(req, candidates)
	require.Nil(t, out)
	require.Error(t, err)
	assert.ErrorIs(t, err, lberrors.ErrNoCandidate)
	assert.Contains(t, err.Error(), "reject=total_tokens")
}

func TestReRankFilterByNodeBudgets(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{
		RouteClass:          types.RouteLLMDecode,
		BudgetMaxInflight:   4,
		BudgetMaxQueueDepth: 8,
	}
	candidates := []types.Candidate{
		{Node: types.NodeSnapshot{NodeID: "n1", Inflight: 10, QueueDepth: 1}},
		{Node: types.NodeSnapshot{NodeID: "n2", Inflight: 2, QueueDepth: 20}},
		{Node: types.NodeSnapshot{NodeID: "n3", Inflight: 2, QueueDepth: 4}},
	}

	out, err := plugin.ReRank(req, candidates)
	require.NoError(t, err)
	require.Len(t, out, 1)
	assert.Equal(t, "n3", out[0].Node.NodeID)
	assert.Contains(t, out[0].Reason, reasonEnforced)
}

func TestReRankRejectWhenAllCandidatesExceedNodeBudget(t *testing.T) {
	plugin := Plugin{}
	req := types.RequestContext{
		RouteClass:          types.RouteLLMDecode,
		BudgetMaxInflight:   1,
		BudgetMaxQueueDepth: 1,
	}
	candidates := []types.Candidate{{Node: types.NodeSnapshot{NodeID: "n1", Inflight: 2, QueueDepth: 2}}}

	out, err := plugin.ReRank(req, candidates)
	require.Nil(t, out)
	require.Error(t, err)
	assert.ErrorIs(t, err, lberrors.ErrNoCandidate)
	assert.Contains(t, err.Error(), "reject=node_budget")
}
