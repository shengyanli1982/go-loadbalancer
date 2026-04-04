package lb

// Selector 定义负载均衡选择器的接口
// 用于从多个后端服务器中选择一个来处理请求
type Selector interface {
	// Select 从后端列表中选择一个后端服务器
	Select(backends []Backend) Backend
}

// HashSelector 定义基于哈希算法的负载均衡选择器接口
// 通过哈希键值来选择后端服务器，保证相同键值始终路由到同一后端
type HashSelector interface {
	// SelectByHash 根据哈希键从后端列表中选择一个后端服务器
	SelectByHash(backends []Backend, key []byte) Backend
}

// SelectOrNil 安全地从后端列表中选择一个后端
// 如果后端列表为空，返回 nil
func SelectOrNil(s Selector, backends []Backend) Backend {
	if len(backends) == 0 {
		return nil
	}
	return s.Select(backends)
}
