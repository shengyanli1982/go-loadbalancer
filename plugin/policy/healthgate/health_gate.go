package healthgate

import (
	"go-loadbalancer/plugin/policy"
	"go-loadbalancer/registry"
	"go-loadbalancer/types"
)

const pluginName = "health_gate"

// Plugin 实现健康过滤策略。
type Plugin struct{}

func init() {
	registry.MustRegisterPolicy(Plugin{})
}

func (Plugin) Name() string {
	return pluginName
}

func (Plugin) ReRank(_ types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error) {
	filtered := make([]types.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if !candidate.Node.Healthy {
			continue
		}
		candidate.Reason = append(candidate.Reason, "policy=health_gate")
		filtered = append(filtered, candidate)
	}
	if len(filtered) == 0 {
		return nil, nil
	}
	return filtered, nil
}

var _ policy.Plugin = Plugin{}
