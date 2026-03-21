package llmsessionaffinity

import (
	"sort"
	"strings"

	"github.com/shengyanli1982/go-loadbalancer/plugin/policy"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

const pluginName = "llm_session_affinity"

const (
	MetadataAffinityNodeKey = "llm_session_affinity_node"
	MetadataDegradeNodesKey = "llm_session_affinity_degrade_nodes"
)

const (
	reasonSkippedNonDecode = "policy=llm_session_affinity_skipped_non_decode"
	reasonApplied          = "policy=llm_session_affinity"
	reasonHit              = "policy=llm_session_affinity_hit"
	reasonMiss             = "policy=llm_session_affinity_miss"
	reasonDegrade          = "policy=llm_session_affinity_degrade"
)

// Plugin enforces decode-session affinity and configurable degrade ordering.
type Plugin struct{}

func init() {
	registry.MustRegisterPolicy(Plugin{})
}

func (Plugin) Name() string {
	return pluginName
}

func (Plugin) ReRank(req types.RequestContext, candidates []types.Candidate) ([]types.Candidate, error) {
	if len(candidates) == 0 {
		return nil, nil
	}

	if req.RouteClass != types.RouteLLMDecode {
		for i := range candidates {
			candidates[i].Reason = append(candidates[i].Reason, reasonSkippedNonDecode)
		}
		return candidates, nil
	}

	target := strings.TrimSpace(req.Metadata[MetadataAffinityNodeKey])
	degradeOrder := parseOrderedNodes(req.Metadata[MetadataDegradeNodesKey])
	if target == "" && len(degradeOrder) == 0 {
		for i := range candidates {
			candidates[i].Reason = append(candidates[i].Reason, reasonApplied)
		}
		return candidates, nil
	}

	var degradeRank map[string]int
	if len(degradeOrder) > 0 {
		degradeRank = make(map[string]int, len(degradeOrder))
		for i, nodeID := range degradeOrder {
			degradeRank[nodeID] = i + 1
		}
	}

	foundTarget := false
	sort.SliceStable(candidates, func(i, j int) bool {
		return affinityRank(candidates[i].Node.NodeID, target, degradeRank) < affinityRank(candidates[j].Node.NodeID, target, degradeRank)
	})

	for i := range candidates {
		nodeID := candidates[i].Node.NodeID
		candidates[i].Reason = append(candidates[i].Reason, reasonApplied)
		switch {
		case target != "" && nodeID == target:
			foundTarget = true
			candidates[i].Reason = append(candidates[i].Reason, reasonHit)
		case degradeRank[nodeID] > 0:
			candidates[i].Reason = append(candidates[i].Reason, reasonDegrade)
		}
	}
	if target != "" && !foundTarget {
		for i := range candidates {
			candidates[i].Reason = append(candidates[i].Reason, reasonMiss)
		}
	}
	return candidates, nil
}

func affinityRank(nodeID, target string, degradeRank map[string]int) int {
	if target != "" && nodeID == target {
		return 0
	}
	if rank, ok := degradeRank[nodeID]; ok {
		return rank
	}
	return 1_000_000
}

func parseOrderedNodes(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == ';' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
	})
	if len(parts) == 0 {
		return nil
	}
	ordered := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		nodeID := strings.TrimSpace(part)
		if nodeID == "" {
			continue
		}
		if _, ok := seen[nodeID]; ok {
			continue
		}
		seen[nodeID] = struct{}{}
		ordered = append(ordered, nodeID)
	}
	if len(ordered) == 0 {
		return nil
	}
	return ordered
}

var _ policy.Plugin = Plugin{}
