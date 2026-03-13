package leastrequest

import (
	"fmt"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm/internal/selectutil"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const pluginName = "least_request"

// Plugin 实现 least_request 算法。
type Plugin struct{}

func init() {
	registry.MustRegisterAlgorithm(Plugin{})
}

func (Plugin) Name() string {
	return pluginName
}

func (Plugin) SelectCandidates(_ types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK=%d: %w", topK, lberrors.ErrPluginMisconfigured)
	}
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	selected := selectutil.SelectTopK(nodes, topK)
	limit := len(selected)
	out := make([]types.Candidate, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, types.Candidate{
			Node:  selected[i],
			Score: float64(selected[i].Inflight*10000 + selected[i].QueueDepth*100),
			Reason: []string{
				"algorithm=least_request",
				"sorted_by_inflight_queue_latency_error",
			},
		})
	}
	return out, nil
}

var _ algorithm.Plugin = Plugin{}
