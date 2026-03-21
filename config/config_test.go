package config_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/config"
	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/builtin"
	"github.com/shengyanli1982/go-loadbalancer/plugin/objective"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

type customObjective struct{}

func (customObjective) Name() string { return "config_custom_objective" }

func (customObjective) Choose(_ types.RequestContext, candidates []types.Candidate) (types.Candidate, error) {
	if len(candidates) == 0 {
		return types.Candidate{}, lberrors.ErrNoCandidate
	}
	return candidates[0], nil
}

// TestDefaultConfigValidate 验证默认配置可以通过校验。
func TestDefaultConfigValidate(t *testing.T) {
	cfg := config.DefaultConfig()
	require.NoError(t, cfg.Validate())
}

func TestDefaultConfigUsesOnlyCapacityAndHealthPolicies(t *testing.T) {
	cfg := config.DefaultConfig()
	assert.Equal(t, []string{
		config.PolicyHealthGate,
		config.PolicyTenantQuota,
		config.PolicyLLMBudgetGate,
	}, cfg.Plugins.Policies)
}

func TestDefaultConfigLLMWeightsExcludeKVHit(t *testing.T) {
	cfg := config.DefaultConfig()

	prefillWeights := cfg.Weights.ByRouteClass[types.RouteLLMPrefill]
	decodeWeights := cfg.Weights.ByRouteClass[types.RouteLLMDecode]

	assert.NotContains(t, prefillWeights, config.MetricKVHit)
	assert.NotContains(t, decodeWeights, config.MetricKVHit)
	assert.Equal(t, 10000, sumWeights(prefillWeights))
	assert.Equal(t, 10000, sumWeights(decodeWeights))
}

func TestDefaultConfigObjectiveConcurrencyGuard(t *testing.T) {
	cfg := config.DefaultConfig()
	assert.Equal(t, 128, cfg.Plugins.Objective.MaxConcurrent)
}

func TestDefaultConfigInputGuardDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	assert.False(t, cfg.InputGuard)
}

func TestDefaultConfigReliabilityPilotDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	assert.False(t, cfg.ReliabilityPilot)
}

// TestValidateReturnsJoinedErrors 验证多项错误会以 join 形式返回。
func TestValidateReturnsJoinedErrors(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TopK = 0
	cfg.Plugins.Objective.Enabled = true
	cfg.Plugins.Objective.Name = config.ObjectiveWeighted
	cfg.Plugins.Objective.TimeoutMs = 3
	cfg.RouteClasses = []types.RouteClass{"bad", types.RouteLLMPrefill}
	cfg.Plugins.Policies = []string{config.PolicyHealthGate, config.PolicyHealthGate, "missing_policy"}
	cfg.Weights.ByRouteClass[types.RouteLLMPrefill] = map[string]int{
		config.MetricQueue:     6000,
		config.MetricErrorRate: 3000,
	}

	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, lberrors.ErrInvalidConfig)

	cfgErrors := flattenConfigErrors(err)
	codes := make(map[string]struct{}, len(cfgErrors))
	for _, e := range cfgErrors {
		codes[e.Code] = struct{}{}
	}

	assert.Contains(t, codes, lberrors.CodeInvalidTopK)
	assert.Contains(t, codes, lberrors.CodeInvalidRouteClass)
	assert.Contains(t, codes, lberrors.CodeDuplicatePolicy)
	assert.Contains(t, codes, lberrors.CodeUnknownPolicy)
	assert.Contains(t, codes, lberrors.CodeInvalidWeightSum)
	assert.Contains(t, codes, lberrors.CodeMissingLLMWeights)
}

// TestValidateUnknownAlgorithmBinding 验证算法绑定未注册时返回对应错误码。
func TestValidateUnknownAlgorithmBinding(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Plugins.Algorithms[types.RouteGeneric] = "unknown_algo"

	err := cfg.Validate()
	require.Error(t, err)
	assert.ErrorIs(t, err, lberrors.ErrInvalidConfig)

	var cfgErr *lberrors.ConfigError
	require.True(t, errors.As(err, &cfgErr))
	assert.Equal(t, lberrors.CodeMissingAlgorithmBinding, cfgErr.Code)
}

// TestWithSnapshotTTLGuard 验证快照 TTL 防护开关可通过 Option 正常设置。
func TestWithSnapshotTTLGuard(t *testing.T) {
	cfg := config.DefaultConfig()
	assert.False(t, cfg.SnapshotTTLGuard)

	config.WithSnapshotTTLGuard(true)(&cfg)
	assert.True(t, cfg.SnapshotTTLGuard)
}

func TestWithInputGuard(t *testing.T) {
	cfg := config.DefaultConfig()
	assert.False(t, cfg.InputGuard)

	config.WithInputGuard(true)(&cfg)
	assert.True(t, cfg.InputGuard)
}

