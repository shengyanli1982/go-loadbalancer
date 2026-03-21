package types

import "time"

// RouteClass identifies request classes.
type RouteClass string

const (
	RouteGeneric    RouteClass = "generic"
	RouteLLMPrefill RouteClass = "llm-prefill"
	RouteLLMDecode  RouteClass = "llm-decode"
)

// RequestContext carries routing inputs.
type RequestContext struct {
	RequestID            string
	TenantID             string
	SessionID            string
	RouteClass           RouteClass
	PrimaryPool          string
	SecondaryPools       []string
	PromptTokens         int
	ExpectedTokens       int
	BudgetMaxTotalTokens int
	BudgetMaxInflight    int
	BudgetMaxQueueDepth  int
	Region               string
	Metadata             map[string]string
}

// NodeSnapshot carries per-node runtime metrics and externally supplied state metadata.
type NodeSnapshot struct {
	NodeID         string
	Region         string
	Pool           string
	ObservedAt     time.Time
	Version        string
	Source         string
	CooldownUntil  time.Time
	OutlierReason  string
	Healthy        bool
	Outlier        bool
	FreshnessTTLms int64
	StaticWeight   int
	Inflight       int
	QueueDepth     int
	CPUUtil        float64
	MemUtil        float64
	AvgLatencyMs   float64
	P95LatencyMs   float64
	ErrorRate      float64
	KVCacheHitRate float64
	TTFTms         float64
	TPOTms         float64
}

// Candidate is a ranked node candidate.
type Candidate struct {
	Node   NodeSnapshot
	Score  float64
	Reason []string
}
