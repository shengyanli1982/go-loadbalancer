package config

import (
	"errors"
	"fmt"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/telemetry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const (
	// FallbackPolicyRanked 表示直接回退到策略排序结果。
	FallbackPolicyRanked = "policy_ranked"

	// 内置算法插件名。
	AlgorithmRoundRobin         = "rr"
	AlgorithmWeightedRoundRobin = "wrr"
	AlgorithmConsistentHash     = "consistent_hash"
	AlgorithmP2C                = "p2c"
	AlgorithmLeastRequest       = "least_request"

	// 内置策略插件名。
	PolicyHealthGate         = "health_gate"
	PolicyTenantQuota        = "tenant_quota"
	PolicyLLMKVAffinity      = "llm_kv_affinity"
	PolicyLLMStageAware      = "llm_stage_aware"
	PolicyLLMTokenAwareQueue = "llm_token_aware_queue"

	// 内置目标函数插件名。
	ObjectiveWeighted = "weighted_objective"

	// 权重指标键。
	MetricQueue      = "queue"
	MetricP95Latency = "p95_latency"
	MetricErrorRate  = "error_rate"
	MetricTTFT       = "ttft"
	MetricTPOT       = "tpot"
	MetricKVHit      = "kv_hit"

	// 配置字段键（用于错误定位）。
	FieldTopK                    = "top_k"
	FieldRouteClasses            = "route_classes"
	FieldPluginsAlgorithms       = "plugins.algorithms"
	FieldPluginsPolicies         = "plugins.policies"
	FieldPluginsObjectiveName    = "plugins.objective.name"
	FieldPluginsObjectiveTimeout = "plugins.objective.timeout_ms"
	FieldFallbackChain           = "fallback_chain"
	FieldWeights                 = "weights"
)

var requiredLLMMetrics = [...]string{
	MetricTTFT,
	MetricTPOT,
	MetricKVHit,
}

// ObjectiveConfig 定义目标函数插件配置。
type ObjectiveConfig struct {
	Enabled   bool
	Name      string
	TimeoutMs int
}

// PluginConfig 定义插件绑定关系。
type PluginConfig struct {
	Algorithms map[types.RouteClass]string
	Policies   []string
	Objective  ObjectiveConfig
}

// WeightConfig 定义路由打分权重。
type WeightConfig struct {
	ByRouteClass map[types.RouteClass]map[string]int
}

// Config 定义 A2X 核心配置。
type Config struct {
	TopK          int
	RouteClasses  []types.RouteClass
	FallbackChain []string
	Plugins       PluginConfig
	Weights       WeightConfig
	TelemetrySink telemetry.Sink
}

// Option 定义配置函数式选项。
type Option func(*Config)

// DefaultConfig 返回默认配置。
func DefaultConfig() Config {
	return Config{
		TopK: 5,
		RouteClasses: []types.RouteClass{
			types.RouteGeneric,
			types.RouteLLMPrefill,
			types.RouteLLMDecode,
		},
		FallbackChain: []string{FallbackPolicyRanked, AlgorithmLeastRequest, AlgorithmP2C},
		Plugins: PluginConfig{
			Algorithms: map[types.RouteClass]string{
				types.RouteGeneric:    AlgorithmP2C,
				types.RouteLLMPrefill: AlgorithmLeastRequest,
				types.RouteLLMDecode:  AlgorithmLeastRequest,
			},
			Policies: []string{PolicyHealthGate, PolicyTenantQuota},
			Objective: ObjectiveConfig{
				Enabled:   false,
				Name:      ObjectiveWeighted,
				TimeoutMs: 3,
			},
		},
		Weights: WeightConfig{
			ByRouteClass: map[types.RouteClass]map[string]int{
				types.RouteGeneric: {
					MetricQueue:      5000,
					MetricP95Latency: 3000,
					MetricErrorRate:  2000,
				},
				types.RouteLLMPrefill: {
					MetricQueue:      2000,
					MetricP95Latency: 1500,
					MetricErrorRate:  1500,
					MetricTTFT:       2500,
					MetricTPOT:       1000,
					MetricKVHit:      1500,
				},
				types.RouteLLMDecode: {
					MetricQueue:      2000,
					MetricP95Latency: 1500,
					MetricErrorRate:  1500,
					MetricTTFT:       1000,
					MetricTPOT:       2500,
					MetricKVHit:      1500,
				},
			},
		},
		TelemetrySink: telemetry.NoopSink{},
	}
}

// Validate 对配置执行强校验。
func (c *Config) Validate() error {
	var errs []error

	// 检查 TopK 是否在允许范围内（1-32）。
	if c.TopK < 1 || c.TopK > 32 {
		errs = append(errs, lberrors.NewConfigError(
			lberrors.CodeInvalidTopK,
			FieldTopK,
			c.TopK,
			"must be between 1 and 32",
		))
	}

	// 检查路由类别列表是否为空。
	if len(c.RouteClasses) == 0 {
		errs = append(errs, lberrors.NewConfigError(
			lberrors.CodeInvalidRouteClass,
			FieldRouteClasses,
			c.RouteClasses,
			"must not be empty",
		))
	}

	seenRouteClass := make(map[types.RouteClass]struct{}, len(c.RouteClasses))
	// 验证每个路由类别的有效性和唯一性。
	for i, rc := range c.RouteClasses {
		if !isValidRouteClass(rc) {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidRouteClass,
				fmt.Sprintf("%s[%d]", FieldRouteClasses, i),
				rc,
				"must be one of generic,llm-prefill,llm-decode",
			))
			continue
		}
		if _, ok := seenRouteClass[rc]; ok {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidRouteClass,
				fmt.Sprintf("%s[%d]", FieldRouteClasses, i),
				rc,
				"must not contain duplicates",
			))
			continue
		}
		seenRouteClass[rc] = struct{}{}
	}

	// 验证每个路由类别都有对应的算法绑定。
	for _, rc := range c.RouteClasses {
		name, ok := c.Plugins.Algorithms[rc]
		if !ok || name == "" {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeMissingAlgorithmBinding,
				fmt.Sprintf("%s.%s", FieldPluginsAlgorithms, rc),
				name,
				"must bind one algorithm per route class",
			))
			continue
		}
		if !registry.HasAlgorithm(name) {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeMissingAlgorithmBinding,
				fmt.Sprintf("%s.%s", FieldPluginsAlgorithms, rc),
				name,
				"algorithm is not registered",
			))
		}
	}

	seenPolicy := make(map[string]struct{}, len(c.Plugins.Policies))
	// 验证策略的唯一性和注册状态。
	for i, p := range c.Plugins.Policies {
		if _, ok := seenPolicy[p]; ok {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeDuplicatePolicy,
				fmt.Sprintf("%s[%d]", FieldPluginsPolicies, i),
				p,
				"policy must be unique",
			))
			continue
		}
		seenPolicy[p] = struct{}{}
		if !registry.HasPolicy(p) {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeUnknownPolicy,
				fmt.Sprintf("%s[%d]", FieldPluginsPolicies, i),
				p,
				"policy is not registered",
			))
		}
	}

	// 验证目标函数配置（如果启用）。
	if c.Plugins.Objective.Enabled {
		if c.Plugins.Objective.Name == "" {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidObjective,
				FieldPluginsObjectiveName,
				c.Plugins.Objective.Name,
				"must not be empty when objective is enabled",
			))
		} else if !registry.HasObjective(c.Plugins.Objective.Name) {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidObjective,
				FieldPluginsObjectiveName,
				c.Plugins.Objective.Name,
				"objective is not registered",
			))
		}
		if c.Plugins.Objective.TimeoutMs < 1 || c.Plugins.Objective.TimeoutMs > 20 {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidObjectiveTimeout,
				FieldPluginsObjectiveTimeout,
				c.Plugins.Objective.TimeoutMs,
				"must be between 1 and 20",
			))
		}
	}

	// 验证回退链配置。
	if len(c.FallbackChain) == 0 {
		errs = append(errs, lberrors.NewConfigError(
			lberrors.CodeInvalidFallbackChain,
			FieldFallbackChain,
			c.FallbackChain,
			"must not be empty",
		))
	}
	seenFallback := make(map[string]struct{}, len(c.FallbackChain))
	// 验证回退链中每个元素的有效性和唯一性。
	for i, s := range c.FallbackChain {
		if s == "" {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidFallbackChain,
				fmt.Sprintf("%s[%d]", FieldFallbackChain, i),
				s,
				"must not be empty",
			))
			continue
		}
		if _, ok := seenFallback[s]; ok {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidFallbackChain,
				fmt.Sprintf("%s[%d]", FieldFallbackChain, i),
				s,
				"must not contain duplicates",
			))
			continue
		}
		seenFallback[s] = struct{}{}
		if s != FallbackPolicyRanked && !registry.HasAlgorithm(s) {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidFallbackChain,
				fmt.Sprintf("%s[%d]", FieldFallbackChain, i),
				s,
				"must be policy_ranked or a registered algorithm",
			))
		}
	}

	// 验证每个路由类别的权重配置。
	for _, rc := range c.RouteClasses {
		weights, ok := c.Weights.ByRouteClass[rc]
		if !ok {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidWeight,
				fmt.Sprintf("%s.%s", FieldWeights, rc),
				nil,
				"weights must be configured for each route class",
			))
			continue
		}
		sum := 0
		// 验证每个权重值的范围，并计算权重总和。
		for metric, w := range weights {
			if w < 0 || w > 10000 {
				errs = append(errs, lberrors.NewConfigError(
					lberrors.CodeInvalidWeight,
					fmt.Sprintf("%s.%s.%s", FieldWeights, rc, metric),
					w,
					"must be between 0 and 10000",
				))
			}
			sum += w
		}
		// 检查权重总和是否为 10000。
		if sum != 10000 {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidWeightSum,
				fmt.Sprintf("%s.%s", FieldWeights, rc),
				sum,
				"weight sum must equal 10000",
			))
		}

		// 对于 LLM 路由类别，检查是否包含必需的权重项。
		if rc == types.RouteLLMPrefill || rc == types.RouteLLMDecode {
			for _, metric := range requiredLLMMetrics {
				if _, ok := weights[metric]; !ok {
					errs = append(errs, lberrors.NewConfigError(
						lberrors.CodeMissingLLMWeights,
						fmt.Sprintf("%s.%s.%s", FieldWeights, rc, metric),
						nil,
						"llm route classes must include ttft,tpot,kv_hit",
					))
				}
			}
		}
	}

	// 如果有任何错误，返回合并的错误列表。
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// isValidRouteClass 判断路由类别是否为受支持的内置枚举值。
func isValidRouteClass(routeClass types.RouteClass) bool {
	switch routeClass {
	case types.RouteGeneric, types.RouteLLMPrefill, types.RouteLLMDecode:
		return true
	default:
		return false
	}
}