func TestWithReliabilityPilot(t *testing.T) {
	cfg := config.DefaultConfig()
	assert.False(t, cfg.ReliabilityPilot)

	config.WithReliabilityPilot(true)(&cfg)
	assert.True(t, cfg.ReliabilityPilot)
}

func TestRouteProfileForFallsBackToLegacyBindings(t *testing.T) {
	cfg := config.DefaultConfig()

	profile := cfg.RouteProfileFor(types.RouteLLMPrefill)
	assert.Equal(t, cfg.Plugins.Algorithms[types.RouteLLMPrefill], profile.Algorithm)
	assert.Equal(t, cfg.Plugins.Policies, profile.Policies)

	profile.Policies[0] = "mutated_policy"
	assert.NotEqual(t, profile.Policies[0], cfg.Plugins.Policies[0])
}

func TestWithRouteProfileOverridesLegacyBindings(t *testing.T) {
	cfg := config.DefaultConfig()
	config.WithRouteProfile(types.RouteGeneric, config.RouteProfile{
		Algorithm: config.AlgorithmRoundRobin,
		Policies:  []string{config.PolicyHealthGate},
		DegradeChain: []string{
			config.FallbackPolicyRanked,
			config.AlgorithmRoundRobin,
		},
	})(&cfg)

	profile := cfg.RouteProfileFor(types.RouteGeneric)
	assert.Equal(t, config.AlgorithmRoundRobin, profile.Algorithm)
	assert.Equal(t, []string{config.PolicyHealthGate}, profile.Policies)
	assert.Equal(t, []string{config.FallbackPolicyRanked, config.AlgorithmRoundRobin}, profile.DegradeChain)
}

func TestValidateAllowsProfileAlgorithmWithoutLegacyBinding(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.RouteClasses = []types.RouteClass{types.RouteGeneric}
	cfg.Plugins.Algorithms = map[types.RouteClass]string{}
	cfg.Plugins.Policies = nil
	config.WithRouteProfile(types.RouteGeneric, config.RouteProfile{
		Algorithm: config.AlgorithmLeastRequest,
		Policies:  []string{config.PolicyHealthGate},
	})(&cfg)

	require.NoError(t, cfg.Validate())
}

func TestValidateRouteProfileInvalidKey(t *testing.T) {
	cfg := config.DefaultConfig()
	config.WithRouteProfile(types.RouteClass("bad"), config.RouteProfile{
		Algorithm: config.AlgorithmLeastRequest,
		Policies:  []string{config.PolicyHealthGate},
	})(&cfg)

	err := cfg.Validate()
	require.Error(t, err)
	cfgErrors := flattenConfigErrors(err)
	assert.True(t, hasConfigError(cfgErrors, lberrors.CodeInvalidRouteClass, "route_profiles.bad"))
}

func TestValidateRouteProfileUnknownAlgorithm(t *testing.T) {
	cfg := config.DefaultConfig()
	config.WithRouteProfile(types.RouteGeneric, config.RouteProfile{
		Algorithm: "unknown_algo",
		Policies:  []string{config.PolicyHealthGate},
	})(&cfg)

	err := cfg.Validate()
	require.Error(t, err)
	cfgErrors := flattenConfigErrors(err)
	assert.True(t, hasConfigError(cfgErrors, lberrors.CodeMissingAlgorithmBinding, "route_profiles.generic.algorithm"))
}

func TestWithRouteDegradeChainOverridesFallbackChain(t *testing.T) {
	cfg := config.DefaultConfig()
	config.WithRouteDegradeChain(types.RouteLLMDecode, config.AlgorithmRoundRobin, config.AlgorithmLeastRequest)(&cfg)

	profile := cfg.RouteProfileFor(types.RouteLLMDecode)
	assert.Equal(t, []string{config.AlgorithmRoundRobin, config.AlgorithmLeastRequest}, profile.DegradeChain)
}

func TestValidateRouteProfileInvalidDegradeChain(t *testing.T) {
	cfg := config.DefaultConfig()
	config.WithRouteProfile(types.RouteLLMDecode, config.RouteProfile{
		DegradeChain: []string{"unknown_degrade_algo"},
	})(&cfg)

	err := cfg.Validate()
	require.Error(t, err)
	cfgErrors := flattenConfigErrors(err)
	assert.True(t, hasConfigError(cfgErrors, lberrors.CodeInvalidFallbackChain, "route_profiles.llm-decode.degrade_chain[0]"))
}

// TestValidateSkipWeightChecksWhenObjectiveDisabled 验证 objective 关闭时不强制校验 weighted 权重规则。
func TestValidateSkipWeightChecksWhenObjectiveDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Weights.ByRouteClass = map[types.RouteClass]map[string]int{}

	require.NoError(t, cfg.Validate())
}

