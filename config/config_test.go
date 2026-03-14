package config_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/shengyanli1982/go-loadbalancer/config"
	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	_ "github.com/shengyanli1982/go-loadbalancer/plugin/builtin"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// TestDefaultConfigValidate 验证默认配置可以通过校验。
func TestDefaultConfigValidate(t *testing.T) {
	cfg := config.DefaultConfig()
	require.NoError(t, cfg.Validate())
}

// TestValidateReturnsJoinedErrors 验证多项错误会以 join 形式返回。
func TestValidateReturnsJoinedErrors(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TopK = 0
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
