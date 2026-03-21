package weighted

import (
	"math"

	"github.com/shengyanli1982/go-loadbalancer/config"
	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/objective"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const pluginName = "weighted_objective"

type scoreWeights struct {
	queue      float64
	p95Latency float64
	errorRate  float64
	ttft       float64
	tpot       float64
	kvHit      float64
}

var (
	genericWeights = scoreWeights{
		queue:      0.5,
		p95Latency: 0.3,
		errorRate:  0.2,
	}
	llmPrefillWeights = scoreWeights{
		queue:      0.20,
		p95Latency: 0.15,
		errorRate:  0.15,
		ttft:       0.25,
		tpot:       0.10,
		kvHit:      0.15,
	}
	llmDecodeWeights = scoreWeights{
		queue:      0.20,
		p95Latency: 0.15,
		errorRate:  0.15,
		ttft:       0.10,
		tpot:       0.25,
		kvHit:      0.15,
	}
)

type Plugin struct {
	routeWeights map[types.RouteClass]scoreWeights
}

func init() {
	registry.MustRegisterObjective(&Plugin{})
}

func (*Plugin) Name() string {
	return pluginName
}

// SetRouteWeights 将配置中的整数权重(总和 10000)转换为评分系数。
func (p *Plugin) SetRouteWeights(byRouteClass map[types.RouteClass]map[string]int) {
	if len(byRouteClass) == 0 {
		p.routeWeights = nil
		return
	}

	converted := make(map[types.RouteClass]scoreWeights, len(byRouteClass))
	for routeClass, weights := range byRouteClass {
		converted[routeClass] = convertRouteWeights(weights)
	}
	p.routeWeights = converted
}

// Choose 在候选集中选择综合得分最低的节点。
func (p *Plugin) Choose(req types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	if len(candidates) == 0 {
		return types.Candidate{}, lberrors.ErrNoCandidate
	}

	best := candidates[0]
	best.Score = p.weightedScore(req.RouteClass, best.Node)
	for i := 1; i < len(candidates); i++ {
		score := p.weightedScore(req.RouteClass, candidates[i].Node)
		if score < best.Score || (almostEqual(score, best.Score) && candidates[i].Node.NodeID < best.Node.NodeID) {
			best = candidates[i]
			best.Score = score
		}
	}

	best.Reason = append(best.Reason, "objective=weighted_objective")
	return best, nil
}

// weightedScore 计算单个节点得分，分值越小越优。
func (p *Plugin) weightedScore(routeClass types.RouteClass, node types.NodeSnapshot) float64 {
	weights := p.resolveWeights(routeClass)
	return weights.queue*float64(node.QueueDepth) +
		weights.p95Latency*node.P95LatencyMs +
		weights.errorRate*node.ErrorRate*100 +
		weights.ttft*node.TTFTms +
		weights.tpot*node.TPOTms -
		weights.kvHit*node.KVCacheHitRate*100
}

// resolveWeights 返回当前路由类别的有效权重，优先使用外部配置。
func (p *Plugin) resolveWeights(routeClass types.RouteClass) scoreWeights {
	if p.routeWeights != nil {
		if weights, ok := p.routeWeights[routeClass]; ok {
			return weights
		}
	}

	switch routeClass {
	case types.RouteLLMPrefill:
		return llmPrefillWeights
	case types.RouteLLMDecode:
		return llmDecodeWeights
	default:
		return genericWeights
	}
}

// convertRouteWeights 将配置权重映射为浮点系数。
func convertRouteWeights(weights map[string]int) scoreWeights {
	normalize := func(metric string) float64 {
		return float64(weights[metric]) / 10000
	}
	return scoreWeights{
		queue:      normalize(config.MetricQueue),
		p95Latency: normalize(config.MetricP95Latency),
		errorRate:  normalize(config.MetricErrorRate),
		ttft:       normalize(config.MetricTTFT),
		tpot:       normalize(config.MetricTPOT),
		kvHit:      normalize(config.MetricKVHit),
	}
}

// almostEqual 用于浮点比较，避免极小误差导致不稳定排序。
func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

var _ objective.Plugin = (*Plugin)(nil)
var _ objective.RouteWeightsAware = (*Plugin)(nil)
