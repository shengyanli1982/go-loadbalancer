package balancer

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"time"

	"github.com/shengyanli1982/go-loadbalancer/config"
	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	algorithmplugin "github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/builtin"
	objectiveplugin "github.com/shengyanli1982/go-loadbalancer/plugin/objective"
	policyplugin "github.com/shengyanli1982/go-loadbalancer/plugin/policy"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/telemetry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const (
	stageFilter    = "filter"
	stageObjective = "objective"
	stageRoute     = "route"
	stageFallback  = "fallback"

	outcomeSuccess = "success"
	outcomeFailed  = "failed"

	reasonSelectedByObjective    = "selected_by=objective"
	reasonSelectedByPolicyRanked = "selected_by=policy_ranked"
	reasonFallbackPolicyRanked   = "fallback=policy_ranked"
	reasonFallbackPrefix         = "fallback="
	reasonCauseFormat            = "cause=%v"

	defaultObjectiveMaxConcurrent = 128
)

// Balancer 定义 A2X 路由主接口。
type Balancer interface {
	Route(ctx context.Context, req types.RequestContext, nodes []types.NodeSnapshot) (types.Candidate, error)
	Close(ctx context.Context) error
}

// a2xBalancer 是 Balancer 接口的默认实现。
// 包含配置、插件注册表和遥测数据收集器。
type a2xBalancer struct {
	cfg                config.Config  // 路由配置
	sink               telemetry.Sink // 遥测数据收集器
	routeAlgorithms    map[types.RouteClass]algorithmplugin.Plugin
	policies           []policyplugin.Plugin
	hardPolicies       []policyplugin.Plugin
	objectivePlugin    objectiveplugin.Plugin
	objectiveLimiter   chan struct{}
	fallbackAlgorithms map[string]algorithmplugin.Plugin
	telemetryEnabled   bool
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
	reg := registry.Default()

	routeAlgorithms := make(map[types.RouteClass]algorithmplugin.Plugin, len(local.Plugins.Algorithms))
	for routeClass, algorithmName := range local.Plugins.Algorithms {
		if ctor, ok := reg.GetAlgorithmFactory(algorithmName); ok {
			plugin, err := instantiateAlgorithmFromFactory(algorithmName, ctor)
			if err != nil {
				return nil, err
			}
			routeAlgorithms[routeClass] = plugin
			continue
		}
		prototype, ok := reg.GetAlgorithm(algorithmName)
		if !ok || prototype == nil {
			return nil, fmt.Errorf("algorithm=%s: %w", algorithmName, lberrors.ErrUnknownPlugin)
		}
		plugin, err := instantiateAlgorithmPlugin(prototype)
		if err != nil {
			return nil, fmt.Errorf("algorithm=%s: %w", algorithmName, err)
		}
		routeAlgorithms[routeClass] = plugin
	}

	policies := make([]policyplugin.Plugin, 0, len(local.Plugins.Policies))
	hardPolicies := make([]policyplugin.Plugin, 0, len(local.Plugins.Policies))
	for _, policyName := range local.Plugins.Policies {
		if ctor, ok := reg.GetPolicyFactory(policyName); ok {
			plugin, err := instantiatePolicyFromFactory(policyName, ctor)
			if err != nil {
				return nil, err
			}
			policies = append(policies, plugin)
			if isHardConstraintPolicy(plugin) {
				hardPolicies = append(hardPolicies, plugin)
			}
			continue
		}
		prototype, ok := reg.GetPolicy(policyName)
		if !ok || prototype == nil {
			return nil, fmt.Errorf("policy=%s: %w", policyName, lberrors.ErrUnknownPlugin)
		}
		plugin, err := instantiatePolicyPlugin(prototype)
		if err != nil {
			return nil, fmt.Errorf("policy=%s: %w", policyName, err)
		}
		policies = append(policies, plugin)
		if isHardConstraintPolicy(plugin) {
			hardPolicies = append(hardPolicies, plugin)
		}
	}

	var objectivePlugin objectiveplugin.Plugin
	var objectiveLimiter chan struct{}
	if local.Plugins.Objective.Enabled {
		if ctor, ok := reg.GetObjectiveFactory(local.Plugins.Objective.Name); ok {
			plugin, err := instantiateObjectiveFromFactory(local.Plugins.Objective.Name, ctor)
			if err != nil {
				return nil, err
			}
			if configurable, ok := plugin.(objectiveplugin.RouteWeightsAware); ok {
				configurable.SetRouteWeights(local.Weights.ByRouteClass)
			}
			objectivePlugin = plugin
		} else {
			prototype, ok := reg.GetObjective(local.Plugins.Objective.Name)
			if !ok || prototype == nil {
				return nil, fmt.Errorf("objective=%s: %w", local.Plugins.Objective.Name, lberrors.ErrUnknownPlugin)
			}
			plugin, err := instantiateObjectivePlugin(prototype)
			if err != nil {
				return nil, fmt.Errorf("objective=%s: %w", local.Plugins.Objective.Name, err)
			}
			if configurable, ok := plugin.(objectiveplugin.RouteWeightsAware); ok {
				configurable.SetRouteWeights(local.Weights.ByRouteClass)
			}
			objectivePlugin = plugin
		}
		maxConcurrent := local.Plugins.Objective.MaxConcurrent
		if maxConcurrent == 0 {
			maxConcurrent = defaultObjectiveMaxConcurrent
		}
		if maxConcurrent > 0 {
			objectiveLimiter = make(chan struct{}, maxConcurrent)
		}
	}

	fallbackAlgorithms := make(map[string]algorithmplugin.Plugin, len(local.FallbackChain))
	for _, step := range local.FallbackChain {
		if step == config.FallbackPolicyRanked {
			continue
		}
		if _, exists := fallbackAlgorithms[step]; exists {
			continue
		}
		if ctor, ok := reg.GetAlgorithmFactory(step); ok {
			plugin, err := instantiateAlgorithmFromFactory(step, ctor)
			if err != nil {
				return nil, err
			}
			fallbackAlgorithms[step] = plugin
			continue
		}
		prototype, ok := reg.GetAlgorithm(step)
		if !ok || prototype == nil {
			return nil, fmt.Errorf("fallback=%s: %w", step, lberrors.ErrUnknownPlugin)
		}
		plugin, err := instantiateAlgorithmPlugin(prototype)
		if err != nil {
			return nil, fmt.Errorf("fallback=%s: %w", step, err)
		}
		fallbackAlgorithms[step] = plugin
	}

	telemetryEnabled := local.TelemetrySink != nil
	switch local.TelemetrySink.(type) {
	case telemetry.NoopSink, *telemetry.NoopSink:
		telemetryEnabled = false
	}

	return &a2xBalancer{
		cfg:                local,
		sink:               local.TelemetrySink,
		routeAlgorithms:    routeAlgorithms,
		policies:           policies,
		hardPolicies:       hardPolicies,
		objectivePlugin:    objectivePlugin,
		objectiveLimiter:   objectiveLimiter,
		fallbackAlgorithms: fallbackAlgorithms,
		telemetryEnabled:   telemetryEnabled,
	}, nil
}

