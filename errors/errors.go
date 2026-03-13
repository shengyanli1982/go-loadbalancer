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
	// CodeInvalidTopK 表示 top_k 不在允许范围。
	CodeInvalidTopK             = "CONFIG_INVALID_TOPK"
	// CodeInvalidRouteClass 表示路由类别非法或重复。
	CodeInvalidRouteClass       = "CONFIG_INVALID_ROUTE_CLASS"
	// CodeMissingAlgorithmBinding 表示路由类别缺少算法绑定或绑定算法未注册。
	CodeMissingAlgorithmBinding = "CONFIG_MISSING_ALGORITHM_BINDING"
	// CodeDuplicatePolicy 表示策略链中存在重复项。
	CodeDuplicatePolicy         = "CONFIG_DUPLICATE_POLICY"
	// CodeUnknownPolicy 表示策略链包含未注册策略。
	CodeUnknownPolicy           = "CONFIG_UNKNOWN_POLICY"
	// CodeInvalidObjective 表示目标函数配置非法。
	CodeInvalidObjective        = "CONFIG_INVALID_OBJECTIVE"
	// CodeInvalidObjectiveTimeout 表示目标函数超时配置非法。
	CodeInvalidObjectiveTimeout = "CONFIG_INVALID_OBJECTIVE_TIMEOUT"
	// CodeInvalidFallbackChain 表示回退链配置非法。
	CodeInvalidFallbackChain    = "CONFIG_INVALID_FALLBACK_CHAIN"
	// CodeInvalidWeight 表示权重值非法。
	CodeInvalidWeight           = "CONFIG_INVALID_WEIGHT"
	// CodeInvalidWeightSum 表示权重和不为 10000。
	CodeInvalidWeightSum        = "CONFIG_INVALID_WEIGHT_SUM"
	// CodeMissingLLMWeights 表示 LLM 路由缺少必须权重项。
	CodeMissingLLMWeights       = "CONFIG_MISSING_LLM_WEIGHTS"
)

// ConfigError 表示配置字段级错误。
type ConfigError struct {
	Code       string
	Field      string
	Value      any
	Constraint string
}

// Error 返回配置错误的可读字符串。
func (e *ConfigError) Error() string {
	return fmt.Sprintf("%s field=%s value=%v constraint=%s", e.Code, e.Field, e.Value, e.Constraint)
}

// Unwrap 将字段级配置错误归并到 ErrInvalidConfig。
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
