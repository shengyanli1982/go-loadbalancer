package objective

import "github.com/shengyanli1982/go-loadbalancer/types"

// Plugin 定义目标函数插件契约。
type Plugin interface {
	Name() string
	Choose(req types.RequestContext, candidates []types.Candidate) (types.Candidate, error)
}