// Close 预留资源释放入口，当前实现无状态可释放。
func (b *a2xBalancer) Close(_ context.Context) error {
	return nil
}

// Route 执行完整路由流程：过滤、算法筛选、策略重排、目标函数择优与回退。
func (b *a2xBalancer) Route(ctx context.Context, req types.RequestContext, nodes []types.NodeSnapshot) (types.Candidate, error) {
	var started time.Time
	if b.telemetryEnabled {
		started = time.Now()
	}

	// 第一步：先做硬约束过滤（健康状态、模型可用性）。
	// 过滤出健康且支持目标模型的节点。
	filtered, filterErr := filterNodes(req, nodes, b.cfg.SnapshotTTLGuard)
	if filterErr != nil {
		if b.telemetryEnabled {
			b.emit(telemetry.TelemetryEvent{
				Type:       telemetry.EventRouteDecision,
				RouteClass: string(req.RouteClass),
				Stage:      stageFilter,
				Outcome:    outcomeFailed,
				Reason:     filterErr.Error(),
				DurationMs: sinceMs(started),
			})
		}
		return types.Candidate{}, filterErr
	}

	algorithmPlugin, ok := b.routeAlgorithms[req.RouteClass]
	if !ok || algorithmPlugin == nil {
		return types.Candidate{}, fmt.Errorf("route_class=%s: %w", req.RouteClass, lberrors.ErrPluginMisconfigured)
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
	for _, policyPlugin := range b.policies {
		nextRanked, policyErr := policyPlugin.ReRank(req, ranked)
		if policyErr != nil || len(nextRanked) == 0 {
			baseErr := policyErr
			if baseErr == nil {
				baseErr = lberrors.ErrNoCandidate
			}
			if isHardConstraintPolicy(policyPlugin) {
				return types.Candidate{}, baseErr
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
			candidate.Reason = append(candidate.Reason, reasonSelectedByObjective)
			if b.telemetryEnabled {
				b.emit(telemetry.TelemetryEvent{
					Type:                 telemetry.EventObjectiveResult,
					RouteClass:           string(req.RouteClass),
					Stage:                stageObjective,
					Outcome:              outcomeSuccess,
					Plugin:               b.cfg.Plugins.Objective.Name,
					ObjectiveCancellable: isObjectiveCancellable(b.objectivePlugin),
					DurationMs:           sinceMs(started),
				})
			}
			return candidate, nil
		}
		if b.telemetryEnabled {
			b.emit(telemetry.TelemetryEvent{
				Type:                 telemetry.EventObjectiveResult,
				RouteClass:           string(req.RouteClass),
				Stage:                stageObjective,
				Outcome:              outcomeFailed,
				Reason:               objectiveErr.Error(),
				Plugin:               b.cfg.Plugins.Objective.Name,
				ObjectiveCancellable: isObjectiveCancellable(b.objectivePlugin),
				DurationMs:           sinceMs(started),
			})
		}
		candidate, fbErr := b.fallback(ctx, req, filtered, ranked, objectiveErr)
		if fbErr != nil {
			return types.Candidate{}, errors.Join(objectiveErr, fbErr)
		}
		return candidate, nil
	}

	// 第五步：默认返回策略排序后的第一候选。
	// 如果没有启用目标函数或目标函数失败，返回策略排序后的最优候选。
	ranked[0].Reason = append(ranked[0].Reason, reasonSelectedByPolicyRanked)
	if b.telemetryEnabled {
		b.emit(telemetry.TelemetryEvent{
			Type:       telemetry.EventRouteDecision,
			RouteClass: string(req.RouteClass),
			Stage:      stageRoute,
			Outcome:    outcomeSuccess,
			DurationMs: sinceMs(started),
		})
	}
	return ranked[0], nil
}

// chooseByObjective 在超时约束下调用目标函数插件进行二次择优。
func (b *a2xBalancer) chooseByObjective(ctx context.Context, req types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	objectiveName := b.cfg.Plugins.Objective.Name
	plugin := b.objectivePlugin
	if plugin == nil {
		return types.Candidate{}, fmt.Errorf("objective=%s: %w", objectiveName, lberrors.ErrUnknownPlugin)
	}
	timeout := b.objectiveTimeout(req)
	timeoutCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	release, acquireErr := b.acquireObjectiveSlot(timeoutCtx, objectiveName, timeout)
	if acquireErr != nil {
		return types.Candidate{}, acquireErr
	}
	defer release()

	if ctxPlugin, ok := plugin.(objectiveplugin.ContextPlugin); ok {
		candidate, err := safeChooseWithContext(ctxPlugin, timeoutCtx, req, candidates, objectiveName)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
				return types.Candidate{}, fmt.Errorf("objective=%s timeout=%s: %w", objectiveName, timeout, lberrors.ErrPluginTimeout)
			}
			return types.Candidate{}, err
		}
		return candidate, nil
	}

	type result struct {
		candidate types.Candidate
		err       error
	}
	resCh := make(chan result, 1)
	// Objective 在 goroutine 内执行，避免阻塞 Route 热路径。
	// 使用 goroutine 并发执行目标函数，防止长时间阻塞主路由流程。
	go func() {
		candidate, err := safeChoose(plugin, req, candidates, objectiveName)
		resCh <- result{candidate: candidate, err: err}
	}()
	deadline, _ := timeoutCtx.Deadline()
	timer := time.NewTimer(time.Until(deadline))
	defer func() {
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
	}()

	select {
	case <-timeoutCtx.Done():
		if errors.Is(timeoutCtx.Err(), context.DeadlineExceeded) {
			return types.Candidate{}, fmt.Errorf("objective=%s timeout=%s: %w", objectiveName, timeout, lberrors.ErrPluginTimeout)
		}
		return types.Candidate{}, timeoutCtx.Err()
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
			candidate, allowed, hardErr := b.enforceHardPolicies(req, candidate)
			if hardErr != nil {
				return types.Candidate{}, hardErr
			}
			if !allowed {
				continue
			}
			candidate.Reason = append(candidate.Reason, reasonFallbackPolicyRanked, fmt.Sprintf(reasonCauseFormat, cause))
			if b.telemetryEnabled {
				b.emit(telemetry.TelemetryEvent{
					Type:       telemetry.EventRouteFallback,
					RouteClass: string(req.RouteClass),
					Stage:      stageFallback,
					Outcome:    outcomeSuccess,
					Reason:     candidate.Node.NodeID,
				})
			}
			return candidate, nil
		default:
			// 回退步骤是算法名，使用该算法从过滤后的节点中选择候选。
			plugin, ok := b.fallbackAlgorithms[step]
			if !ok {
				continue
			}
			topK := b.cfg.TopK
			if topK < 1 {
				topK = 1
			}
			if topK > len(filtered) {
				topK = len(filtered)
			}
			candidates, err := plugin.SelectCandidates(req, filtered, topK)
			if err != nil || len(candidates) == 0 {
				continue
			}

			for _, candidate := range candidates {
				candidate, allowed, hardErr := b.enforceHardPolicies(req, candidate)
				if hardErr != nil {
					return types.Candidate{}, hardErr
				}
				if !allowed {
					continue
				}
				candidate.Reason = append(candidate.Reason, reasonFallbackPrefix+step, fmt.Sprintf(reasonCauseFormat, cause))
				if b.telemetryEnabled {
					b.emit(telemetry.TelemetryEvent{
						Type:       telemetry.EventRouteFallback,
						RouteClass: string(req.RouteClass),
						Stage:      stageFallback,
						Outcome:    outcomeSuccess,
						Reason:     candidate.Node.NodeID,
						Plugin:     step,
					})
				}
				return candidate, nil
			}
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
func filterNodes(req types.RequestContext, nodes []types.NodeSnapshot, enforceSnapshotTTL bool) ([]types.NodeSnapshot, error) {
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoHealthyNodes
	}

	model := req.Model
	healthyCount := 0
	matchedCount := 0
	for i := range nodes {
		n := &nodes[i]
		if !n.Healthy {
			continue
		}
		if enforceSnapshotTTL && n.FreshnessTTLms <= 0 {
			continue
		}
		healthyCount++
		if model != "" && !nodeSupportsModel(n, model) {
			continue
		}
		matchedCount++
	}

	if healthyCount == 0 {
		return nil, lberrors.ErrNoHealthyNodes
	}
	if matchedCount == 0 {
		return nil, lberrors.ErrNoModelAvailable
	}
	if matchedCount == len(nodes) {
		return nodes, nil
	}

	filtered := make([]types.NodeSnapshot, 0, matchedCount)
	for i := range nodes {
		n := &nodes[i]
		if !n.Healthy {
			continue
		}
		if enforceSnapshotTTL && n.FreshnessTTLms <= 0 {
			continue
		}
		if model != "" && !nodeSupportsModel(n, model) {
			continue
		}
		filtered = append(filtered, *n)
	}
	return filtered, nil
}

func nodeSupportsModel(n *types.NodeSnapshot, model string) bool {
	if n.ModelCapability != nil {
		return n.ModelCapability.Allows(model)
	}
	if len(n.ModelAvailability) == 0 {
		return true
	}
	return n.ModelAvailability[model]
}

func (b *a2xBalancer) enforceHardPolicies(req types.RequestContext, candidate types.Candidate) (types.Candidate, bool, error) {
	if len(b.hardPolicies) == 0 {
		return candidate, true, nil
	}

	ranked := []types.Candidate{candidate}
	for _, policyPlugin := range b.hardPolicies {
		nextRanked, err := policyPlugin.ReRank(req, ranked)
		if err != nil {
			return types.Candidate{}, false, err
		}
		if len(nextRanked) == 0 {
			return types.Candidate{}, false, nil
		}
		ranked = nextRanked
	}
	return ranked[0], true, nil
}

func safeChoose(plugin objectiveplugin.Plugin, req types.RequestContext, candidates []types.Candidate, objectiveName string) (candidate types.Candidate, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("objective=%s panic=%v: %w", objectiveName, recovered, lberrors.ErrPluginMisconfigured)
		}
	}()
	return plugin.Choose(req, candidates)
}

