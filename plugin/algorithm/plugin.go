package algorithm

import (
	"context"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// Plugin 定义算法插件契约。
type Plugin interface {
	Name() string
	SelectCandidates(req types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error)
}

// ContextPlugin 是可选扩展接口，用于为算法插件传递取消和超时语义。
type ContextPlugin interface {
	Plugin
	SelectCandidatesWithContext(ctx context.Context, req types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error)
}
