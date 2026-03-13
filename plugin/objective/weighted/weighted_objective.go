package weighted

import (
	"math"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/objective"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// pluginName 是 weighted_objective 插件注册名。
const pluginName = "weighted_objective"

// scoreWeights 定义单个路由类型下各指标的加权系数。
type scoreWeights struct {
	queue      float64
	p95Latency float64
	errorRate  float64
	ttft       float64
	tpot       float64
	kvHit      float64
}

var (
	// genericWeights 是通用路由权重。
	genericWeights = scoreWeights{
		queue:      0.5,
		p95Latency: 0.3,
		errorRate:  0.2,
	}
	// llmPrefillWeights 是 LLM prefill 场景权重。
	llmPrefillWeights = scoreWeights{
		queue:      0.20,
		p95Latency: 0.15,
		errorRate:  0.15,
		ttft:       0.25,
		tpot:       0.10,
		kvHit:      0.15,
	}
	// llmDecodeWeights 是 LLM decode 场景权重。
	llmDecodeWeights = scoreWeights{
		queue:      0.20,
		p95Latency: 0.15,
		errorRate:  0.15,
		ttft:       0.10,
		tpot:       0.25,
		kvHit:      0.15,
	}
)

// Plugin 实现加权目标函数。
type Plugin struct{}

func init() {
	registry.MustRegisterObjective(Plugin{})
}

// Name 返回插件注册名。
func (Plugin) Name() string {
	return pluginName
}

// Choose 在候选集中选出加权得分最小的节点。
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

// weightedScore 按路由类型计算节点综合分，分值越小越优。
func weightedScore(routeClass types.RouteClass, node types.NodeSnapshot) float64 {
	weights := genericWeights
	switch routeClass {
	case types.RouteLLMPrefill:
		weights = llmPrefillWeights
	case types.RouteLLMDecode:
		weights = llmDecodeWeights
	}
	return weights.queue*float64(node.QueueDepth) +
		weights.p95Latency*node.P95LatencyMs +
		weights.errorRate*node.ErrorRate*100 +
		weights.ttft*node.TTFTMs +
		weights.tpot*node.TPOTMs -
		weights.kvHit*node.KVCacheHitRate*100
}

// almostEqual 判断两个浮点数是否近似相等。
func almostEqual(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}

var _ objective.Plugin = Plugin{}
