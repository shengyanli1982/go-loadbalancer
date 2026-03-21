package policy

import (
	"context"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// Plugin 定义策略插件契约。
type Plugin interface {
	Name() string
	ReRank(req types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error)
}

// Role describes the internal responsibility of a policy plugin.
type Role string

const (
	RoleFilter   Role = "filter"
	RoleRerank   Role = "rerank"
	RoleGuard    Role = "guard"
	RoleAffinity Role = "affinity"
)

// RoleAwarePlugin is an optional extension interface for exposing policy intent without changing execution wiring.
type RoleAwarePlugin interface {
	Plugin
	PolicyRole() Role
}

// ContextPlugin 是可选扩展接口，用于为策略插件传递取消和超时语义。
type ContextPlugin interface {
	Plugin
	ReRankWithContext(ctx context.Context, req types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error)
}

// HardConstraintPlugin 标记“不可被 fallback 绕过”的硬约束策略。
type HardConstraintPlugin interface {
	IsHardConstraint() bool
}
