package algorithm

import "github.com/shengyanli1982/go-loadbalancer/types"

// Plugin 定义算法插件契约。
type Plugin interface {
	Name() string
	SelectCandidates(req types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error)
}
