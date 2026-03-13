package objective

import "go-loadbalancer/types"

// Plugin 定义目标函数插件契约。
type Plugin interface {
	Name() string
	Choose(req types.RequestContext, candidates []types.Candidate) (types.Candidate, error)
}
