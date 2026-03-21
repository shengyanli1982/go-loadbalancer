package config

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/go-playground/validator/v10"
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
	AlgorithmConsistentHash     = "ch"
	AlgorithmP2C                = "p2c"
	AlgorithmLeastRequest       = "lr"

	// 内置策略插件名。
	PolicyHealthGate         = "health_gate"
	PolicyTenantQuota        = "tenant_quota"
	PolicyLLMBudgetGate      = "llm_budget_gate"
	PolicyLLMKVAffinity      = "llm_kv_affinity"
	PolicyLLMSessionAffinity = "llm_session_affinity"
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
	FieldPluginsObjectiveMaxConc = "plugins.objective.max_concurrent"
	FieldFallbackChain           = "fallback_chain"
	FieldInputGuard              = "input_guard"
	FieldReliabilityPilot        = "reliability_pilot"
	FieldWeights                 = "weights"
)

var requiredLLMMetrics = [...]string{
	MetricTTFT,
	MetricTPOT,
	MetricKVHit,
}

var cfgValidator = validator.New(validator.WithRequiredStructEnabled())

type basicValidationView struct {
	TopK             int                `validate:"min=1,max=32"`
	RouteClasses     []types.RouteClass `validate:"min=1,unique,dive,oneof=generic llm-prefill llm-decode"`
	FallbackChain    []string           `validate:"min=1,unique,dive,required"`
	Policies         []string           `validate:"unique,dive,required"`
	ObjectiveName    *string            `validate:"omitempty,min=1"`
	ObjectiveTimeout *int               `validate:"omitempty,min=1,max=200"`
	ObjectiveMaxConc *int               `validate:"omitempty,min=1,max=2048"`
}

type validationErrorMapping struct {
	code       string
	field      string
	constraint string
	value      func(*Config) any
}

var basicValidationErrorMappings = map[string]validationErrorMapping{
	"TopK": {
		code:       lberrors.CodeInvalidTopK,
		field:      FieldTopK,
		constraint: "must be between 1 and 32",
		value: func(c *Config) any {
			return c.TopK
		},
	},
	"ObjectiveTimeout": {
		code:       lberrors.CodeInvalidObjectiveTimeout,
		field:      FieldPluginsObjectiveTimeout,
		constraint: "must be between 1 and 200",
		value: func(c *Config) any {
			return c.Plugins.Objective.TimeoutMs
		},
	},
	"ObjectiveMaxConc": {
		code:       lberrors.CodeInvalidObjective,
		field:      FieldPluginsObjectiveMaxConc,
		constraint: "must be between 1 and 2048 when set",
		value: func(c *Config) any {
			return c.Plugins.Objective.MaxConcurrent
		},
	},
	"RouteClasses": {
		code:       lberrors.CodeInvalidRouteClass,
		field:      FieldRouteClasses,
		constraint: "must not be empty",
		value: func(c *Config) any {
			return c.RouteClasses
		},
	},
	"FallbackChain": {
		code:       lberrors.CodeInvalidFallbackChain,
		field:      FieldFallbackChain,
		constraint: "must not be empty",
		value: func(c *Config) any {
			return c.FallbackChain
		},
	},
	"Policies": {
		code:       lberrors.CodeDuplicatePolicy,
		field:      FieldPluginsPolicies,
		constraint: "policy must be unique",
		value: func(c *Config) any {
			return c.Plugins.Policies
		},
	},
	"ObjectiveName": {
		code:       lberrors.CodeInvalidObjective,
		field:      FieldPluginsObjectiveName,
		constraint: "must not be empty when objective is enabled",
		value: func(c *Config) any {
			return c.Plugins.Objective.Name
		},
	},
}

