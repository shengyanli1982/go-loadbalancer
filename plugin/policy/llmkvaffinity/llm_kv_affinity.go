package llmkvaffinity

import (
	"sort"

	"github.com/shengyanli1982/go-loadbalancer/plugin/policy"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// pluginName 是 llm_kv_affinity 插件注册名。
const pluginName = "llm_kv_affinity"

const (
	reasonApplied = "policy=llm_kv_affinity"
	reasonSkipped = "policy=llm_kv_affinity_skipped_non_llm"
)

// Plugin 实现 KV 命中优先重排策略。
type Plugin struct{}

func init() {
	registry.MustRegisterPolicy(Plugin{})
}

// Name 返回插件注册名。
func (Plugin) Name() string {
	return pluginName
}

// ReRank 对 LLM 路由按 KV 命中率优先重排。
func (Plugin) ReRank(req types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	out := append([]types.Candidate(nil), candidates...)
	if req.RouteClass != types.RouteLLMPrefill && req.RouteClass != types.RouteLLMDecode {
		for i := range out {
			out[i].Reason = append(out[i].Reason, reasonSkipped)
		}
		return out, nil
	}

	sort.SliceStable(out, func(i, j int) bool {
		ai, aj := out[i].Node, out[j].Node
		if ai.KVCacheHitRate != aj.KVCacheHitRate {
			return ai.KVCacheHitRate > aj.KVCacheHitRate
		}
		if ai.Inflight != aj.Inflight {
			return ai.Inflight < aj.Inflight
		}
		if ai.QueueDepth != aj.QueueDepth {
			return ai.QueueDepth < aj.QueueDepth
		}
		return ai.NodeID < aj.NodeID
	})
	for i := range out {
		out[i].Reason = append(out[i].Reason, reasonApplied)
	}
	return out, nil
}

var _ policy.Plugin = Plugin{}
