package llmbudgetgate

import (
	"fmt"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/policy"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const pluginName = "llm_budget_gate"

const (
	reasonSkippedNonLLM = "policy=llm_budget_gate_skipped_non_llm"
	reasonSkippedNoRule = "policy=llm_budget_gate_skipped_no_budget"
	reasonEnforced      = "policy=llm_budget_gate"
)

// Plugin enforces request/node budgets for LLM routing.
type Plugin struct{}

func init() {
	registry.MustRegisterPolicy(Plugin{})
}

func (Plugin) Name() string {
	return pluginName
}

func (Plugin) IsHardConstraint() bool {
	return true
}

func (Plugin) ReRank(req types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}
	if req.RouteClass != types.RouteLLMPrefill && req.RouteClass != types.RouteLLMDecode {
		for i := range candidates {
			candidates[i].Reason = append(candidates[i].Reason, reasonSkippedNonLLM)
		}
		return candidates, nil
	}

	totalBudget := req.BudgetMaxTotalTokens
	inflightBudget := req.BudgetMaxInflight
	queueBudget := req.BudgetMaxQueueDepth
	if totalBudget <= 0 && inflightBudget <= 0 && queueBudget <= 0 {
		for i := range candidates {
			candidates[i].Reason = append(candidates[i].Reason, reasonSkippedNoRule)
		}
		return candidates, nil
	}

	totalTokens := req.PromptTokens + req.ExpectedTokens
	if totalBudget > 0 && totalTokens > totalBudget {
		return nil, fmt.Errorf("policy=%s reject=total_tokens total_tokens=%d budget=%d: %w", pluginName, totalTokens, totalBudget, lberrors.ErrNoCandidate)
	}

	out := candidates[:0]
	for i := range candidates {
		candidate := candidates[i]
		if inflightBudget > 0 && candidate.Node.Inflight > inflightBudget {
			continue
		}
		if queueBudget > 0 && candidate.Node.QueueDepth > queueBudget {
			continue
		}
		candidate.Reason = append(candidate.Reason, reasonEnforced)
		out = append(out, candidate)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("policy=%s reject=node_budget inflight_budget=%d queue_budget=%d: %w", pluginName, inflightBudget, queueBudget, lberrors.ErrNoCandidate)
	}
	return out, nil
}

var _ policy.Plugin = Plugin{}
var _ policy.HardConstraintPlugin = Plugin{}