func safeChooseWithContext(plugin objectiveplugin.ContextPlugin, ctx context.Context, req types.RequestContext, candidates []types.Candidate, objectiveName string) (candidate types.Candidate, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("objective=%s panic=%v: %w", objectiveName, recovered, lberrors.ErrPluginMisconfigured)
		}
	}()
	return plugin.ChooseWithContext(ctx, req, candidates)
}

func (b *a2xBalancer) objectiveTimeout(req types.RequestContext) time.Duration {
	base := time.Duration(b.cfg.Plugins.Objective.TimeoutMs) * time.Millisecond
	totalTokens := req.PromptTokens + req.ExpectedTokens
	switch req.RouteClass {
	case types.RouteLLMPrefill:
		switch {
		case totalTokens >= 8192:
			return base * 3
		case totalTokens >= 2048:
			return base * 2
		}
	case types.RouteLLMDecode:
		if req.ExpectedTokens >= 2048 {
			return base * 2
		}
	}
	return base
}

func (b *a2xBalancer) acquireObjectiveSlot(ctx context.Context, objectiveName string, timeout time.Duration) (func(), error) {
	if cap(b.objectiveLimiter) == 0 {
		return func() {}, nil
	}

	select {
	case b.objectiveLimiter <- struct{}{}:
		return func() {
			<-b.objectiveLimiter
		}, nil
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(ctx.Err(), context.Canceled) {
			return nil, fmt.Errorf("objective=%s timeout=%s max_concurrent=%d: %w", objectiveName, timeout, cap(b.objectiveLimiter), lberrors.ErrPluginTimeout)
		}
		return nil, ctx.Err()
	}
}

