package rr

import (
	"fmt"
	"sync/atomic"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// pluginName 是 rr 插件注册名。
const pluginName = "rr"

const (
	reasonAlgorithmRR = "algorithm=rr"
	reasonRotation    = "selected_by_round_robin_rotation"
)

// Plugin 实现 round robin 算法。
type Plugin struct {
	next uint64
}

func init() {
	registry.MustRegisterAlgorithm(&Plugin{})
}

// Name 返回插件注册名。
func (*Plugin) Name() string {
	return pluginName
}

// SelectCandidates 以轮转顺序返回候选节点。
func (p *Plugin) SelectCandidates(_ types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK=%d: %w", topK, lberrors.ErrPluginMisconfigured)
	}
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	limit := topK
	if limit > len(nodes) {
		limit = len(nodes)
	}
	start := int(atomic.AddUint64(&p.next, 1)-1) % len(nodes)

	reasonBuffer := make([]string, limit*2)
	out := make([]types.Candidate, 0, limit)
	for i := range limit {
		idx := (start + i) % len(nodes)
		reasonOffset := i * 2
		reason := reasonBuffer[reasonOffset : reasonOffset+2 : reasonOffset+2]
		reason[0] = reasonAlgorithmRR
		reason[1] = reasonRotation
		out = append(out, types.Candidate{
			Node:   nodes[idx],
			Score:  float64(i),
			Reason: reason,
		})
	}
	return out, nil
}

var _ algorithm.Plugin = (*Plugin)(nil)
