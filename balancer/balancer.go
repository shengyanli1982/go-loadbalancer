package balancer

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/shengyanli1982/go-loadbalancer/config"
	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/builtin"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/telemetry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const fallbackPolicyRanked = "policy_ranked"

// Balancer 定义 A2X 路由主接口。
type Balancer interface {
	Route(ctx context.Context, req types.RequestContext, nodes []types.NodeSnapshot) (types.Candidate, error)
	Close(ctx context.Context) error
}

type a2xBalancer struct {
	cfg  config.Config
	reg  *registry.Manager
	sink telemetry.Sink
}

// New 创建 Balancer 实例。
func New(cfg config.Config, opts ...config.Option) (Balancer, error) {
	local := cfg
	for _, opt := range opts {
		if opt != nil {
			opt(&local)
		}
	}
	if local.TelemetrySink == nil {
		local.TelemetrySink = telemetry.NoopSink{}
	}
	if err := local.Validate(); err != nil {
		return nil, err
	}
	return &a2xBalancer{cfg: local, reg: registry.Default(), sink: local.TelemetrySink}, nil
}

func (b *a2xBalancer) Close(_ context.Context) error {
	return nil
}

func (b *a2xBalancer) Route(ctx context.Context, req types.RequestContext, nodes []types.NodeSnapshot) (types.Candidate, error) {
	started := time.Now()

	// 第一步：先做硬约束过滤（健康状态、模型可用性）。
	filtered, filterErr := filterNodes(req, nodes)
	if filterErr != nil {
		b.emit(telemetry.TelemetryEvent{
			Type:       telemetry.EventRouteDecision,
			RouteClass: string(req.RouteClass),
			Stage:      "filter",
			Outcome:    "failed",
			Reason:     filterErr.Error(),
			DurationMs: sinceMs(started),
		})
		return types.Candidate{}, filterErr
	}

	algorithmName, ok := b.cfg.Plugins.Algorithms[req.RouteClass]
	if !ok || algorithmName == "" {
		return types.Candidate{}, fmt.Errorf("route_class=%s: %w", req.RouteClass, lberrors.ErrPluginMisconfigured)
	}
	algorithmPlugin, ok := b.reg.GetAlgorithm(algorithmName)
	if !ok {
		return types.Candidate{}, fmt.Errorf("algorithm=%s: %w", algorithmName, lberrors.ErrUnknownPlugin)
	}

	// 第二步：算法层给出候选集；候选为空或出错都进入回退链。
	candidates, err := algorithmPlugin.SelectCandidates(req, filtered, b.cfg.TopK)
	if err != nil {
		candidate, fbErr := b.fallback(ctx, req, filtered, nil, errors.Join(err, lberrors.ErrNoCandidate))
		if fbErr != nil {
			return types.Candidate{}, errors.Join(err, fbErr)
		}
		return candidate, nil
	}
	if len(candidates) == 0 {
		candidate, fbErr := b.fallback(ctx, req, filtered, nil, lberrors.ErrNoCandidate)
		if fbErr != nil {
			return types.Candidate{}, fbErr
		}
		return candidate, nil
	}

	ranked := candidates
	// 第三步：策略层按顺序重排，任何策略失败都触发回退。
	for _, policyName := range b.cfg.Plugins.Policies {
		policyPlugin, ok := b.reg.GetPolicy(policyName)
		if !ok {
			candidate, fbErr := b.fallback(ctx, req, filtered, ranked, fmt.Errorf("policy=%s: %w", policyName, lberrors.ErrUnknownPlugin))
			if fbErr != nil {
				return types.Candidate{}, fbErr
			}
			return candidate, nil
		}
		nextRanked, policyErr := policyPlugin.ReRank(req, ranked)
		if policyErr != nil || len(nextRanked) == 0 {
			baseErr := policyErr
			if baseErr == nil {
				baseErr = lberrors.ErrNoCandidate
			}
			candidate, fbErr := b.fallback(ctx, req, filtered, ranked, baseErr)
			if fbErr != nil {
				return types.Candidate{}, errors.Join(baseErr, fbErr)
			}
			return candidate, nil
		}
		ranked = nextRanked
	}

	// 第四步：可选 Objective 二次择优；失败时按设计降级回退。
	if b.cfg.Plugins.Objective.Enabled {
		candidate, objectiveErr := b.chooseByObjective(ctx, req, ranked)
		if objectiveErr == nil {
			candidate.Reason = append(candidate.Reason, "selected_by=objective")
			b.emit(telemetry.TelemetryEvent{
				Type:       telemetry.EventObjectiveResult,
				RouteClass: string(req.RouteClass),
				Stage:      "objective",
				Outcome:    "success",
				Plugin:     b.cfg.Plugins.Objective.Name,
				DurationMs: sinceMs(started),
			})
			return candidate, nil
		}
		candidate, fbErr := b.fallback(ctx, req, filtered, ranked, objectiveErr)
		if fbErr != nil {
			return types.Candidate{}, errors.Join(objectiveErr, fbErr)
		}
		return candidate, nil
	}

	// 第五步：默认返回策略排序后的第一候选。
	ranked[0].Reason = append(ranked[0].Reason, "selected_by=policy_ranked")
	b.emit(telemetry.TelemetryEvent{
		Type:       telemetry.EventRouteDecision,
		RouteClass: string(req.RouteClass),
		Stage:      "route",
		Outcome:    "success",
		DurationMs: sinceMs(started),
	})
	return ranked[0], nil
}

