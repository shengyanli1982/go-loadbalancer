package consistenthash

import (
	"fmt"
	"sort"
	"strconv"
	"sync"

	lberrors "github.com/shengyanli1982/go-loadbalancer/errors"
	"github.com/shengyanli1982/go-loadbalancer/plugin/algorithm"
	"github.com/shengyanli1982/go-loadbalancer/registry"
	"github.com/shengyanli1982/go-loadbalancer/types"
)

// pluginName 是 consistent_hash 插件注册名。
const pluginName = "consistent_hash"

const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211

	baseReplicasPerWeight = 32
	maxReplicasPerNode    = 512

	reasonAlgorithmConsistentHash = "algorithm=consistent_hash"
	reasonRingLookup              = "selected_by_consistent_hash_ring"
)

type ringEntry struct {
	hash   uint64
	nodeID string
}

type nodeKey struct {
	nodeID string
	weight int
}

type ringCache struct {
	keys []nodeKey
	ring []ringEntry
}

type Plugin struct {
	mu    sync.RWMutex
	cache ringCache
}

func init() {
	registry.MustRegisterAlgorithm(&Plugin{})
}

// Name 返回插件注册名。
func (*Plugin) Name() string {
	return pluginName
}

// SelectCandidates 基于一致性哈希环选择候选节点。
func (p *Plugin) SelectCandidates(req types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK=%d: %w", topK, lberrors.ErrPluginMisconfigured)
	}
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	keys, nodeByID := canonicalNodeKeys(nodes)
	if len(nodeByID) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	limit := topK
	if limit > len(nodeByID) {
		limit = len(nodeByID)
	}

	ring := p.loadOrBuildRing(keys)
	if len(ring) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	keyHash := hashRequestKey(req)
	start := sort.Search(len(ring), func(i int) bool { return ring[i].hash >= keyHash })
	if start == len(ring) {
		start = 0
	}

	selected := make(map[string]struct{}, limit)
	out := make([]types.Candidate, 0, limit)
	for i := 0; i < len(ring) && len(out) < limit; i++ {
		idx := (start + i) % len(ring)
		nodeID := ring[idx].nodeID
		if _, exists := selected[nodeID]; exists {
			continue
		}
		node, ok := nodeByID[nodeID]
		if !ok {
			continue
		}
		selected[nodeID] = struct{}{}
		out = append(out, types.Candidate{
			Node:   node,
			Score:  float64(ring[idx].hash),
			Reason: []string{reasonAlgorithmConsistentHash, reasonRingLookup},
		})
	}
	if len(out) == 0 {
		return nil, lberrors.ErrNoCandidate
	}
	return out, nil
}

func (p *Plugin) loadOrBuildRing(keys []nodeKey) []ringEntry {
	p.mu.RLock()
	if keysEqual(keys, p.cache.keys) {
		ring := p.cache.ring
		p.mu.RUnlock()
		return ring
	}
	p.mu.RUnlock()

	ring := buildRing(keys)

	p.mu.Lock()
	defer p.mu.Unlock()
	if keysEqual(keys, p.cache.keys) {
		return p.cache.ring
	}
	p.cache = ringCache{keys: keys, ring: ring}
	return p.cache.ring
}

func buildRing(keys []nodeKey) []ringEntry {
	capacity := 0
	for i := 0; i < len(keys); i++ {
		capacity += replicasForWeight(keys[i].weight)
	}
	ring := make([]ringEntry, 0, capacity)

	for _, key := range keys {
		replicas := replicasForWeight(key.weight)
		for replica := 0; replica < replicas; replica++ {
			ring = append(ring, ringEntry{
				hash:   hashString64a(fnvOffset64, key.nodeID+"#"+strconv.Itoa(replica)),
				nodeID: key.nodeID,
			})
		}
	}

	sort.Slice(ring, func(i, j int) bool {
		if ring[i].hash != ring[j].hash {
			return ring[i].hash < ring[j].hash
		}
		return ring[i].nodeID < ring[j].nodeID
	})
	return ring
}

func replicasForWeight(weight int) int {
	w := weight
	if w <= 0 {
		w = 1
	}
	replicas := w * baseReplicasPerWeight
	if replicas > maxReplicasPerNode {
		return maxReplicasPerNode
	}
	return replicas
}

func canonicalNodeKeys(nodes []types.NodeSnapshot) ([]nodeKey, map[string]types.NodeSnapshot) {
	nodeByID := make(map[string]types.NodeSnapshot, len(nodes))
	weightByID := make(map[string]int, len(nodes))
	for _, node := range nodes {
		if node.NodeID == "" {
			continue
		}
		nodeByID[node.NodeID] = node
		weightByID[node.NodeID] = effectiveWeight(node)
	}

	keys := make([]nodeKey, 0, len(weightByID))
	for nodeID, weight := range weightByID {
		keys = append(keys, nodeKey{
			nodeID: nodeID,
			weight: weight,
		})
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].nodeID != keys[j].nodeID {
			return keys[i].nodeID < keys[j].nodeID
		}
		return keys[i].weight < keys[j].weight
	})
	return keys, nodeByID
}

func keysEqual(a, b []nodeKey) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func effectiveWeight(node types.NodeSnapshot) int {
	if node.StaticWeight <= 0 {
		return 1
	}
	return node.StaticWeight
}

func hashRequestKey(req types.RequestContext) uint64 {
	h := uint64(fnvOffset64)
	if req.SessionID != "" {
		return hashString64a(h, "sid:"+req.SessionID)
	}
	if req.RequestID != "" {
		return hashString64a(h, "rid:"+req.RequestID)
	}
	h = hashString64a(h, "tenant:"+req.TenantID)
	h = hashString64a(h, "model:"+req.Model)
	h = hashString64a(h, "route:"+string(req.RouteClass))
	return h
}

func hashString64a(h uint64, s string) uint64 {
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	return h
}

var _ algorithm.Plugin = (*Plugin)(nil)
