// Package lb 提供负载均衡算法的实现
//
// 后端（Backend）接口定义
package lb

// Backend 定义后端服务器接口
// 所有负载均衡算法都基于 Backend 接口进行选择
type Backend interface {
	// Address 返回后端服务器的地址
	Address() string
}

// WeightedBackend 定义带权重后端接口
// 支持权重分配的算法（如 WeightedRR、SmoothWeightedRR）使用此接口
type WeightedBackend interface {
	Backend
	// Weight 返回后端的权重值
	Weight() int
}

// ConnBackend 定义支持连接数统计的后端接口
// LeastConn 算法使用此接口获取当前连接数
type ConnBackend interface {
	Backend
	// ActiveConnections 返回当前活跃连接数
	ActiveConnections() int
}

// HealthBackend 定义支持健康检查的后端接口
// 可用于过滤不健康的后端
type HealthBackend interface {
	Backend
	// IsHealthy 返回后端是否健康
	IsHealthy() bool
}