func (b *a2xBalancer) chooseByObjective(ctx context.Context, req types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	objectiveName := b.cfg.Plugins.Objective.Name
	plugin, ok := b.reg.GetObjective(objectiveName)
	if !ok {
		return types.Candidate{}, fmt.Errorf("objective=%s: %w", objectiveName, lberrors.ErrUnknownPlugin)
	}
	timeout := time.Duration(b.cfg.Plugins.Objective.TimeoutMs) * time.Millisecond

	type result struct {
		candidate types.Candidate
		err       error
	}
	resCh := make(chan result, 1)
	// Objective 在 goroutine 内执行，避免阻塞 Route 热路径。
	go func() {
		candidate, err := plugin.Choose(req, candidates)
		resCh <- result{candidate: candidate, err: err}
	}()
	timer := time.NewTimer(timeout)
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	select {
	case <-ctx.Done():
		return types.Candidate{}, ctx.Err()
	// 超时视为插件失败，交给上层 fallback 处理。
	case <-timer.C:
		return types.Candidate{}, fmt.Errorf("objective=%s timeout=%s: %w", objectiveName, timeout, lberrors.ErrPluginTimeout)
	case res := <-resCh:
		if res.err != nil {
			return types.Candidate{}, res.err
		}
		return res.candidate, nil
	}
}

func (b *a2xBalancer) fallback(ctx context.Context, req types.RequestContext, filtered []types.NodeSnapshot, ranked []types.Candidate, cause error) (types.Candidate, error) {
	// 回退链允许混合使用 policy_ranked 和算法名，按配置顺序逐个尝试。
	for _, step := range b.cfg.FallbackChain {
		switch step {
		case fallbackPolicyRanked:
			if len(ranked) == 0 {
				continue
			}
			candidate := ranked[0]
			candidate.Reason = append(candidate.Reason, "fallback=policy_ranked", fmt.Sprintf("cause=%v", cause))
			b.emit(telemetry.TelemetryEvent{
				Type:       telemetry.EventRouteFallback,
				RouteClass: string(req.RouteClass),
				Stage:      "fallback",
				Outcome:    "success",
				Reason:     candidate.Node.NodeID,
			})
			return candidate, nil
		default:
			plugin, ok := b.reg.GetAlgorithm(step)
			if !ok {
				continue
			}
			candidates, err := plugin.SelectCandidates(req, filtered, 1)
			if err != nil || len(candidates) == 0 {
				continue
			}
			candidate := candidates[0]
			candidate.Reason = append(candidate.Reason, "fallback="+step, fmt.Sprintf("cause=%v", cause))
			b.emit(telemetry.TelemetryEvent{
				Type:       telemetry.EventRouteFallback,
				RouteClass: string(req.RouteClass),
				Stage:      "fallback",
				Outcome:    "success",
				Reason:     candidate.Node.NodeID,
				Plugin:     step,
			})
			return candidate, nil
		}
	}
	return types.Candidate{}, errors.Join(cause, lberrors.ErrNoCandidate)
}

func (b *a2xBalancer) emit(e telemetry.TelemetryEvent) {
	e.Timestamp = time.Now()
	telemetry.EmitSafe(b.sink, e)
}

func filterNodes(req types.RequestContext, nodes []types.NodeSnapshot) ([]types.NodeSnapshot, error) {
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoHealthyNodes
	}
	filtered := make([]types.NodeSnapshot, 0, len(nodes))
	healthyCount := 0
	for _, n := range nodes {
		if !n.Healthy {
			continue
		}
		healthyCount++
		if req.Model != "" && len(n.ModelAvailability) > 0 && !n.ModelAvailability[req.Model] {
			continue
		}
		filtered = append(filtered, n)
	}
	if healthyCount == 0 {
		return nil, lberrors.ErrNoHealthyNodes
	}
	if len(filtered) == 0 {
		return nil, lberrors.ErrNoModelAvailable
	}
	return filtered, nil
}

func sinceMs(started time.Time) int64 {
	return time.Since(started).Milliseconds()
}
