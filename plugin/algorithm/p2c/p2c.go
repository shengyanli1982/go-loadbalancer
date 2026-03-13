package p2c

import (
	"fmt"
	"hash/fnv"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm/internal/selectutil"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const pluginName = "p2c"

// Plugin 实现 p2c 算法。
type Plugin struct{}

func init() {
	registry.MustRegisterAlgorithm(Plugin{})
}

func (Plugin) Name() string {
	return pluginName
}

func (Plugin) SelectCandidates(req types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK=%d: %w", topK, lberrors.ErrPluginMisconfigured)
	}
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	limit := min(topK, len(nodes))
	selected := make([]types.Candidate, 0, limit)

	// 先按 P2C 选出首个候选，后续候选按统一比较器补齐。
	first := pickByTwoChoices(req, nodes)
	selected = append(selected, types.Candidate{
		Node:  first,
		Score: nodeScore(first),
		Reason: []string{
			"algorithm=p2c",
			"p2c_selected_from_two_choices",
		},
	})
	if limit == 1 {
		return selected, nil
	}

	remaining := selectutil.SelectTopKExcludeNodeID(nodes, first.NodeID, limit-1)
	for _, node := range remaining {
		selected = append(selected, types.Candidate{
			Node:  node,
			Score: nodeScore(node),
			Reason: []string{
				"algorithm=p2c",
				"selected_from_sorted_residual",
			},
		})
	}
	return selected, nil
}

func pickByTwoChoices(req types.RequestContext, nodes []types.NodeSnapshot) types.NodeSnapshot {
	if len(nodes) == 1 {
		return nodes[0]
	}
	// 使用请求哈希生成两个下标，保证相同请求分布相对稳定。
	h := hashRequest(req)
	i := int(h % uint64(len(nodes)))
	j := int((h/uint64(len(nodes)) + 1) % uint64(len(nodes)))
	if i == j {
		j = (j + 1) % len(nodes)
	}
	a, b := nodes[i], nodes[j]
	if selectutil.LessNode(a, b) {
		return a
	}
	return b
}

func hashRequest(req types.RequestContext) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(req.RequestID))
	_, _ = h.Write([]byte(req.SessionID))
	_, _ = h.Write([]byte(req.TenantID))
	_, _ = h.Write([]byte(req.Model))
	return h.Sum64()
}

func nodeScore(node types.NodeSnapshot) float64 {
	return float64(node.Inflight*10000+node.QueueDepth*100) + node.P95LatencyMs + node.ErrorRate*1000
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

var _ algorithm.Plugin = Plugin{}