// TestValidateWeightedObjectiveRequiresWeights 验证启用 weighted objective 后会启用严格权重校验。
func TestValidateWeightedObjectiveRequiresWeights(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Plugins.Objective.Enabled = true
	cfg.Plugins.Objective.Name = config.ObjectiveWeighted
	cfg.Plugins.Objective.TimeoutMs = 3
	delete(cfg.Weights.ByRouteClass, types.RouteGeneric)

	err := cfg.Validate()
	require.Error(t, err)

	cfgErrors := flattenConfigErrors(err)
	codes := make(map[string]struct{}, len(cfgErrors))
	for _, e := range cfgErrors {
		codes[e.Code] = struct{}{}
	}
	assert.Contains(t, codes, lberrors.CodeInvalidWeight)
}

// TestValidateCustomObjectiveSkipsWeightedRules 验证启用自定义 objective 时不强制 weighted 权重规则。
func TestValidateCustomObjectiveSkipsWeightedRules(t *testing.T) {
	err := registry.RegisterObjective(customObjective{})
	if err != nil && !errors.Is(err, lberrors.ErrDuplicatePlugin) {
		require.NoError(t, err)
	}

	cfg := config.DefaultConfig()
	cfg.Plugins.Objective.Enabled = true
	cfg.Plugins.Objective.Name = "config_custom_objective"
	cfg.Plugins.Objective.TimeoutMs = 3
	cfg.Weights.ByRouteClass = map[types.RouteClass]map[string]int{}

	require.NoError(t, cfg.Validate())
}

// TestValidateObjectiveTimeoutInvalid 验证启用 objective 时超时参数会被校验。
func TestValidateObjectiveTimeoutInvalid(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Plugins.Objective.Enabled = true
	cfg.Plugins.Objective.Name = config.ObjectiveWeighted
	cfg.Plugins.Objective.TimeoutMs = 0

	err := cfg.Validate()
	require.Error(t, err)

	cfgErrors := flattenConfigErrors(err)
	codes := make(map[string]struct{}, len(cfgErrors))
	for _, e := range cfgErrors {
		codes[e.Code] = struct{}{}
	}
	assert.Contains(t, codes, lberrors.CodeInvalidObjectiveTimeout)
}

func TestValidateObjectiveTimeoutTooLarge(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Plugins.Objective.Enabled = true
	cfg.Plugins.Objective.Name = config.ObjectiveWeighted
	cfg.Plugins.Objective.TimeoutMs = 201

	err := cfg.Validate()
	require.Error(t, err)

	cfgErrors := flattenConfigErrors(err)
	codes := make(map[string]struct{}, len(cfgErrors))
	for _, e := range cfgErrors {
		codes[e.Code] = struct{}{}
	}
	assert.Contains(t, codes, lberrors.CodeInvalidObjectiveTimeout)
}

func TestValidateObjectiveMaxConcurrentInvalidWhenSet(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Plugins.Objective.Enabled = true
	cfg.Plugins.Objective.Name = config.ObjectiveWeighted
	cfg.Plugins.Objective.TimeoutMs = 3
	cfg.Plugins.Objective.MaxConcurrent = -1

	err := cfg.Validate()
	require.Error(t, err)

	cfgErrors := flattenConfigErrors(err)
	assert.True(t, hasConfigError(cfgErrors, lberrors.CodeInvalidObjective, config.FieldPluginsObjectiveMaxConc))
}

// TestValidateObjectiveTimeoutIgnoredWhenDisabled 验证 objective 关闭时不触发 objective 超时校验。
func TestValidateObjectiveTimeoutIgnoredWhenDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Plugins.Objective.Enabled = false
	cfg.Plugins.Objective.TimeoutMs = 0

	require.NoError(t, cfg.Validate())
}

// TestValidateObjectiveNameRequiredWhenEnabled 验证启用 objective 时要求提供非空 objective 名称。
func TestValidateObjectiveNameRequiredWhenEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Plugins.Objective.Enabled = true
	cfg.Plugins.Objective.Name = ""
	cfg.Plugins.Objective.TimeoutMs = 3

	err := cfg.Validate()
	require.Error(t, err)

	cfgErrors := flattenConfigErrors(err)
	codes := make(map[string]struct{}, len(cfgErrors))
	for _, e := range cfgErrors {
		codes[e.Code] = struct{}{}
	}
	assert.Contains(t, codes, lberrors.CodeInvalidObjective)
}

func TestWithObjectiveMaxConcurrent(t *testing.T) {
	cfg := config.DefaultConfig()
	config.WithObjectiveMaxConcurrent(64)(&cfg)
	assert.Equal(t, 64, cfg.Plugins.Objective.MaxConcurrent)
}

