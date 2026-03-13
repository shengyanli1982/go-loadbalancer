package policy

import "go-loadbalancer/types"

// Plugin 定义策略插件契约。
type Plugin interface {
	Name() string
	ReRank(req types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error)
}