func instantiateAlgorithmPlugin(prototype algorithmplugin.Plugin) (algorithmplugin.Plugin, error) {
	instance, err := instantiatePluginInstance(prototype)
	if err != nil {
		return nil, err
	}
	plugin, ok := instance.(algorithmplugin.Plugin)
	if !ok {
		return nil, fmt.Errorf("algorithm plugin=%T: %w", prototype, lberrors.ErrPluginMisconfigured)
	}
	return plugin, nil
}

func instantiateAlgorithmFromFactory(name string, ctor func() algorithmplugin.Plugin) (algorithmplugin.Plugin, error) {
	if ctor == nil {
		return nil, fmt.Errorf("algorithm=%s: %w", name, lberrors.ErrPluginMisconfigured)
	}
	plugin := ctor()
	if plugin == nil {
		return nil, fmt.Errorf("algorithm=%s: %w", name, lberrors.ErrPluginMisconfigured)
	}
	if plugin.Name() == "" {
		return nil, fmt.Errorf("algorithm=%s: %w", name, lberrors.ErrPluginMisconfigured)
	}
	return plugin, nil
}

func instantiatePolicyPlugin(prototype policyplugin.Plugin) (policyplugin.Plugin, error) {
	instance, err := instantiatePluginInstance(prototype)
	if err != nil {
		return nil, err
	}
	plugin, ok := instance.(policyplugin.Plugin)
	if !ok {
		return nil, fmt.Errorf("policy plugin=%T: %w", prototype, lberrors.ErrPluginMisconfigured)
	}
	return plugin, nil
}