// ObjectiveConfig 定义目标函数插件配置。
type ObjectiveConfig struct {
	Enabled   bool
	Name      string
	TimeoutMs int
	// MaxConcurrent 为 objective 并发上限。0 表示使用 balancer 内置默认值。
	MaxConcurrent int
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
	TopK             int
	RouteClasses     []types.RouteClass
	FallbackChain    []string
	SnapshotTTLGuard bool
	InputGuard       bool
	ReliabilityPilot bool
	RouteProfiles    RouteProfileConfig
	Plugins          PluginConfig
	Weights          WeightConfig
	TelemetrySink    telemetry.Sink
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
		FallbackChain:    []string{FallbackPolicyRanked, AlgorithmLeastRequest, AlgorithmP2C},
		SnapshotTTLGuard: false,
		InputGuard:       false,
		ReliabilityPilot: false,
		RouteProfiles: RouteProfileConfig{
			ByRouteClass: map[types.RouteClass]RouteProfile{},
		},
		Plugins: PluginConfig{
			Algorithms: map[types.RouteClass]string{
				types.RouteGeneric:    AlgorithmP2C,
				types.RouteLLMPrefill: AlgorithmLeastRequest,
				types.RouteLLMDecode:  AlgorithmLeastRequest,
			},
			Policies: []string{
				PolicyHealthGate,
				PolicyTenantQuota,
				PolicyLLMBudgetGate,
				PolicyLLMTokenAwareQueue,
				PolicyLLMSessionAffinity,
				PolicyLLMStageAware,
				PolicyLLMKVAffinity,
			},
			Objective: ObjectiveConfig{
				Enabled:       false,
				Name:          ObjectiveWeighted,
				TimeoutMs:     3,
				MaxConcurrent: 128,
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
	errs = append(errs, validateBasicFields(c)...)
	errs = append(errs, validateAlgorithmBindings(c)...)
	errs = append(errs, validatePolicyRegistrations(c)...)
	errs = append(errs, validateRouteProfiles(c)...)
	errs = append(errs, validateObjectiveRegistration(c)...)
	errs = append(errs, validateFallbackRegistrations(c)...)

	// 仅在启用 weighted_objective 时强制执行内置权重约束。
	if c.Plugins.Objective.Enabled && c.Plugins.Objective.Name == ObjectiveWeighted {
		errs = append(errs, validateWeightedObjectiveWeights(c)...)
	}

	// 如果有任何错误，返回合并的错误列表。
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

func validateAlgorithmBindings(c *Config) []error {
	seenRouteClass := make(map[types.RouteClass]struct{}, len(c.RouteClasses))
	out := make([]error, 0)
	for _, rc := range c.RouteClasses {
		if !isValidRouteClass(rc) {
			continue
		}
		if _, ok := seenRouteClass[rc]; ok {
			continue
		}
		seenRouteClass[rc] = struct{}{}

		name := c.resolveRouteProfile(rc).Algorithm
		if name == "" {
			out = append(out, lberrors.NewConfigError(
				lberrors.CodeMissingAlgorithmBinding,
				fmt.Sprintf("%s.%s", FieldPluginsAlgorithms, rc),
				name,
				"must bind one algorithm per route class",
			))
			continue
		}
		if !registry.HasAlgorithm(name) {
			out = append(out, lberrors.NewConfigError(
				lberrors.CodeMissingAlgorithmBinding,
				fmt.Sprintf("%s.%s", FieldPluginsAlgorithms, rc),
				name,
				"algorithm is not registered",
			))
		}
	}
	return out
}

func validatePolicyRegistrations(c *Config) []error {
	seenPolicy := make(map[string]struct{}, len(c.Plugins.Policies))
	out := make([]error, 0)
	for i, p := range c.Plugins.Policies {
		if p == "" {
			continue
		}
		if _, ok := seenPolicy[p]; ok {
			continue
		}
		seenPolicy[p] = struct{}{}
		if !registry.HasPolicy(p) {
			out = append(out, lberrors.NewConfigError(
				lberrors.CodeUnknownPolicy,
				fmt.Sprintf("%s[%d]", FieldPluginsPolicies, i),
				p,
				"policy is not registered",
			))
		}
	}
	return out
}

func validateObjectiveRegistration(c *Config) []error {
	if !c.Plugins.Objective.Enabled || c.Plugins.Objective.Name == "" {
		return nil
	}
	if registry.HasObjective(c.Plugins.Objective.Name) {
		return nil
	}
	return []error{lberrors.NewConfigError(
		lberrors.CodeInvalidObjective,
		FieldPluginsObjectiveName,
		c.Plugins.Objective.Name,
		"objective is not registered",
	)}
}

func validateFallbackRegistrations(c *Config) []error {
	seenFallback := make(map[string]struct{}, len(c.FallbackChain))
	out := make([]error, 0)
	for i, s := range c.FallbackChain {
		if s == "" {
			continue
		}
		if _, ok := seenFallback[s]; ok {
			continue
		}
		seenFallback[s] = struct{}{}
		if s != FallbackPolicyRanked && !registry.HasAlgorithm(s) {
			out = append(out, lberrors.NewConfigError(
				lberrors.CodeInvalidFallbackChain,
				fmt.Sprintf("%s[%d]", FieldFallbackChain, i),
				s,
				"must be policy_ranked or a registered algorithm",
			))
		}
	}
	return out
}

func validateWeightedObjectiveWeights(c *Config) []error {
	out := make([]error, 0)
	for _, rc := range c.RouteClasses {
		weights, ok := c.Weights.ByRouteClass[rc]
		if !ok {
			out = append(out, lberrors.NewConfigError(
				lberrors.CodeInvalidWeight,
				fmt.Sprintf("%s.%s", FieldWeights, rc),
				nil,
				"weights must be configured for each route class",
			))
			continue
		}

		sum := 0
		for metric, w := range weights {
			if w < 0 || w > 10000 {
				out = append(out, lberrors.NewConfigError(
					lberrors.CodeInvalidWeight,
					fmt.Sprintf("%s.%s.%s", FieldWeights, rc, metric),
					w,
					"must be between 0 and 10000",
				))
			}
			sum += w
		}
		if sum != 10000 {
			out = append(out, lberrors.NewConfigError(
				lberrors.CodeInvalidWeightSum,
				fmt.Sprintf("%s.%s", FieldWeights, rc),
				sum,
				"weight sum must equal 10000",
			))
		}

		if rc == types.RouteLLMPrefill || rc == types.RouteLLMDecode {
			for _, metric := range requiredLLMMetrics {
				if _, ok := weights[metric]; !ok {
					out = append(out, lberrors.NewConfigError(
						lberrors.CodeMissingLLMWeights,
						fmt.Sprintf("%s.%s.%s", FieldWeights, rc, metric),
						nil,
						"llm route classes must include ttft,tpot,kv_hit",
					))
				}
			}
		}
	}
	return out
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

func validateBasicFields(c *Config) []error {
	view := basicValidationView{
		TopK:          c.TopK,
		RouteClasses:  c.RouteClasses,
		FallbackChain: c.FallbackChain,
		Policies:      c.Plugins.Policies,
	}
	if c.Plugins.Objective.Enabled {
		name := c.Plugins.Objective.Name
		view.ObjectiveName = &name
		timeout := c.Plugins.Objective.TimeoutMs
		view.ObjectiveTimeout = &timeout
		if c.Plugins.Objective.MaxConcurrent != 0 {
			maxConc := c.Plugins.Objective.MaxConcurrent
			view.ObjectiveMaxConc = &maxConc
		}
	}

	err := cfgValidator.Struct(view)
	if err == nil {
		return nil
	}

	var validationErrs validator.ValidationErrors
	if !errors.As(err, &validationErrs) {
		return []error{lberrors.NewConfigError(
			lberrors.CodeInvalidObjective,
			"config.validation",
			nil,
			err.Error(),
		)}
	}

	out := make([]error, 0, len(validationErrs))
	for _, item := range validationErrs {
		out = append(out, mapBasicValidationError(item, c))
	}
	return out
}

func mapBasicValidationError(fieldErr validator.FieldError, c *Config) error {
	fieldName, fieldIndex := splitIndexedStructField(fieldErr.StructField())

	if fieldName == "RouteClasses" {
		switch fieldErr.Tag() {
		case "unique":
			dupIdx, dupValue := firstDuplicateRouteClass(c.RouteClasses)
			field := FieldRouteClasses
			if dupIdx >= 0 {
				field = fmt.Sprintf("%s[%d]", FieldRouteClasses, dupIdx)
			}
			return lberrors.NewConfigError(
				lberrors.CodeInvalidRouteClass,
				field,
				dupValue,
				"must not contain duplicates",
			)
		case "oneof":
			field := FieldRouteClasses
			if fieldIndex >= 0 {
				field = fmt.Sprintf("%s[%d]", FieldRouteClasses, fieldIndex)
			}
			return lberrors.NewConfigError(
				lberrors.CodeInvalidRouteClass,
				field,
				fieldErr.Value(),
				"must be one of generic,llm-prefill,llm-decode",
			)
		}
	}

	if fieldName == "FallbackChain" {
		switch fieldErr.Tag() {
		case "unique":
			dupIdx, dupValue := firstDuplicateString(c.FallbackChain)
			field := FieldFallbackChain
			if dupIdx >= 0 {
				field = fmt.Sprintf("%s[%d]", FieldFallbackChain, dupIdx)
			}
			return lberrors.NewConfigError(
				lberrors.CodeInvalidFallbackChain,
				field,
				dupValue,
				"must not contain duplicates",
			)
		case "required":
			field := FieldFallbackChain
			if fieldIndex >= 0 {
				field = fmt.Sprintf("%s[%d]", FieldFallbackChain, fieldIndex)
			}
			return lberrors.NewConfigError(
				lberrors.CodeInvalidFallbackChain,
				field,
				fieldErr.Value(),
				"must not be empty",
			)
		}
	}

	if fieldName == "Policies" && fieldErr.Tag() == "unique" {
		dupIdx, dupValue := firstDuplicateString(c.Plugins.Policies)
		field := FieldPluginsPolicies
		if dupIdx >= 0 {
			field = fmt.Sprintf("%s[%d]", FieldPluginsPolicies, dupIdx)
		}
		return lberrors.NewConfigError(
			lberrors.CodeDuplicatePolicy,
			field,
			dupValue,
			"policy must be unique",
		)
	}
	if fieldName == "Policies" && fieldErr.Tag() == "required" {
		field := FieldPluginsPolicies
		if fieldIndex >= 0 {
			field = fmt.Sprintf("%s[%d]", FieldPluginsPolicies, fieldIndex)
		}
		return lberrors.NewConfigError(
			lberrors.CodeUnknownPolicy,
			field,
			fieldErr.Value(),
			"policy is not registered",
		)
	}

	mapping, ok := basicValidationErrorMappings[fieldName]
	if !ok {
		return lberrors.NewConfigError(
			defaultValidationCodeForField(fieldName),
			fmt.Sprintf("config.validation.%s", fieldErr.StructField()),
			fieldErr.Value(),
			fieldErr.Error(),
		)
	}
	return lberrors.NewConfigError(
		mapping.code,
		mapping.field,
		mapping.value(c),
		mapping.constraint,
	)
}

func splitIndexedStructField(structField string) (string, int) {
	parts := strings.SplitN(structField, "[", 2)
	if len(parts) < 2 {
		return structField, -1
	}
	idxPart := strings.TrimSuffix(parts[1], "]")
	idx, err := strconv.Atoi(idxPart)
	if err != nil {
		return parts[0], -1
	}
	return parts[0], idx
}

func firstDuplicateString(values []string) (int, string) {
	seen := make(map[string]struct{}, len(values))
	for i, value := range values {
		if _, ok := seen[value]; ok {
			return i, value
		}
		seen[value] = struct{}{}
	}
	return -1, ""
}

func firstDuplicateRouteClass(values []types.RouteClass) (int, types.RouteClass) {
	seen := make(map[types.RouteClass]struct{}, len(values))
	for i, value := range values {
		if _, ok := seen[value]; ok {
			return i, value
		}
		seen[value] = struct{}{}
	}
	return -1, ""
}

func defaultValidationCodeForField(fieldName string) string {
	switch fieldName {
	case "TopK":
		return lberrors.CodeInvalidTopK
	case "RouteClasses":
		return lberrors.CodeInvalidRouteClass
	case "FallbackChain":
		return lberrors.CodeInvalidFallbackChain
	case "Policies":
		return lberrors.CodeDuplicatePolicy
	case "ObjectiveName", "ObjectiveTimeout", "ObjectiveMaxConc":
		return lberrors.CodeInvalidObjective
	default:
		return lberrors.CodeInvalidObjective
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

// WithObjectiveMaxConcurrent 设置 objective 的并发上限（仅在 objective 启用时生效）。
func WithObjectiveMaxConcurrent(v int) Option {
	return func(c *Config) {
		c.Plugins.Objective.MaxConcurrent = v
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

// WithSnapshotTTLGuard 设置是否启用快照新鲜度 TTL 防护。
func WithSnapshotTTLGuard(enabled bool) Option {
	return func(c *Config) {
		c.SnapshotTTLGuard = enabled
	}
}

// WithInputGuard 设置是否启用 Route 输入防御校验。
func WithInputGuard(enabled bool) Option {
	return func(c *Config) {
		c.InputGuard = enabled
	}
}

// WithReliabilityPilot toggles pool priority and outlier isolation pilot.
func WithReliabilityPilot(enabled bool) Option {
	return func(c *Config) {
		c.ReliabilityPilot = enabled
	}
}

// WithTelemetrySink 设置路由遥测事件接收器。
func WithTelemetrySink(s telemetry.Sink) Option {
	return func(c *Config) {
		c.TelemetrySink = s
	}
}