// WithTopK 设置 TopK。
func WithTopK(v int) Option {
	return func(c *Config) {
		c.TopK = v
	}
}

// WithFallback 设置回退链。
func WithFallback(chain ...string) Option {
	return func(c *Config) {
		c.FallbackChain = append([]string(nil), chain...)
	}
}

// WithAlgorithm 设置路由类对应算法。
func WithAlgorithm(routeClass types.RouteClass, pluginName string) Option {
	return func(c *Config) {
		if c.Plugins.Algorithms == nil {
			c.Plugins.Algorithms = make(map[types.RouteClass]string)
		}
		c.Plugins.Algorithms[routeClass] = pluginName
	}
}

// WithAlgorithmString 保留字符串入参版本，兼容旧调用。
func WithAlgorithmString(routeClass, pluginName string) Option {
	return WithAlgorithm(types.RouteClass(routeClass), pluginName)
}

// WithPolicies 设置策略链。
func WithPolicies(names ...string) Option {
	return func(c *Config) {
		c.Plugins.Policies = append([]string(nil), names...)
	}
}

// WithObjective 设置目标函数插件。
func WithObjective(name string, timeoutMs int, enabled bool) Option {
	return func(c *Config) {
		c.Plugins.Objective.Name = name
		c.Plugins.Objective.TimeoutMs = timeoutMs
		c.Plugins.Objective.Enabled = enabled
	}
}

// WithWeight 设置某路由类某指标权重。
func WithWeight(routeClass types.RouteClass, metric string, w int) Option {
	return func(c *Config) {
		if c.Weights.ByRouteClass == nil {
			c.Weights.ByRouteClass = make(map[types.RouteClass]map[string]int)
		}
		if c.Weights.ByRouteClass[routeClass] == nil {
			c.Weights.ByRouteClass[routeClass] = make(map[string]int)
		}
		c.Weights.ByRouteClass[routeClass][metric] = w
	}
}

// WithWeightString 保留字符串入参版本，兼容旧调用。
func WithWeightString(routeClass, metric string, w int) Option {
	return WithWeight(types.RouteClass(routeClass), metric, w)
}

// WithTelemetrySink 设置 telemetry sink。
func WithTelemetrySink(s telemetry.Sink) Option {
	return func(c *Config) {
		c.TelemetrySink = s
	}
}
