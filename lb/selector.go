package lb

// Selector picks one backend from a candidate list.
//
// Selector 定义负载均衡选择器的接口
// 用于从多个后端服务器中选择一个来处理请求
type Selector interface {
	// Select chooses one backend from backends, or returns nil when the list is empty.
	//
	// Select 从后端列表中选择一个后端服务器
	Select(backends []Backend) Backend
}

// HashSelector picks a backend by hash key so that equal keys always route to the same backend.
//
// HashSelector 定义基于哈希算法的负载均衡选择器接口
// 通过哈希键值来选择后端服务器，保证相同键值始终路由到同一后端
type HashSelector interface {
	// SelectByHash returns the backend that key maps to, or nil when the list is empty.
	//
	// SelectByHash 根据哈希键从后端列表中选择一个后端服务器
	SelectByHash(backends []Backend, key []byte) Backend
}

// ConsistentHashSelector combines Selector and HashSelector for consistent-hashing algorithms.
//
// ConsistentHashSelector 定义一致性哈希选择器的组合接口
// 同时具备 Select（内部生成随机 key）与 SelectByHash（按调用方指定 key 路由）双能力，
// 是一致性哈希类算法构造函数的统一返回类型
// 满足者：Maglev、RingHash、Rendezvous
type ConsistentHashSelector interface {
	Selector
	HashSelector
}

// RequestReleaser releases an in-flight request that Select previously assigned to a backend.
//
// RequestReleaser 定义释放在途请求占用的接口
// Release 的语义是告知选择器：此前由 Select 分配给该后端的某个在途请求已经完成，
// 选择器据此递减该后端的内部计数。调用方漏掉 Release 会让计数持续漂移，
// 被抬高计数的后端将遭系统性饿死，导致选择失真
//
// 满足者与其所计的量各不相同，但「释放一个在途请求」的语义一致：
//   - LeastConn: 计该后端的在途连接数
//   - P2C: 计该后端的衰减负载（EWMA inflight）
//   - ARB（ActiveRequestBias）: 计该后端的活跃请求数
//
// 正因三者计的量不同，接口名不采用 Conn 一类只对部分算法成立的措辞
type RequestReleaser interface {
	// Release decrements the in-flight counter of backend; it is a no-op when backend
	// is nil or is not being tracked.
	//
	// Release 释放指定后端的一个在途请求占用（递减其内部计数）
	// backend 为 nil 或不在跟踪列表内时为空操作
	Release(backend Backend)
}

// TrackedSelector combines Selector and RequestReleaser for selectors that count in-flight requests.
//
// TrackedSelector 定义跟踪在途请求数的选择器组合接口
// 同时具备 Select（选择并递增计数）与 Release（递减计数）能力，二者必须配对使用：
// 每一次 Select 都应对应一次 Release，否则内部计数只增不减
// 是跟踪在途请求类算法构造函数的统一返回类型，调用方无需类型断言即可调用 Release
//
// 返回本接口的构造函数：NewLeastConn、NewP2C、NewP2CWithOptions、
// NewActiveRequestBias、NewActiveRequestBiasWithOptions
//
// 正确配对用法：
//
//	sel := lb.NewLeastConn()
//	backend := sel.Select(backends)
//	defer sel.Release(backend) // 请求结束时释放，与上面的 Select 一一对应
//	// ... 使用 backend 处理本次请求 ...
type TrackedSelector interface {
	Selector
	RequestReleaser
}
