package lb

const (
	// DefaultVirtualNodes is the default number of virtual nodes per backend.
	//
	// DefaultVirtualNodes 每个后端的默认虚拟节点数
	DefaultVirtualNodes = 100

	// DefaultMaglevTableSize is the default size of the Maglev lookup table, a prime.
	//
	// DefaultMaglevTableSize Maglev 查找表默认大小（质数）
	DefaultMaglevTableSize = 65537

	// TreeThresholdLeastConn is the backend count at or above which the
	// least-connections selector switches from a linear scan to an index heap;
	// TreeThresholdARB is the same threshold for the active-request-bias selector.
	//
	// TreeThresholdLeastConn/TreeThresholdARB 树结构阈值
	// N >= 阈值时使用树结构 O(log n)，否则线性扫描 O(n)
	TreeThresholdLeastConn = 32
	TreeThresholdARB       = 32
)
