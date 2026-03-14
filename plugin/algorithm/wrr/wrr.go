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

// Plugin 实现 weighted round robin 算法。
type Plugin struct {
	mu    sync.Mutex
	state topologyState
}

type nodeState struct {
	nodeID string
	weight int
}

type topologyState struct {
	signature   []nodeState
	nodeIDs     []string
	weights     []int
	currents    []int
	nodeIndices []int
	weightOrder []int
	totalWeight int
}

func init() {
	registry.MustRegisterAlgorithm(&Plugin{})
}

// Name 返回插件注册名。
func (*Plugin) Name() string {
	return pluginName
}

// SelectCandidates 返回候选节点：首个候选由 smooth WRR 选出，其余按权重序补齐。
func (p *Plugin) SelectCandidates(_ types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK=%d: %w", topK, lberrors.ErrPluginMisconfigured)
	}
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.ensureState(nodes)
	if len(p.state.nodeIDs) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	limit := topK
	if limit > len(p.state.nodeIDs) {
		limit = len(p.state.nodeIDs)
	}

	reasonBuffer := make([]string, limit*2)
	out := make([]types.Candidate, 0, limit)

	bestIdx := -1
	bestCurrent := 0
	for i := 0; i < len(p.state.nodeIDs); i++ {
		p.state.currents[i] += p.state.weights[i]
		current := p.state.currents[i]
		if bestIdx == -1 || current > bestCurrent || (current == bestCurrent && p.state.nodeIDs[i] < p.state.nodeIDs[bestIdx]) {
			bestIdx = i
			bestCurrent = current
		}
	}
	if bestIdx == -1 {
		return nil, lberrors.ErrNoCandidate
	}
	p.state.currents[bestIdx] -= p.state.totalWeight

	firstReason := reasonBuffer[:2:2]
	firstReason[0] = reasonAlgorithmWRR
	firstReason[1] = reasonSmoothWeightedRR
	out = append(out, types.Candidate{
		Node:   nodes[p.state.nodeIndices[bestIdx]],
		Score:  float64(p.state.weights[bestIdx]),
		Reason: firstReason,
	})

	for _, idx := range p.state.weightOrder {
		if len(out) >= limit {
			break
		}
		if idx == bestIdx {
			continue
		}
		reasonOffset := len(out) * 2
		reason := reasonBuffer[reasonOffset : reasonOffset+2 : reasonOffset+2]
		reason[0] = reasonAlgorithmWRR
		reason[1] = reasonWeightFallback
		out = append(out, types.Candidate{
			Node:   nodes[p.state.nodeIndices[idx]],
			Score:  float64(p.state.weights[idx]),
			Reason: reason,
		})
	}

	return out, nil
}

func effectiveWeight(node *types.NodeSnapshot) int {
	if node.StaticWeight <= 0 {
		return 1
	}
	return node.StaticWeight
}

func (p *Plugin) ensureState(nodes []types.NodeSnapshot) {
	if nodesEqualState(nodes, p.state.signature) {
		return
	}

	state := topologyState{
		signature: make([]nodeState, len(nodes)),
	}

	firstIndexByNodeID := make(map[string]int, len(nodes))
	for i := 0; i < len(nodes); i++ {
		node := &nodes[i]
		weight := effectiveWeight(node)
		state.signature[i] = nodeState{
			nodeID: node.NodeID,
			weight: weight,
		}
		if _, exists := firstIndexByNodeID[node.NodeID]; !exists {
			firstIndexByNodeID[node.NodeID] = i
			state.nodeIDs = append(state.nodeIDs, node.NodeID)
			state.weights = append(state.weights, weight)
			state.currents = append(state.currents, 0)
			state.nodeIndices = append(state.nodeIndices, i)
		}
	}

	state.weightOrder = make([]int, len(state.nodeIDs))
	for i := 0; i < len(state.nodeIDs); i++ {
		state.weightOrder[i] = i
	}
	sort.Slice(state.weightOrder, func(i, j int) bool {
		wi := state.weights[state.weightOrder[i]]
		wj := state.weights[state.weightOrder[j]]
		if wi != wj {
			return wi > wj
		}
		return state.nodeIDs[state.weightOrder[i]] < state.nodeIDs[state.weightOrder[j]]
	})

	total := 0
	for i := 0; i < len(state.weights); i++ {
		total += state.weights[i]
	}
	if total <= 0 {
		total = len(state.weights)
	}
	state.totalWeight = total
	p.state = state
}

func nodesEqualState(nodes []types.NodeSnapshot, signature []nodeState) bool {
	if len(nodes) != len(signature) {
		return false
	}
	for i := 0; i < len(nodes); i++ {
		if nodes[i].NodeID != signature[i].nodeID {
			return false
		}
		if effectiveWeight(&nodes[i]) != signature[i].weight {
			return false
		}
	}
	return true
}

var _ algorithm.Plugin = (*Plugin)(nil)
