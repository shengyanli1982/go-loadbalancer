package lb

const (
	// MaxBackends 支持的最大后端数量
	MaxBackends = 65536

	// MinRingSize / MaxRingSize 一致性哈希环大小范围
	MinRingSize = 128
	MaxRingSize = 1 << 20

	// DefaultRingSize 默认哈希环大小
	DefaultRingSize = 1 << 16

	// MinWeight / MaxWeight 权重范围
	MinWeight = 1
	MaxWeight = 65535

	// DefaultVirtualNodes 每个后端的默认虚拟节点数
	DefaultVirtualNodes = 100
)