// TestValidateEmptyCollections 验证 route_classes 和 fallback_chain 为空时返回对应错误码。
func TestValidateEmptyCollections(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.RouteClasses = nil
	cfg.FallbackChain = nil

	err := cfg.Validate()
	require.Error(t, err)

	cfgErrors := flattenConfigErrors(err)
	codes := make(map[string]struct{}, len(cfgErrors))
	for _, e := range cfgErrors {
		codes[e.Code] = struct{}{}
	}
	assert.Contains(t, codes, lberrors.CodeInvalidRouteClass)
	assert.Contains(t, codes, lberrors.CodeInvalidFallbackChain)
}

// TestValidateDuplicateCollections 验证集合重复项会返回对应错误码。
func TestValidateDuplicateCollections(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.RouteClasses = []types.RouteClass{types.RouteGeneric, types.RouteGeneric}
	cfg.FallbackChain = []string{config.FallbackPolicyRanked, config.FallbackPolicyRanked}
	cfg.Plugins.Policies = []string{config.PolicyHealthGate, config.PolicyHealthGate}

	err := cfg.Validate()
	require.Error(t, err)

	cfgErrors := flattenConfigErrors(err)
	codes := make(map[string]struct{}, len(cfgErrors))
	for _, e := range cfgErrors {
		codes[e.Code] = struct{}{}
	}
	assert.Contains(t, codes, lberrors.CodeInvalidRouteClass)
	assert.Contains(t, codes, lberrors.CodeInvalidFallbackChain)
	assert.Contains(t, codes, lberrors.CodeDuplicatePolicy)
	assert.True(t, hasConfigError(cfgErrors, lberrors.CodeInvalidRouteClass, "route_classes[1]"))
	assert.True(t, hasConfigError(cfgErrors, lberrors.CodeInvalidFallbackChain, "fallback_chain[1]"))
	assert.True(t, hasConfigError(cfgErrors, lberrors.CodeDuplicatePolicy, "plugins.policies[1]"))
}

// TestValidateRouteClassEnumFieldPath 验证 route class 非法值会返回带索引的字段路径。
func TestValidateRouteClassEnumFieldPath(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.RouteClasses = []types.RouteClass{"bad"}
	cfg.FallbackChain = []string{config.FallbackPolicyRanked}
	cfg.Plugins.Algorithms = map[types.RouteClass]string{
		types.RouteGeneric: config.AlgorithmLeastRequest,
	}

	err := cfg.Validate()
	require.Error(t, err)

	cfgErrors := flattenConfigErrors(err)
	assert.True(t, hasConfigError(cfgErrors, lberrors.CodeInvalidRouteClass, "route_classes[0]"))
}

// TestValidateFallbackEmptyItemFieldPath 验证 fallback 空元素会返回带索引的字段路径。
func TestValidateFallbackEmptyItemFieldPath(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.FallbackChain = []string{"", config.AlgorithmLeastRequest}

	err := cfg.Validate()
	require.Error(t, err)

	cfgErrors := flattenConfigErrors(err)
	assert.True(t, hasConfigError(cfgErrors, lberrors.CodeInvalidFallbackChain, "fallback_chain[0]"))
}

// TestValidatePolicyEmptyItemFieldPath 验证策略空元素会返回带索引的字段路径。
func TestValidatePolicyEmptyItemFieldPath(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Plugins.Policies = []string{"", config.PolicyHealthGate}

	err := cfg.Validate()
	require.Error(t, err)

	cfgErrors := flattenConfigErrors(err)
	assert.True(t, hasConfigError(cfgErrors, lberrors.CodeUnknownPolicy, "plugins.policies[0]"))
}

// flattenConfigErrors 递归展开 errors.Join 链中的 ConfigError。
func flattenConfigErrors(err error) []*lberrors.ConfigError {
	if err == nil {
		return nil
	}
	var out []*lberrors.ConfigError

	var cfgErr *lberrors.ConfigError
	if errors.As(err, &cfgErr) {
		out = append(out, cfgErr)
	}

	type multiUnwrapper interface {
		Unwrap() []error
	}
	if joined, ok := err.(multiUnwrapper); ok {
		for _, child := range joined.Unwrap() {
			out = append(out, flattenConfigErrors(child)...)
		}
	}
	return out
}

func hasConfigError(errs []*lberrors.ConfigError, code, field string) bool {
	for _, e := range errs {
		if e.Code == code && e.Field == field {
			return true
		}
	}
	return false
}

func sumWeights(weights map[string]int) int {
	total := 0
	for _, weight := range weights {
		total += weight
	}
	return total
}

var _ objective.Plugin = customObjective{}
