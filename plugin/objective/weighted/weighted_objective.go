package weighted

import (
	"math"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/objective"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const pluginName = "weighted_objective"

// Plugin 实现加权目标函数。
type Plugin struct{}

func init() {
	registry.MustRegisterObjective(Plugin{})
}

func (Plugin) Name() string {
	return pluginName
}

func (Plugin) Choose(req types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	if len(candidates) == 0 {
		return types.Candidate{}, lberrors.ErrNoCandidate
	}
	best := candidates[0]
	best.Score = weightedScore(req.RouteClass, best.Node)
	for i := 1; i < len(candidates); i++ {
		score := weightedScore(req.RouteClass, candidates[i].Node)
		if score < best.Score || (almostEqual(score, best.Score) && candidates[i].Node.NodeID < best.Node.NodeID) {
			best = candidates[i]
			best.Score = score
		}
	}
	best.Reason = append(best.Reason, "objective=weighted_objective")
	return best, nil
}

func weightedScore(routeClass types.RouteClass, node types.NodeSnapshot) float64 {
	weights := map[string]float64{
		"queue":       0.5,
		"p95_latency": 0.3,
		"error_rate":  0.2,
		"ttft":        0.0,
		"tpot":        0.0,
		"kv_hit":      0.0,
	}
	switch routeClass {
	case types.RouteLLMPrefill:
		weights["queue"] = 0.20
		weights["p95_latency"] = 0.15
		weights["error_rate"] = 0.15
		weights["ttft"] = 0.25
		weights["tpot"] = 0.10
		weights["kv_hit"] = 0.15
	case types.RouteLLMDecode:
		weights["queue"] = 0.20
		weights["p95_latency"] = 0.15
		weights["error_rate"] = 0.15
		weights["ttft"] = 0.10
		weights["tpot"] = 0.25
		weights["kv_hit"] = 0.15
	}
	return weights["queue"]*float64(node.QueueDepth) +
		weights["p95_latency"]*node.P95LatencyMs +
		weights["error_rate"]*node.ErrorRate*100 +
		weights["ttft"]*node.TTFTMs +
		weights["tpot"]*node.TPOTMs -
		weights["kv_hit"]*node.KVCacheHitRate*100
}

func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

var _ objective.Plugin = Plugin{}
