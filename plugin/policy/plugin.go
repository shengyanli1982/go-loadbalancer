package policy

import "github.com/shengyanli1982/go-loadbalancer/types"

// Plugin 定义策略插件契约。
type Plugin interface {
	Name() string
	ReRank(req types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error)
}

// HardConstraintPlugin 标记“不可被 fallback 绕过”的硬约束策略。
type HardConstraintPlugin interface {
	IsHardConstraint() bool
}