func instantiatePolicyFromFactory(name string, ctor func() policyplugin.Plugin) (policyplugin.Plugin, error) {
	if ctor == nil {
		return nil, fmt.Errorf("policy=%s: %w", name, lberrors.ErrPluginMisconfigured)
	}
	plugin := ctor()
	if plugin == nil {
		return nil, fmt.Errorf("policy=%s: %w", name, lberrors.ErrPluginMisconfigured)
	}
	if plugin.Name() == "" {
		return nil, fmt.Errorf("policy=%s: %w", name, lberrors.ErrPluginMisconfigured)
	}
	return plugin, nil
}

func instantiateObjectivePlugin(prototype objectiveplugin.Plugin) (objectiveplugin.Plugin, error) {
	instance, err := instantiatePluginInstance(prototype)
	if err != nil {
		return nil, err
	}
	plugin, ok := instance.(objectiveplugin.Plugin)
	if !ok {
		return nil, fmt.Errorf("objective plugin=%T: %w", prototype, lberrors.ErrPluginMisconfigured)
	}
	return plugin, nil
}

func instantiateObjectiveFromFactory(name string, ctor func() objectiveplugin.Plugin) (objectiveplugin.Plugin, error) {
	if ctor == nil {
		return nil, fmt.Errorf("objective=%s: %w", name, lberrors.ErrPluginMisconfigured)
	}
	plugin := ctor()
	if plugin == nil {
		return nil, fmt.Errorf("objective=%s: %w", name, lberrors.ErrPluginMisconfigured)
	}
	if plugin.Name() == "" {
		return nil, fmt.Errorf("objective=%s: %w", name, lberrors.ErrPluginMisconfigured)
	}
	return plugin, nil
}

func instantiatePluginInstance(prototype any) (any, error) {
	if prototype == nil {
		return nil, fmt.Errorf("plugin=nil: %w", lberrors.ErrPluginMisconfigured)
	}
	typ := reflect.TypeOf(prototype)
	if typ.Kind() == reflect.Ptr {
		return reflect.New(typ.Elem()).Interface(), nil
	}
	return reflect.New(typ).Elem().Interface(), nil
}

func isHardConstraintPolicy(plugin policyplugin.Plugin) bool {
	hard, ok := plugin.(policyplugin.HardConstraintPlugin)
	return ok && hard.IsHardConstraint()
}

func isObjectiveCancellable(plugin objectiveplugin.Plugin) bool {
	if plugin == nil {
		return false
	}
	_, ok := plugin.(objectiveplugin.ContextPlugin)
	return ok
}

// sinceMs 返回从 started 到当前时刻的毫秒耗时。
func sinceMs(started time.Time) int64 {
	return time.Since(started).Milliseconds()
}
