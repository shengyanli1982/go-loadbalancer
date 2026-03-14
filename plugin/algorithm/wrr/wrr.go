package wrr

import (
	"fmt"
	"sort"
	"sync"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// pluginName 是 wrr 插件注册名。
const pluginName = "wrr"

const (
	reasonAlgorithmWRR     = "algorithm=wrr"
	reasonSmoothWeightedRR = "selected_by_smooth_weighted_round_robin"
	reasonWeightFallback   = "filled_by_weight_order_fallback"
)

// Plugin 实现 smooth weighted round robin 算法。
type Plugin struct {
	mu       sync.Mutex
	currents map[string]int
}

func init() {
	registry.MustRegisterAlgorithm(&Plugin{})
}

// Name 返回插件注册名。
func (*Plugin) Name() string {
	return pluginName
}

// SelectCandidates 按 smooth weighted round robin 返回候选节点。
func (p *Plugin) SelectCandidates(_ types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK=%d: %w", topK, lberrors.ErrPluginMisconfigured)
	}
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	limit := topK
	if limit > len(nodes) {
		limit = len(nodes)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.currents == nil {
		p.currents = make(map[string]int, len(nodes))
	}

	active := make(map[string]struct{}, len(nodes))
	weights := make(map[string]int, len(nodes))
	totalWeight := 0
	for _, node := range nodes {
		active[node.NodeID] = struct{}{}
		w := effectiveWeight(node)
		weights[node.NodeID] = w
		totalWeight += w
		if _, ok := p.currents[node.NodeID]; !ok {
			p.currents[node.NodeID] = 0
		}
	}
	for nodeID := range p.currents {
		if _, ok := active[nodeID]; !ok {
			delete(p.currents, nodeID)
		}
	}
	if totalWeight <= 0 {
		totalWeight = len(nodes)
	}

	selected := make(map[string]struct{}, limit)
	out := make([]types.Candidate, 0, limit)
	maxSteps := len(nodes) * (limit + 2)

	for step := 0; len(out) < limit && step < maxSteps; step++ {
		bestIdx := -1
		bestID := ""
		bestCurrent := 0

		for i := 0; i < len(nodes); i++ {
			nodeID := nodes[i].NodeID
			if _, exists := selected[nodeID]; exists {
				continue
			}

			p.currents[nodeID] += weights[nodeID]
			current := p.currents[nodeID]
			if bestIdx == -1 || current > bestCurrent || (current == bestCurrent && nodeID < bestID) {
				bestIdx = i
				bestID = nodeID
				bestCurrent = current
			}
		}
		if bestIdx == -1 {
			break
		}

		p.currents[bestID] -= totalWeight
		selected[bestID] = struct{}{}
		out = append(out, types.Candidate{
			Node:   nodes[bestIdx],
			Score:  float64(weights[bestID]),
			Reason: []string{reasonAlgorithmWRR, reasonSmoothWeightedRR},
		})
	}

	if len(out) == limit {
		return out, nil
	}

	remaining := make([]types.NodeSnapshot, 0, len(nodes)-len(out))
	for _, node := range nodes {
		if _, exists := selected[node.NodeID]; exists {
			continue
		}
		remaining = append(remaining, node)
	}
	sort.Slice(remaining, func(i, j int) bool {
		wi := effectiveWeight(remaining[i])
		wj := effectiveWeight(remaining[j])
		if wi != wj {
			return wi > wj
		}
		return remaining[i].NodeID < remaining[j].NodeID
	})
	for i := 0; i < len(remaining) && len(out) < limit; i++ {
		out = append(out, types.Candidate{
			Node:   remaining[i],
			Score:  float64(effectiveWeight(remaining[i])),
			Reason: []string{reasonAlgorithmWRR, reasonWeightFallback},
		})
	}

	return out, nil
}

func effectiveWeight(node types.NodeSnapshot) int {
	if node.StaticWeight <= 0 {
		return 1
	}
	return node.StaticWeight
}

var _ algorithm.Plugin = (*Plugin)(nil)
