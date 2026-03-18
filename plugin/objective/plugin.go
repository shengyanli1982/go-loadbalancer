package objective

import (
	"context"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// Plugin 定义目标函数插件契约。
type Plugin interface {
	Name() string
	Choose(req types.RequestContext, candidates []types.Candidate) (types.Candidate, error)
}

// ContextPlugin 是可选扩展接口，用于支持超时与取消语义。
type ContextPlugin interface {
	Plugin
	ChooseWithContext(ctx context.Context, req types.RequestContext, candidates []types.Candidate) (types.Candidate, error)
}

// RouteWeightsAware 是可选扩展接口，用于接收按路由类别配置的权重。
type RouteWeightsAware interface {
	SetRouteWeights(byRouteClass map[types.RouteClass]map[string]int)
}
