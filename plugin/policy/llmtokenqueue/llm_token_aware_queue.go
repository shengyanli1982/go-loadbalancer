package llmtokenqueue

import (
	"sort"

	"github.com/shengyanli1982/go-loadbalancer/plugin/policy"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// pluginName 是 llm_token_aware_queue 插件注册名。
const pluginName = "llm_token_aware_queue"

const (
	shortRequestTokenThreshold = 2048

	reasonShortReq = "policy=llm_token_aware_queue_short_request"
	reasonLongReq  = "policy=llm_token_aware_queue_long_request"
	reasonSkipped  = "policy=llm_token_aware_queue_skipped_non_llm"
)

// Plugin 实现 token 感知排队重排策略。
type Plugin struct{}

func init() {
	registry.MustRegisterPolicy(Plugin{})
}

// Name 返回插件注册名。
func (Plugin) Name() string {
	return pluginName
}

// ReRank 根据请求 token 规模在公平性与吞吐之间做折中重排。
func (Plugin) ReRank(req types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	if req.RouteClass != types.RouteLLMPrefill && req.RouteClass != types.RouteLLMDecode {
		for i := range candidates {
			candidates[i].Reason = append(candidates[i].Reason, reasonSkipped)
		}
		return candidates, nil
	}

	totalTokens := req.PromptTokens + req.ExpectedTokens
	shortRequest := totalTokens > 0 && totalTokens <= shortRequestTokenThreshold

	sort.SliceStable(candidates, func(i, j int) bool {
		si := score(candidates[i].Node, shortRequest)
		sj := score(candidates[j].Node, shortRequest)
		if si != sj {
			return si < sj
		}
		return candidates[i].Node.NodeID < candidates[j].Node.NodeID
	})

	reason := reasonLongReq
	if shortRequest {
		reason = reasonShortReq
	}
	for i := range candidates {
		candidates[i].Reason = append(candidates[i].Reason, reason)
	}
	return candidates, nil
}

func score(node types.NodeSnapshot, shortRequest bool) float64 {
	if shortRequest {
		// 短请求优先快进快出，增强对队列深度的惩罚。
		return float64(node.QueueDepth*1000+node.Inflight*100) + node.TPOTms*10 + node.P95LatencyMs
	}
	// 长请求更关注总体吞吐与公平，优先选择 inflight 较低节点。
	return float64(node.Inflight*1000+node.QueueDepth*100) + node.TPOTms*10 + node.P95LatencyMs
}

var _ policy.Plugin = Plugin{}
