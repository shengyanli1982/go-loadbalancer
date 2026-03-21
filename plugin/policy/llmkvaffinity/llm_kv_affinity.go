package llmkvaffinity

import (
	"sort"
	"strings"

	"github.com/shengyanli1982/go-loadbalancer/plugin/policy"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// pluginName 是 llm_kv_affinity 插件注册名。
const pluginName = "llm_kv_affinity"

const (
	MetadataPreferredNodesKey = "llm_kv_affinity_preferred_nodes"

	reasonApplied = "policy=llm_kv_affinity"
	reasonSkipped = "policy=llm_kv_affinity_skipped_non_llm"
	reasonHinted  = "policy=llm_kv_affinity_request_hint"
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

func (Plugin) PolicyRole() policy.Role {
	return policy.RoleAffinity
}

// ReRank 对 LLM 路由按 KV 命中率优先重排。
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

	preferredNodes := parsePreferredNodes(req.Metadata)
	sort.SliceStable(candidates, func(i, j int) bool {
		ai, aj := candidates[i].Node, candidates[j].Node
		aiPreferred := preferredNodes[ai.NodeID]
		ajPreferred := preferredNodes[aj.NodeID]
		if aiPreferred != ajPreferred {
			return aiPreferred
		}
		if ai.KVCacheHitRate != aj.KVCacheHitRate {
			return ai.KVCacheHitRate > aj.KVCacheHitRate
		}
		if ai.Inflight != aj.Inflight {
			return ai.Inflight < aj.Inflight
		}
		if ai.QueueDepth != aj.QueueDepth {
			return ai.QueueDepth < aj.QueueDepth
		}
		if req.RouteClass == types.RouteLLMPrefill && ai.TTFTms != aj.TTFTms {
			return ai.TTFTms < aj.TTFTms
		}
		if req.RouteClass == types.RouteLLMDecode && ai.TPOTms != aj.TPOTms {
			return ai.TPOTms < aj.TPOTms
		}
		return ai.NodeID < aj.NodeID
	})
	for i := range candidates {
		candidates[i].Reason = append(candidates[i].Reason, reasonApplied)
		if preferredNodes[candidates[i].Node.NodeID] {
			candidates[i].Reason = append(candidates[i].Reason, reasonHinted)
		}
	}
	return candidates, nil
}

func parsePreferredNodes(metadata map[string]string) map[string]bool {
	if len(metadata) == 0 {
		return nil
	}
	raw, ok := metadata[MetadataPreferredNodesKey]
	if !ok || raw == "" {
		return nil
	}

	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
	})
	if len(fields) == 0 {
		return nil
	}

	preferred := make(map[string]bool, len(fields))
	for _, field := range fields {
		nodeID := strings.TrimSpace(field)
		if nodeID == "" {
			continue
		}
		preferred[nodeID] = true
	}
	if len(preferred) == 0 {
		return nil
	}
	return preferred
}

var _ policy.Plugin = Plugin{}
var _ policy.RoleAwarePlugin = Plugin{}
