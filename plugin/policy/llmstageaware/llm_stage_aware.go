package llmstageaware

import (
	"sort"

	"github.com/shengyanli1982/go-loadbalancer/plugin/policy"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// pluginName 是 llm_stage_aware 插件注册名。
const pluginName = "llm_stage_aware"

const (
	reasonPrefill = "policy=llm_stage_aware_prefill_ttft_first"
	reasonDecode  = "policy=llm_stage_aware_decode_tpot_first"
	reasonSkipped = "policy=llm_stage_aware_skipped_non_llm"
)

// Plugin 实现 prefill/decode 阶段感知重排策略。
type Plugin struct{}

func init() {
	registry.MustRegisterPolicy(Plugin{})
}

// Name 返回插件注册名。
func (Plugin) Name() string {
	return pluginName
}

// ReRank 根据 LLM 阶段优先不同指标做重排。
func (Plugin) ReRank(req types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	switch req.RouteClass {
	case types.RouteLLMPrefill:
		sort.SliceStable(candidates, func(i, j int) bool {
			ai, aj := candidates[i].Node, candidates[j].Node
			if ai.TTFTms != aj.TTFTms {
				return ai.TTFTms < aj.TTFTms
			}
			if ai.QueueDepth != aj.QueueDepth {
				return ai.QueueDepth < aj.QueueDepth
			}
			if ai.P95LatencyMs != aj.P95LatencyMs {
				return ai.P95LatencyMs < aj.P95LatencyMs
			}
			return ai.NodeID < aj.NodeID
		})
		for i := range candidates {
			candidates[i].Reason = append(candidates[i].Reason, reasonPrefill)
		}
	case types.RouteLLMDecode:
		sort.SliceStable(candidates, func(i, j int) bool {
			ai, aj := candidates[i].Node, candidates[j].Node
			if ai.TPOTms != aj.TPOTms {
				return ai.TPOTms < aj.TPOTms
			}
			if ai.Inflight != aj.Inflight {
				return ai.Inflight < aj.Inflight
			}
			if ai.P95LatencyMs != aj.P95LatencyMs {
				return ai.P95LatencyMs < aj.P95LatencyMs
			}
			return ai.NodeID < aj.NodeID
		})
		for i := range candidates {
			candidates[i].Reason = append(candidates[i].Reason, reasonDecode)
		}
	default:
		for i := range candidates {
			candidates[i].Reason = append(candidates[i].Reason, reasonSkipped)
		}
	}
	return candidates, nil
}

var _ policy.Plugin = Plugin{}
