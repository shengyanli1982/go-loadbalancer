package lb

// 错误码定义
const (
	ErrCodeNilBackends     = 1001 // 后端列表为空或nil的错误码
	ErrCodeInvalidWeight   = 1002 // 权重值无效的错误码
	ErrCodeInvalidRingSize = 1003 // 环形哈希大小无效的错误码
)

// Error 定义负载均衡器的错误类型
// 包含错误码、错误信息和原始错误
type Error struct {
	Code    int    // 错误码
	Message string // 错误描述信息
	Cause   error  // 原始错误，用于错误链追溯
}

// Error 实现 error 接口的 Error 方法
func (e *Error) Error() string {
	return e.Message
}

// Unwrap 实现 error 接口的 Unwrap 方法，用于错误链追溯
func (e *Error) Unwrap() error {
	return e.Cause
}

// Is 实现 error 接口的 Is 方法，用于错误比较
func (e *Error) Is(target error) bool {
	if t, ok := target.(*Error); ok {
		return e.Code == t.Code
	}
	return false
}

// 预定义的错误变量
var ErrNilBackends = &Error{
	Code:    ErrCodeNilBackends,
	Message: "backend list is nil or empty", // 后端列表为空或nil
}

// ErrInvalidWeight 创建权重无效错误的工厂函数
func ErrInvalidWeight(cause error) *Error {
	return &Error{
		Code:    ErrCodeInvalidWeight,
		Message: "weight must be greater than 0", // 权重必须大于0
		Cause:   cause,
	}
}

// ErrInvalidRingSize 创建环形哈希大小无效错误的工厂函数
func ErrInvalidRingSize(cause error) *Error {
	return &Error{
		Code:    ErrCodeInvalidRingSize,
		Message: "ring size must be between 128 and 1048576", // 环形哈希大小必须在128到1048576之间
		Cause:   cause,
	}
}
