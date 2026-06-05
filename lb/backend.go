package lb

// Backend 接口体系定义了负载均衡算法所需的后端能力抽象。
//
// 核心接口层级：
//   - Backend: 基础接口，所有算法必需
//   - WeightedBackend: 扩展权重能力，用于 WeightedRR、SmoothWeightedRR、LeastConn
//   - ConnBackend: 扩展连接数能力（预留，当前未使用）
//   - HealthBackend: 扩展健康检查能力（预留，当前未使用）

// Backend 定义后端服务器的最小接口
// 所有负载均衡算法都基于此接口进行选择
type Backend interface {
	// Address 返回后端服务器的唯一标识地址
	// 用于连接计数、指纹计算和日志标识
	Address() string
}

// WeightedBackend 定义带权重的后端接口
// 支持权重分配的算法（WeightedRR、SmoothWeightedRR、LeastConn）使用此接口
// 权重值应 > 0，<= 0 的权重会被视为 1
type WeightedBackend interface {
	Backend
	// Weight 返回后端的权重值
	// 权重越高，被选中的频率越高
	Weight() int
}

// ConnBackend 定义支持连接数统计的后端接口
// 预留接口，供未来扩展使用（当前 LeastConn 使用内部连接计数 map）
type ConnBackend interface {
	Backend
	// ActiveConnections 返回当前活跃连接数
	ActiveConnections() int
}

// HealthBackend 定义支持健康检查的后端接口
// 预留接口，供未来扩展使用（对标 nginx max_fails/fail_timeout 机制）
type HealthBackend interface {
	Backend
	// IsHealthy 返回后端是否健康
	IsHealthy() bool
}

// backend 实现 Backend 接口的私有后端类型
// 外部通过 NewBackend 工厂函数创建，用于 RoundRobin、Random 等不需要权重的算法
type backend struct {
	address string // 后端地址
}

// Address 返回后端地址
func (b *backend) Address() string { return b.address }

// weightedBackend 实现 WeightedBackend 接口的私有后端类型
// 外部通过 NewWeightedBackend 工厂函数创建，用于 WeightedRR、SmoothWeightedRR 等需要权重的算法
type weightedBackend struct {
	address string // 后端地址
	weight  int    // 权重值
}

// Address 返回后端地址（满足 Backend 接口）
func (b *weightedBackend) Address() string { return b.address }

// Weight 返回后端权重（满足 WeightedBackend 接口）
func (b *weightedBackend) Weight() int { return b.weight }

// NewBackend 创建不带权重的后端实例，实现 Backend 接口
// 适用于 RoundRobin、Random、LeastConn、P2C 等非权重算法
func NewBackend(address string) Backend {
	return &backend{address: address}
}

// NewWeightedBackend 创建带权重的后端实例，实现 WeightedBackend 接口
// 适用于 WeightedRR、SmoothWeightedRR 等需要权重分配的算法
// 权重值应 > 0，<= 0 的权重在算法中会被视为 1
func NewWeightedBackend(address string, weight int) WeightedBackend {
	return &weightedBackend{address: address, weight: weight}
}
