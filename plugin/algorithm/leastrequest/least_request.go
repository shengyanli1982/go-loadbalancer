package leastrequest

import (
	"fmt"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm/internal/selectutil"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// pluginName 是 lr 插件注册名。
const (
	pluginName = "lr"

	reasonAlgorithmLeastRequest      = "algorithm=lr"
	reasonSortedByInflightQueueError = "sorted_by_inflight_queue_latency_error"
	reasonCapacity                   = 4
)

// Plugin 实现 lr 算法。
type Plugin struct{}

func init() {
	registry.MustRegisterAlgorithm(Plugin{})
}

// Name 返回插件注册名。
func (Plugin) Name() string {
	return pluginName
}

// SelectCandidates 按 inflight/queue/延迟/错误率优先级选择候选节点。
func (Plugin) SelectCandidates(_ types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK=%d: %w", topK, lberrors.ErrPluginMisconfigured)
	}
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	selectedIdx := selectutil.SelectTopKIndices(nodes, topK)
	limit := len(selectedIdx)
	out := make([]types.Candidate, 0, limit)
	reasonBuffer := make([]string, limit*reasonCapacity)
	for i := range limit {
		reasonOffset := i * reasonCapacity
		reason := reasonBuffer[reasonOffset : reasonOffset+2 : reasonOffset+reasonCapacity]
		reason[0] = reasonAlgorithmLeastRequest
		reason[1] = reasonSortedByInflightQueueError

		node := nodes[selectedIdx[i]]
		out = append(out, types.Candidate{
			Node:   node,
			Score:  float64(node.Inflight*10000 + node.QueueDepth*100),
			Reason: reason,
		})
	}
	return out, nil
}

var _ algorithm.Plugin = Plugin{}
