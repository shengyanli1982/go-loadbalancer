package leastrequest

import (
	"fmt"
	"sort"

	lberrors "go-loadbalancer/errors"
	"go-loadbalancer/plugin/algorithm"
	"go-loadbalancer/registry"
	"go-loadbalancer/types"
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

	copied := append([]types.NodeSnapshot(nil), nodes...)
	sort.Slice(copied, func(i, j int) bool {
		if copied[i].Inflight != copied[j].Inflight {
			return copied[i].Inflight < copied[j].Inflight
		}
		if copied[i].QueueDepth != copied[j].QueueDepth {
			return copied[i].QueueDepth < copied[j].QueueDepth
		}
		if copied[i].P95LatencyMs != copied[j].P95LatencyMs {
			return copied[i].P95LatencyMs < copied[j].P95LatencyMs
		}
		if copied[i].ErrorRate != copied[j].ErrorRate {
			return copied[i].ErrorRate < copied[j].ErrorRate
		}
		return copied[i].NodeID < copied[j].NodeID
	})

	limit := topK
	if limit > len(copied) {
		limit = len(copied)
	}
	out := make([]types.Candidate, 0, limit)
	for i := 0; i < limit; i++ {
		out = append(out, types.Candidate{
			Node:  copied[i],
			Score: float64(copied[i].Inflight*10000 + copied[i].QueueDepth*100),
			Reason: []string{
				"algorithm=least_request",
				"sorted_by_inflight_queue_latency_error",
			},
		})
	}
	return out, nil
}

var _ algorithm.Plugin = Plugin{}
