package lberrors

import (
	"errors"
	"fmt"
)

var (
	// ErrInvalidConfig 表示配置非法。
	ErrInvalidConfig = errors.New("invalid config")
	// ErrNoHealthyNodes 表示无健康节点可用。
	ErrNoHealthyNodes = errors.New("no healthy nodes")
	// ErrNoModelAvailable 表示所有节点都不可提供目标模型。
	ErrNoModelAvailable = errors.New("model not available on any node")
	// ErrNoCandidate 表示无可用候选节点。
	ErrNoCandidate = errors.New("no route candidate")
	// ErrPluginTimeout 表示插件执行超时。
	ErrPluginTimeout = errors.New("plugin timeout")
	// ErrPluginMisconfigured 表示插件配置错误。
	ErrPluginMisconfigured = errors.New("plugin misconfigured")
	// ErrDuplicatePlugin 表示插件重复注册。
	ErrDuplicatePlugin = errors.New("duplicate plugin")
	// ErrUnknownPlugin 表示插件不存在。
	ErrUnknownPlugin = errors.New("unknown plugin")
)

const (
	CodeInvalidTopK             = "CONFIG_INVALID_TOPK"
	CodeInvalidRouteClass       = "CONFIG_INVALID_ROUTE_CLASS"
	CodeMissingAlgorithmBinding = "CONFIG_MISSING_ALGORITHM_BINDING"
	CodeDuplicatePolicy         = "CONFIG_DUPLICATE_POLICY"
	CodeUnknownPolicy           = "CONFIG_UNKNOWN_POLICY"
	CodeInvalidObjective        = "CONFIG_INVALID_OBJECTIVE"
	CodeInvalidObjectiveTimeout = "CONFIG_INVALID_OBJECTIVE_TIMEOUT"
	CodeInvalidFallbackChain    = "CONFIG_INVALID_FALLBACK_CHAIN"
	CodeInvalidWeight           = "CONFIG_INVALID_WEIGHT"
	CodeInvalidWeightSum        = "CONFIG_INVALID_WEIGHT_SUM"
	CodeMissingLLMWeights       = "CONFIG_MISSING_LLM_WEIGHTS"
)

// ConfigError 表示配置字段级错误。
type ConfigError struct {
	Code       string
	Field      string
	Value      any
	Constraint string
}

func (e *ConfigError) Error() string {
	return fmt.Sprintf("%s field=%s value=%v constraint=%s", e.Code, e.Field, e.Value, e.Constraint)
}

func (e *ConfigError) Unwrap() error {
	return ErrInvalidConfig
}

// NewConfigError 构造配置错误。
func NewConfigError(code, field string, value any, constraint string) *ConfigError {
	return &ConfigError{
		Code:       code,
		Field:      field,
		Value:      value,
		Constraint: constraint,
	}
}
