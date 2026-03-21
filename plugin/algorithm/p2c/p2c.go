package p2c

import (
	"fmt"
	"sync/atomic"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm/internal/selectutil"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const pluginName = "p2c"

const (
	reasonCapacity = 10
	p2cMixer       = 11400714819323198485

	reasonAlgorithmP2C         = "algorithm=p2c"
	reasonP2CFromTwoChoices    = "p2c_selected_from_two_choices"
	reasonSelectedFromResidual = "selected_from_sorted_residual"
)

type Plugin struct {
	next uint64
}

func init() {
	registry.MustRegisterAlgorithm(&Plugin{})
}

func (*Plugin) Name() string {
	return pluginName
}

func (p *Plugin) SelectCandidates(_ types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK=%d: %w", topK, lberrors.ErrPluginMisconfigured)
	}
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	limit := min(topK, len(nodes))
	selected := make([]types.Candidate, 0, limit)
	reasonBuffer := make([]string, limit*reasonCapacity)

	firstIdx := p.pickByTwoChoicesIndex(nodes)
	first := nodes[firstIdx]
	firstReason := reasonBuffer[:2:reasonCapacity]
	firstReason[0] = reasonAlgorithmP2C
	firstReason[1] = reasonP2CFromTwoChoices
	selected = append(selected, types.Candidate{
		Node:   first,
		Score:  nodeScorePtr(&first),
		Reason: firstReason,
	})
	if limit == 1 {
		return selected, nil
	}

	remaining := selectutil.SelectTopKExcludeNodeIDIndices(nodes, first.NodeID, limit-1)
	for _, idx := range remaining {
		node := nodes[idx]
		reasonOffset := len(selected) * reasonCapacity
		reason := reasonBuffer[reasonOffset : reasonOffset+2 : reasonOffset+reasonCapacity]
		reason[0] = reasonAlgorithmP2C
		reason[1] = reasonSelectedFromResidual
		selected = append(selected, types.Candidate{
			Node:   node,
			Score:  nodeScorePtr(&node),
			Reason: reason,
		})
	}
	return selected, nil
}

func (p *Plugin) pickByTwoChoicesIndex(nodes []types.NodeSnapshot) int {
	if len(nodes) == 1 {
		return 0
	}

	seed := atomic.AddUint64(&p.next, 1) - 1
	i := int(seed % uint64(len(nodes)))
	j := int(((seed * p2cMixer) + 1) % uint64(len(nodes)))
	if i == j {
		j = (j + 1) % len(nodes)
	}

	a, b := nodes[i], nodes[j]
	if selectutil.LessNode(a, b) {
		return i
	}
	return j
}

func nodeScorePtr(node *types.NodeSnapshot) float64 {
	return float64(node.Inflight*10000+node.QueueDepth*100) + node.P95LatencyMs + node.ErrorRate*1000
}

var _ algorithm.Plugin = (*Plugin)(nil)
