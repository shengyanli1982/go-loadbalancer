package types

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
	Model                string
	PromptTokens         int
	ExpectedTokens       int
	BudgetMaxTotalTokens int
	BudgetMaxInflight    int
	BudgetMaxQueueDepth  int
	Region               string
	Metadata             map[string]string
}

// NodeSnapshot carries per-node runtime metrics.
type NodeSnapshot struct {
	NodeID            string
	Region            string
	Pool              string
	Healthy           bool
	Outlier           bool
	FreshnessTTLms    int64
	StaticWeight      int
	Inflight          int
	QueueDepth        int
	CPUUtil           float64
	MemUtil           float64
	AvgLatencyMs      float64
	P95LatencyMs      float64
	ErrorRate         float64
	KVCacheHitRate    float64
	TTFTms            float64
	TPOTms            float64
	ModelAvailability map[string]bool
	ModelCapability   *ModelCapabilitySet
}

// Candidate is a ranked node candidate.
type Candidate struct {
	Node   NodeSnapshot
	Score  float64
	Reason []string
}

// ModelCapabilitySet is a precompiled model allow-list.
type ModelCapabilitySet struct {
	restricted bool
	single     string
	many       map[string]struct{}
}

// NewModelCapabilitySet builds a model capability set from availability map.
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

// Allows reports whether model is allowed by the set.
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
