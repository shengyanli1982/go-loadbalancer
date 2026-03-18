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
	NodeID            string              // 节点唯一标识
	Region            string              // 节点所在地域
	Healthy           bool                // 节点是否健康
	FreshnessTTLms    int64               // 状态新鲜度 TTL（毫秒），过期后可能被丢弃
	StaticWeight      int                 // 节点静态权重（用于 RR/WRR，<=0 按 1 处理）
	Inflight          int                 // 当前处理中的请求数
	QueueDepth        int                 // 请求队列深度
	CPUUtil           float64             // CPU 使用率（0-100）
	MemUtil           float64             // 内存使用率（0-100）
	AvgLatencyMs      float64             // 平均延迟（毫秒）
	P95LatencyMs      float64             // P95 延迟（毫秒）
	ErrorRate         float64             // 错误率（0-1）
	KVCacheHitRate    float64             // KV 缓存命中率（0-1）
	TTFTMs            float64             // Time To First Token（毫秒）
	TPOTMs            float64             // Time Per Output Token（毫秒）
	ModelAvailability map[string]bool     // 模型可用性映射
	ModelCapability   *ModelCapabilitySet // 可选预编译模型能力集（热路径优先使用）
}

// Candidate 表示候选节点及其评分解释。
// 包含选中的节点、评分和选择原因。
type Candidate struct {
	Node   NodeSnapshot // 候选节点的快照
	Score  float64      // 节点评分（用于排序和展示）
	Reason []string     // 选择原因说明
}

// ModelCapabilitySet 为模型可用性提供预编译表示。
// 语义与 NodeSnapshot.ModelAvailability 保持一致：
// 1) nil 或未受限：表示所有模型均可用。
// 2) 受限且无可用模型：表示所有模型均不可用。
// 3) 受限且有可用模型：仅白名单中的模型可用。
type ModelCapabilitySet struct {
	restricted bool
	single     string
	many       map[string]struct{}
}

// NewModelCapabilitySet 从模型可用性映射构建预编译能力集。
func NewModelCapabilitySet(availability map[string]bool) *ModelCapabilitySet {
	if len(availability) == 0 {
		return nil
	}

	set := &ModelCapabilitySet{restricted: true}
	trueCount := 0
	for model, ok := range availability {
		if !ok {
			continue
		}
		trueCount++
		if trueCount == 1 {
			set.single = model
		}
	}

	switch trueCount {
	case 0:
		return set
	case 1:
		return set
	default:
		set.many = make(map[string]struct{}, trueCount)
		for model, ok := range availability {
			if ok {
				set.many[model] = struct{}{}
			}
		}
		set.single = ""
		return set
	}
}

// Allows 判断目标模型是否可用。
func (s *ModelCapabilitySet) Allows(model string) bool {
	if model == "" || s == nil || !s.restricted {
		return true
	}
	if s.many != nil {
		_, ok := s.many[model]
		return ok
	}
	return s.single != "" && s.single == model
}
