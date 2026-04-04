package lb

// Backend 定义后端服务的基本接口
// 用于表示一个可以接收请求的后端服务器
type Backend interface {
	// Address 返回后端服务器的地址
	Address() string
}

// WeightedBackend 定义带有权重信息的后端接口
// 继承自 Backend，并添加权重配置
type WeightedBackend interface {
	Backend
	// Weight 返回后端服务器的权重值
	Weight() int
}
