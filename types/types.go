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
type RequestContext struct {
	RequestID      string
	TenantID       string
	SessionID      string
	RouteClass     RouteClass
	Model          string
	PromptTokens   int
	ExpectedTokens int
	Region         string
	Metadata       map[string]string
}

// NodeSnapshot 描述节点在路由瞬间的状态。
type NodeSnapshot struct {
	NodeID            string
	Region            string
	Healthy           bool
	Inflight          int
	QueueDepth        int
	CPUUtil           float64
	MemUtil           float64
	AvgLatencyMs      float64
	P95LatencyMs      float64
	ErrorRate         float64
	KVCacheHitRate    float64
	TTFTMs            float64
	TPOTMs            float64
	ModelAvailability map[string]bool
}

// Candidate 表示候选节点及其评分解释。
type Candidate struct {
	Node   NodeSnapshot
	Score  float64
	Reason []string
}
