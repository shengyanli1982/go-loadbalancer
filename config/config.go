package config

import (
	"errors"
	"fmt"

	lberrors "go-loadbalancer/errors"
	"go-loadbalancer/registry"
	"go-loadbalancer/telemetry"
	"go-loadbalancer/types"
)

const (
	fallbackPolicyRanked = "policy_ranked"
)

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
		FallbackChain: []string{fallbackPolicyRanked, "least_request", "p2c"},
		Plugins: PluginConfig{
			Algorithms: map[types.RouteClass]string{
				types.RouteGeneric:    "p2c",
				types.RouteLLMPrefill: "least_request",
				types.RouteLLMDecode:  "least_request",
			},
			Policies: []string{"health_gate", "tenant_quota"},
			Objective: ObjectiveConfig{
				Enabled:   false,
				Name:      "weighted_objective",
				TimeoutMs: 3,
			},
		},
		Weights: WeightConfig{
			ByRouteClass: map[types.RouteClass]map[string]int{
				types.RouteGeneric: {
					"queue":       5000,
					"p95_latency": 3000,
					"error_rate":  2000,
				},
				types.RouteLLMPrefill: {
					"queue":       2000,
					"p95_latency": 1500,
					"error_rate":  1500,
					"ttft":        2500,
					"tpot":        1000,
					"kv_hit":      1500,
				},
				types.RouteLLMDecode: {
					"queue":       2000,
					"p95_latency": 1500,
					"error_rate":  1500,
					"ttft":        1000,
					"tpot":        2500,
					"kv_hit":      1500,
				},
			},
		},
		TelemetrySink: telemetry.NoopSink{},
	}
}

