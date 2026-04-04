package lb

// 版本信息
const (
	Version = "v2.1.0" // 当前包版本
)

// 负载均衡器常量定义
const (
	MaxBackends     = 65536   // 最大后端服务器数量
	MinRingSize     = 128     // 环形哈希最小大小
	MaxRingSize     = 1 << 20 // 环形哈希最大大小 (1048576)
	DefaultRingSize = 1 << 16 // 环形哈希默认大小 (65536)
	MaglevTableSize = 65537   // Maglev 算法默认表大小
	MinWeight       = 1       // 最小权重值
	MaxWeight       = 65535   // 最大权重值
)

// P2C 算法相关常量
const (
	P2CTrials = 2   // P2C 算法尝试次数
	P2CBias   = 0.0 // P2C 算法偏向因子
)
