package tenantquota

import (
	"fmt"
	"strconv"

	lberrors "go-loadbalancer/errors"
	"go-loadbalancer/plugin/policy"
	"go-loadbalancer/registry"
	"go-loadbalancer/types"
)

const (
	pluginName          = "tenant_quota"
	metadataMaxInflight = "tenant_quota_max_inflight"
	metadataMaxQueue    = "tenant_quota_max_queue"
)

// Plugin 实现租户配额策略。
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
	maxInflight, maxQueue, enabled, err := parseQuota(req.Metadata)
	if err != nil {
		return nil, err
	}
	if !enabled {
		out := append([]types.Candidate(nil), candidates...)
		for i := range out {
			out[i].Reason = append(out[i].Reason, "policy=tenant_quota_skipped")
		}
		return out, nil
	}

	out := make([]types.Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		if maxInflight > 0 && candidate.Node.Inflight > maxInflight {
			continue
		}
		if maxQueue > 0 && candidate.Node.QueueDepth > maxQueue {
			continue
		}
		candidate.Reason = append(candidate.Reason, "policy=tenant_quota")
		out = append(out, candidate)
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func parseQuota(metadata map[string]string) (maxInflight, maxQueue int, enabled bool, err error) {
	if len(metadata) == 0 {
		return 0, 0, false, nil
	}
	if value, ok := metadata[metadataMaxInflight]; ok {
		enabled = true
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return 0, 0, false, fmt.Errorf("metadata[%s]=%q: %w", metadataMaxInflight, value, lberrors.ErrPluginMisconfigured)
		}
		if parsed < 0 {
			return 0, 0, false, fmt.Errorf("metadata[%s]=%q: %w", metadataMaxInflight, value, lberrors.ErrPluginMisconfigured)
		}
		maxInflight = parsed
	}
	if value, ok := metadata[metadataMaxQueue]; ok {
		enabled = true
		parsed, parseErr := strconv.Atoi(value)
		if parseErr != nil {
			return 0, 0, false, fmt.Errorf("metadata[%s]=%q: %w", metadataMaxQueue, value, lberrors.ErrPluginMisconfigured)
		}
		if parsed < 0 {
			return 0, 0, false, fmt.Errorf("metadata[%s]=%q: %w", metadataMaxQueue, value, lberrors.ErrPluginMisconfigured)
		}
		maxQueue = parsed
	}
	return maxInflight, maxQueue, enabled, nil
}

var _ policy.Plugin = Plugin{}