// Validate 对配置执行强校验。
func (c *Config) Validate() error {
	var errs []error

	if c.TopK < 1 || c.TopK > 32 {
		errs = append(errs, lberrors.NewConfigError(
			lberrors.CodeInvalidTopK,
			"top_k",
			c.TopK,
			"must be between 1 and 32",
		))
	}

	if len(c.RouteClasses) == 0 {
		errs = append(errs, lberrors.NewConfigError(
			lberrors.CodeInvalidRouteClass,
			"route_classes",
			c.RouteClasses,
			"must not be empty",
		))
	}

	seenRouteClass := make(map[types.RouteClass]struct{}, len(c.RouteClasses))
	for i, rc := range c.RouteClasses {
		if !isValidRouteClass(rc) {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidRouteClass,
				fmt.Sprintf("route_classes[%d]", i),
				rc,
				"must be one of generic,llm-prefill,llm-decode",
			))
			continue
		}
		if _, ok := seenRouteClass[rc]; ok {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidRouteClass,
				fmt.Sprintf("route_classes[%d]", i),
				rc,
				"must not contain duplicates",
			))
			continue
		}
		seenRouteClass[rc] = struct{}{}
	}

	for _, rc := range c.RouteClasses {
		name, ok := c.Plugins.Algorithms[rc]
		if !ok || name == "" {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeMissingAlgorithmBinding,
				fmt.Sprintf("plugins.algorithms.%s", rc),
				name,
				"must bind one algorithm per route class",
			))
			continue
		}
		if !registry.HasAlgorithm(name) {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeMissingAlgorithmBinding,
				fmt.Sprintf("plugins.algorithms.%s", rc),
				name,
				"algorithm is not registered",
			))
		}
	}

	seenPolicy := make(map[string]struct{}, len(c.Plugins.Policies))
	for i, p := range c.Plugins.Policies {
		if _, ok := seenPolicy[p]; ok {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeDuplicatePolicy,
				fmt.Sprintf("plugins.policies[%d]", i),
				p,
				"policy must be unique",
			))
			continue
		}
		seenPolicy[p] = struct{}{}
		if !registry.HasPolicy(p) {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeUnknownPolicy,
				fmt.Sprintf("plugins.policies[%d]", i),
				p,
				"policy is not registered",
			))
		}
	}

	if c.Plugins.Objective.Enabled {
		if c.Plugins.Objective.Name == "" {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidObjective,
				"plugins.objective.name",
				c.Plugins.Objective.Name,
				"must not be empty when objective is enabled",
			))
		} else if !registry.HasObjective(c.Plugins.Objective.Name) {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidObjective,
				"plugins.objective.name",
				c.Plugins.Objective.Name,
				"objective is not registered",
			))
		}
		if c.Plugins.Objective.TimeoutMs < 1 || c.Plugins.Objective.TimeoutMs > 20 {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidObjectiveTimeout,
				"plugins.objective.timeout_ms",
				c.Plugins.Objective.TimeoutMs,
				"must be between 1 and 20",
			))
		}
	}

	if len(c.FallbackChain) == 0 {
		errs = append(errs, lberrors.NewConfigError(
			lberrors.CodeInvalidFallbackChain,
			"fallback_chain",
			c.FallbackChain,
			"must not be empty",
		))
	}
	seenFallback := make(map[string]struct{}, len(c.FallbackChain))
	for i, s := range c.FallbackChain {
		if s == "" {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidFallbackChain,
				fmt.Sprintf("fallback_chain[%d]", i),
				s,
				"must not be empty",
			))
			continue
		}
		if _, ok := seenFallback[s]; ok {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidFallbackChain,
				fmt.Sprintf("fallback_chain[%d]", i),
				s,
				"must not contain duplicates",
			))
			continue
		}
		seenFallback[s] = struct{}{}
		if s != fallbackPolicyRanked && !registry.HasAlgorithm(s) {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidFallbackChain,
				fmt.Sprintf("fallback_chain[%d]", i),
				s,
				"must be policy_ranked or a registered algorithm",
			))
		}
	}

	for _, rc := range c.RouteClasses {
		weights, ok := c.Weights.ByRouteClass[rc]
		if !ok {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidWeight,
				fmt.Sprintf("weights.%s", rc),
				nil,
				"weights must be configured for each route class",
			))
			continue
		}
		sum := 0
		for metric, w := range weights {
			if w < 0 || w > 10000 {
				errs = append(errs, lberrors.NewConfigError(
					lberrors.CodeInvalidWeight,
					fmt.Sprintf("weights.%s.%s", rc, metric),
					w,
					"must be between 0 and 10000",
				))
			}
			sum += w
		}
		if sum != 10000 {
			errs = append(errs, lberrors.NewConfigError(
				lberrors.CodeInvalidWeightSum,
				fmt.Sprintf("weights.%s", rc),
				sum,
				"weight sum must equal 10000",
			))
		}

		if rc == types.RouteLLMPrefill || rc == types.RouteLLMDecode {
			for _, metric := range []string{"ttft", "tpot", "kv_hit"} {
				if _, ok := weights[metric]; !ok {
					errs = append(errs, lberrors.NewConfigError(
						lberrors.CodeMissingLLMWeights,
						fmt.Sprintf("weights.%s.%s", rc, metric),
						nil,
						"llm route classes must include ttft,tpot,kv_hit",
					))
				}
			}
		}
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

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
func WithAlgorithm(routeClass, pluginName string) Option {
	return func(c *Config) {
		if c.Plugins.Algorithms == nil {
			c.Plugins.Algorithms = make(map[types.RouteClass]string)
		}
		c.Plugins.Algorithms[types.RouteClass(routeClass)] = pluginName
	}
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
func WithWeight(routeClass, metric string, w int) Option {
	return func(c *Config) {
		rc := types.RouteClass(routeClass)
		if c.Weights.ByRouteClass == nil {
			c.Weights.ByRouteClass = make(map[types.RouteClass]map[string]int)
		}
		if c.Weights.ByRouteClass[rc] == nil {
			c.Weights.ByRouteClass[rc] = make(map[string]int)
		}
		c.Weights.ByRouteClass[rc][metric] = w
	}
}

// WithTelemetrySink 设置 telemetry sink。
func WithTelemetrySink(s telemetry.Sink) Option {
	return func(c *Config) {
		c.TelemetrySink = s
	}
}
