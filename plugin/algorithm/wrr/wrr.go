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
	mu    sync.Mutex
	state topologyState
}

type topologyState struct {
	nodeIDs     []string
	weights     []int
	currents    []int
	canonical   []int
	totalWeight int
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

	p.ensureState(nodes)

	selectedCanonical := make([]bool, len(nodes))
	out := make([]types.Candidate, 0, limit)
	for len(out) < limit {
		bestIdx := -1
		bestCurrent := 0

		for i := 0; i < len(nodes); i++ {
			canonicalIdx := p.state.canonical[i]
			if selectedCanonical[canonicalIdx] {
				continue
			}

			p.state.currents[i] += p.state.weights[i]
			current := p.state.currents[i]
			if bestIdx == -1 || current > bestCurrent || (current == bestCurrent && p.state.nodeIDs[i] < p.state.nodeIDs[bestIdx]) {
				bestIdx = i
				bestCurrent = current
			}
		}
		if bestIdx == -1 {
			break
		}

		p.state.currents[bestIdx] -= p.state.totalWeight
		selectedCanonical[p.state.canonical[bestIdx]] = true
		out = append(out, types.Candidate{
			Node:   nodes[bestIdx],
			Score:  float64(p.state.weights[bestIdx]),
			Reason: []string{reasonAlgorithmWRR, reasonSmoothWeightedRR},
		})
	}

	if len(out) == limit {
		return out, nil
	}

	remaining := make([]int, 0, len(nodes)-len(out))
	for i := 0; i < len(nodes); i++ {
		if selectedCanonical[p.state.canonical[i]] {
			continue
		}
		remaining = append(remaining, i)
	}
	sort.Slice(remaining, func(i, j int) bool {
		wi := p.state.weights[remaining[i]]
		wj := p.state.weights[remaining[j]]
		if wi != wj {
			return wi > wj
		}
		return p.state.nodeIDs[remaining[i]] < p.state.nodeIDs[remaining[j]]
	})
	for i := 0; i < len(remaining) && len(out) < limit; i++ {
		idx := remaining[i]
		selectedCanonical[p.state.canonical[idx]] = true
		out = append(out, types.Candidate{
			Node:   nodes[idx],
			Score:  float64(p.state.weights[idx]),
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

func (p *Plugin) ensureState(nodes []types.NodeSnapshot) {
	if len(p.state.nodeIDs) == len(nodes) {
		same := true
		for i := 0; i < len(nodes); i++ {
			if p.state.nodeIDs[i] != nodes[i].NodeID || p.state.weights[i] != effectiveWeight(nodes[i]) {
				same = false
				break
			}
		}
		if same {
			return
		}
	}

	n := len(nodes)
	state := topologyState{
		nodeIDs:   make([]string, n),
		weights:   make([]int, n),
		currents:  make([]int, n),
		canonical: make([]int, n),
	}
	total := 0
	firstIndexByNodeID := make(map[string]int, n)
	for i := 0; i < n; i++ {
		state.nodeIDs[i] = nodes[i].NodeID
		state.weights[i] = effectiveWeight(nodes[i])
		if first, ok := firstIndexByNodeID[state.nodeIDs[i]]; ok {
			state.canonical[i] = first
		} else {
			firstIndexByNodeID[state.nodeIDs[i]] = i
			state.canonical[i] = i
		}
		total += state.weights[i]
	}
	if total <= 0 {
		total = n
	}
	state.totalWeight = total
	p.state = state
}

var _ algorithm.Plugin = (*Plugin)(nil)
