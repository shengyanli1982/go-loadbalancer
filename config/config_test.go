package config_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"go-loadbalancer/config"
	lberrors "go-loadbalancer/errors"
	_ "go-loadbalancer/plugin/builtin"
	"go-loadbalancer/types"
)

func TestDefaultConfigValidate(t *testing.T) {
	cfg := config.DefaultConfig()
	require.NoError(t, cfg.Validate())
}

func TestValidateReturnsJoinedErrors(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.TopK = 0
	cfg.RouteClasses = []types.RouteClass{"bad", types.RouteLLMPrefill}
	cfg.Plugins.Policies = []string{"health_gate", "health_gate", "missing_policy"}
	cfg.Weights.ByRouteClass[types.RouteLLMPrefill] = map[string]int{
		"queue":      6000,
		"error_rate": 3000,
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
