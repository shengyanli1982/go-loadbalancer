package lb

// Backend 接口体系定义了负载均衡算法所需的后端能力抽象。
//
// 核心接口层级：
//   - Backend: 基础接口，所有算法必需
//   - WeightedBackend: 扩展权重能力，用于 WeightedRR、SmoothWeightedRR、LeastConn
//   - LatencyBackend: 扩展延迟感知能力，用于 LeastTime

// Backend is the minimal interface that every load-balancing algorithm requires.
//
// Backend 定义后端服务器的最小接口
// 所有负载均衡算法都基于此接口进行选择
type Backend interface {
	// Address returns the unique identifier address of the backend.
	//
	// Address 返回后端服务器的唯一标识地址
	// 用于连接计数、指纹计算和日志标识
	Address() string
}

// WeightedBackend is a Backend that also carries a routing weight.
//
// WeightedBackend 定义带权重的后端接口
// 支持权重分配的算法（WeightedRR、SmoothWeightedRR、LeastConn）使用此接口
// 权重值应 > 0，<= 0 的权重会被视为 1
type WeightedBackend interface {
	Backend
	// Weight returns the routing weight of the backend.
	//
	// Weight 返回后端的权重值
	// 权重越高，被选中的频率越高
	Weight() int
}

// LatencyBackend is a Backend that supplies the latency and in-flight observations used by LeastTime.
//
// LatencyBackend 定义支持延迟感知的后端接口
// 外部实现此接口注入延迟数据，供 LeastTime 算法使用
//
// 使用方式：外部测量响应延迟后注入此接口，LeastTime 据此做延迟感知选择。
// 未实现此接口的后端被视为「延迟未知」，取惩罚哨兵 +Inf：恒败于任何有观测数据的后端，
// 仅在集合中不存在任何可观测后端时才被选中（此时无数据后端之间平局，退化为 RR 轮转）。
//
// 活性约束：AverageLatency 与 ActiveConnections 会在 LeastTime 选择器的内部互斥锁
// 临界区内被逐后端调用（本包唯一在热路径锁内执行用户实现代码的算法）。
// 实现必须是非阻塞的纯读取（如 atomic.Load 或读取已维护好的字段）：
// 任何阻塞都会让同一选择器上所有并发 Select 排队停摆；
// 实现也不得回调同一选择器（sync.Mutex 不可重入，重入立即死锁）。
type LatencyBackend interface {
	Backend
	// ActiveConnections returns the current number of active connections.
	//
	// ActiveConnections 返回当前活跃连接数
	ActiveConnections() int
	// AverageLatency returns the average response latency.
	//
	// AverageLatency 返回平均响应延迟（毫秒或微秒均可，算法只关注相对大小）
	AverageLatency() float64
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

// NewBackend creates an unweighted Backend for the given address.
//
// NewBackend 创建不带权重的后端实例，实现 Backend 接口
// 适用于 RoundRobin、Random、LeastConn、P2C 等非权重算法
func NewBackend(address string) Backend {
	return &backend{address: address}
}

// NewWeightedBackend creates a WeightedBackend for the given address and weight.
//
// NewWeightedBackend 创建带权重的后端实例，实现 WeightedBackend 接口
// 适用于 WeightedRR、SmoothWeightedRR 等需要权重分配的算法
// 权重值应 > 0，<= 0 的权重在算法中会被视为 1
func NewWeightedBackend(address string, weight int) WeightedBackend {
	return &weightedBackend{address: address, weight: weight}
}
