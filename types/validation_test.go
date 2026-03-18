package types

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRequestContextValidateSuccess 验证合法请求上下文通过校验。
func TestRequestContextValidateSuccess(t *testing.T) {
	req := RequestContext{
		RouteClass:     RouteGeneric,
		PromptTokens:   0,
		ExpectedTokens: 10,
	}
	require.NoError(t, req.Validate())
}

// TestRequestContextValidateInvalid 验证非法请求上下文返回字段级错误。
func TestRequestContextValidateInvalid(t *testing.T) {
	req := RequestContext{
		RouteClass:     "bad",
		PromptTokens:   -1,
		ExpectedTokens: -2,
	}

	err := req.Validate()
	require.Error(t, err)

	validationErrs := flattenValidationErrors(err)
	assert.True(t, hasValidationField(validationErrs, "route_class"))
	assert.True(t, hasValidationField(validationErrs, "prompt_tokens"))
	assert.True(t, hasValidationField(validationErrs, "expected_tokens"))
}

// TestNodeSnapshotValidateSuccess 验证合法节点快照通过校验。
func TestNodeSnapshotValidateSuccess(t *testing.T) {
	node := NodeSnapshot{
		NodeID:         "n1",
		StaticWeight:   0,
		Inflight:       1,
		QueueDepth:     2,
		CPUUtil:        10,
		MemUtil:        20,
		AvgLatencyMs:   30,
		P95LatencyMs:   40,
		ErrorRate:      0.1,
		KVCacheHitRate: 0.5,
		TTFTMs:         50,
		TPOTMs:         5,
	}
	require.NoError(t, node.Validate())
}

// TestNodeSnapshotValidateInvalid 验证非法节点快照返回字段级错误。
func TestNodeSnapshotValidateInvalid(t *testing.T) {
	node := NodeSnapshot{
		NodeID:         "",
		StaticWeight:   -1,
		Inflight:       -1,
		QueueDepth:     -1,
		CPUUtil:        120,
		MemUtil:        -1,
		AvgLatencyMs:   -1,
		P95LatencyMs:   -1,
		ErrorRate:      2,
		KVCacheHitRate: -1,
		TTFTMs:         -1,
		TPOTMs:         -1,
	}

	err := node.Validate()
	require.Error(t, err)

	validationErrs := flattenValidationErrors(err)
	assert.True(t, hasValidationField(validationErrs, "node_id"))
	assert.True(t, hasValidationField(validationErrs, "static_weight"))
	assert.True(t, hasValidationField(validationErrs, "inflight"))
	assert.True(t, hasValidationField(validationErrs, "queue_depth"))
	assert.True(t, hasValidationField(validationErrs, "cpu_util"))
	assert.True(t, hasValidationField(validationErrs, "mem_util"))
	assert.True(t, hasValidationField(validationErrs, "avg_latency_ms"))
	assert.True(t, hasValidationField(validationErrs, "p95_latency_ms"))
	assert.True(t, hasValidationField(validationErrs, "error_rate"))
	assert.True(t, hasValidationField(validationErrs, "kv_cache_hit_rate"))
	assert.True(t, hasValidationField(validationErrs, "ttft_ms"))
	assert.True(t, hasValidationField(validationErrs, "tpot_ms"))
}

func flattenValidationErrors(err error) []*ValidationError {
	if err == nil {
		return nil
	}
	var out []*ValidationError

	var vErr *ValidationError
	if errors.As(err, &vErr) {
		out = append(out, vErr)
	}

	type multiUnwrapper interface {
		Unwrap() []error
	}
	if joined, ok := err.(multiUnwrapper); ok {
		for _, child := range joined.Unwrap() {
			out = append(out, flattenValidationErrors(child)...)
		}
	}
	return out
}

func hasValidationField(errs []*ValidationError, field string) bool {
	for _, e := range errs {
		if e.Field == field {
			return true
		}
	}
	return false
}
