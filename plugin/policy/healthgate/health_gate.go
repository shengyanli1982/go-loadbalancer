package healthgate

import (
	"github.com/shengyanli1982/go-loadbalancer/plugin/policy"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// pluginName 是 health_gate 插件注册名。
const pluginName = "health_gate"

// Plugin 实现健康过滤策略。
type Plugin struct{}

func init() {
	registry.MustRegisterPolicy(Plugin{})
}

// Name 返回插件注册名。
func (Plugin) Name() string {
	return pluginName
}

// ReRank 过滤掉不健康节点并保持剩余候选相对顺序。
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
