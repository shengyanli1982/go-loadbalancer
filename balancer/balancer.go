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

// Balancer 定义 A2X 路由主接口。
type Balancer interface {
	Route(ctx context.Context, req types.RequestContext, nodes []types.NodeSnapshot) (types.Candidate, error)
	Close(ctx context.Context) error
}

// a2xBalancer 是 Balancer 接口的默认实现。
// 包含配置、插件注册表和遥测数据收集器。
type a2xBalancer struct {
	cfg  config.Config        // 路由配置
	reg  *registry.Manager    // 插件注册表管理器
	sink telemetry.Sink       // 遥测数据收集器
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

// Close 预留资源释放入口，当前实现无状态可释放。
func (b *a2xBalancer) Close(_ context.Context) error {
	return nil
}

// Route 执行完整路由流程：过滤、算法筛选、策略重排、目标函数择优与回退。
func (b *a2xBalancer) Route(ctx context.Context, req types.RequestContext, nodes []types.NodeSnapshot) (types.Candidate, error) {
	started := time.Now()

	// 第一步：先做硬约束过滤（健康状态、模型可用性）。
	// 过滤出健康且支持目标模型的节点。
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
	// 获取对应路由类的算法插件
	algorithmPlugin, ok := b.reg.GetAlgorithm(algorithmName)
	if !ok {
		return types.Candidate{}, fmt.Errorf("algorithm=%s: %w", algorithmName, lberrors.ErrUnknownPlugin)
	}

	// 第二步：算法层给出候选集；候选为空或出错都进入回退链。
	// 调用算法插件从过滤后的节点中选出 TopK 个候选。
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
	// 依次应用所有策略插件对候选进行重排。
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
	// 如果启用了目标函数插件，使用其进行二次选择。
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
	// 如果没有启用目标函数或目标函数失败，返回策略排序后的最优候选。
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

// chooseByObjective 在超时约束下调用目标函数插件进行二次择优。
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
	// 使用 goroutine 并发执行目标函数，防止长时间阻塞主路由流程。
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
	// 如果目标函数执行超时，则认为插件失败，触发回退机制。
	case <-timer.C:
		return types.Candidate{}, fmt.Errorf("objective=%s timeout=%s: %w", objectiveName, timeout, lberrors.ErrPluginTimeout)
	case res := <-resCh:
		if res.err != nil {
			return types.Candidate{}, res.err
		}
		return res.candidate, nil
	}
}

// fallback 按配置的回退链依次尝试生成可用候选。
func (b *a2xBalancer) fallback(ctx context.Context, req types.RequestContext, filtered []types.NodeSnapshot, ranked []types.Candidate, cause error) (types.Candidate, error) {
	// 回退链允许混合使用 policy_ranked 和算法名，按配置顺序逐个尝试。
	// 依次尝试回退链中的每个策略或算法。
	for _, step := range b.cfg.FallbackChain {
		switch step {
		case config.FallbackPolicyRanked:
			// 如果回退步骤是 policy_ranked，直接返回策略排序后的第一候选。
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
			// 回退步骤是算法名，使用该算法从过滤后的节点中选择候选。
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

// emit 统一补齐事件时间戳并安全发送观测事件。
func (b *a2xBalancer) emit(e telemetry.TelemetryEvent) {
	e.Timestamp = time.Now()
	telemetry.EmitSafe(b.sink, e)
}

// filterNodes 按健康状态与模型可用性执行硬约束过滤。
func filterNodes(req types.RequestContext, nodes []types.NodeSnapshot) ([]types.NodeSnapshot, error) {
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoHealthyNodes
	}
	filtered := make([]types.NodeSnapshot, 0, len(nodes))
	healthyCount := 0
	// 遍历所有节点，过滤出健康的节点。
	for _, n := range nodes {
		if !n.Healthy {
			continue
		}
		healthyCount++
		// 如果指定了模型，检查节点是否支持该模型。
		if req.Model != "" && len(n.ModelAvailability) > 0 && !n.ModelAvailability[req.Model] {
			continue
		}
		filtered = append(filtered, n)
	}
	if healthyCount == 0 {
		return nil, lberrors.ErrNoHealthyNodes
	}
	// 如果有健康节点但都不支持目标模型，返回模型不可用错误。
	if len(filtered) == 0 {
		return nil, lberrors.ErrNoModelAvailable
	}
	return filtered, nil
}

// sinceMs 返回从 started 到当前时刻的毫秒耗时。
func sinceMs(started time.Time) int64 {
	return time.Since(started).Milliseconds()
}
