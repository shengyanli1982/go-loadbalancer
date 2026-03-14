package types

// RouteClass 标识请求分类。
type RouteClass string

const (
	// RouteGeneric 表示通用请求。
	RouteGeneric RouteClass = "generic"
	// RouteLLMPrefill 表示 LLM prefill 阶段请求。
	RouteLLMPrefill RouteClass = "llm-prefill"
	// RouteLLMDecode 表示 LLM decode 阶段请求。
	RouteLLMDecode RouteClass = "llm-decode"
)

// RequestContext 描述一次路由请求的上下文信息。
// 包含请求标识、租户信息、会话信息、路由类别、模型信息等。
type RequestContext struct {
	RequestID      string            // 请求唯一标识
	TenantID       string            // 租户 ID
	SessionID      string            // 会话 ID
	RouteClass     RouteClass        // 请求路由类别
	Model          string            // 目标模型名称
	PromptTokens   int               // 提示词 token 数
	ExpectedTokens int               // 期望生成 token 数
	Region         string            // 地域信息
	Metadata       map[string]string // 自定义元数据
}

// NodeSnapshot 描述节点在路由瞬间的状态。
// 包含节点的健康状态、资源使用情况、性能指标等信息。
type NodeSnapshot struct {
	NodeID            string             // 节点唯一标识
	Region            string             // 节点所在地域
	Healthy           bool               // 节点是否健康
	Inflight          int                // 当前处理中的请求数
	QueueDepth        int                // 请求队列深度
	CPUUtil           float64            // CPU 使用率（0-100）
	MemUtil           float64            // 内存使用率（0-100）
	AvgLatencyMs      float64            // 平均延迟（毫秒）
	P95LatencyMs      float64            // P95 延迟（毫秒）
	ErrorRate         float64            // 错误率（0-1）
	KVCacheHitRate    float64            // KV 缓存命中率（0-1）
	TTFTMs            float64            // Time To First Token（毫秒）
	TPOTMs            float64            // Time Per Output Token（毫秒）
	ModelAvailability map[string]bool    // 模型可用性映射
}

// Candidate 表示候选节点及其评分解释。
// 包含选中的节点、评分和选择原因。
type Candidate struct {
	Node   NodeSnapshot // 候选节点的快照
	Score  float64      // 节点评分（用于排序和展示）
	Reason []string     // 选择原因说明
}
