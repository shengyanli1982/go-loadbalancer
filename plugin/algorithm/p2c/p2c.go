package p2c

import (
	"fmt"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm/internal/selectutil"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// pluginName 是 p2c 插件注册名。
const pluginName = "p2c"

const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211

	reasonAlgorithmP2C         = "algorithm=p2c"
	reasonP2CFromTwoChoices    = "p2c_selected_from_two_choices"
	reasonSelectedFromResidual = "selected_from_sorted_residual"
)

// Plugin 实现 p2c 算法。
type Plugin struct{}

func init() {
	registry.MustRegisterAlgorithm(Plugin{})
}

// Name 返回插件注册名。
func (Plugin) Name() string {
	return pluginName
}

// SelectCandidates 先执行一次 P2C 选首个候选，再补齐剩余 topK。
func (Plugin) SelectCandidates(req types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK=%d: %w", topK, lberrors.ErrPluginMisconfigured)
	}
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	limit := min(topK, len(nodes))
	selected := make([]types.Candidate, 0, limit)
	reasonBuffer := make([]string, limit*2)

	// 先按 P2C 选出首个候选，后续候选按统一比较器补齐。
	firstIdx := pickByTwoChoicesIndex(req, nodes)
	first := nodes[firstIdx]
	firstReason := reasonBuffer[:2:2]
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
		reasonOffset := len(selected) * 2
		reason := reasonBuffer[reasonOffset : reasonOffset+2 : reasonOffset+2]
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

// pickByTwoChoicesIndex 从两次哈希命中的节点中选出更优者下标。
func pickByTwoChoicesIndex(req types.RequestContext, nodes []types.NodeSnapshot) int {
	if len(nodes) == 1 {
		return 0
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
		return i
	}
	return j
}

// hashRequest 将请求关键字段哈希为稳定的 uint64 值。
func hashRequest(req types.RequestContext) uint64 {
	h := uint64(fnvOffset64)
	h = hashString64a(h, req.RequestID)
	h = hashString64a(h, req.SessionID)
	h = hashString64a(h, req.TenantID)
	h = hashString64a(h, req.Model)
	return h
}

func hashString64a(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	return h
}

// nodeScorePtr 计算节点在候选输出中的展示分值。
func nodeScorePtr(node *types.NodeSnapshot) float64 {
	return float64(node.Inflight*10000+node.QueueDepth*100) + node.P95LatencyMs + node.ErrorRate*1000
}

var _ algorithm.Plugin = Plugin{}
