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

	keys := canonicalNodeKeys(nodes)
	if len(keys) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	limit := topK
	if limit > len(keys) {
		limit = len(keys)
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

	selectedIDs := make([]string, 0, limit)
	out := make([]types.Candidate, 0, limit)
	for i := 0; i < len(ring) && len(out) < limit; i++ {
		idx := (start + i) % len(ring)
		nodeID := ring[idx].nodeID
		if containsNodeID(selectedIDs, nodeID) {
			continue
		}
		node, ok := findNodeByID(nodes, nodeID)
		if !ok {
			continue
		}
		selectedIDs = append(selectedIDs, nodeID)
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

func canonicalNodeKeys(nodes []types.NodeSnapshot) []nodeKey {
	keys := make([]nodeKey, 0, len(nodes))
	for _, node := range nodes {
		if node.NodeID == "" {
			continue
		}
		keys = append(keys, nodeKey{
			nodeID: node.NodeID,
			weight: effectiveWeight(node),
		})
	}
	if len(keys) == 0 {
		return nil
	}

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].nodeID != keys[j].nodeID {
			return keys[i].nodeID < keys[j].nodeID
		}
		return keys[i].weight > keys[j].weight
	})

	// 去重：同 nodeID 仅保留第一个（更高权重优先）。
	uniq := keys[:1]
	for i := 1; i < len(keys); i++ {
		if keys[i].nodeID == keys[i-1].nodeID {
			continue
		}
		uniq = append(uniq, keys[i])
	}
	return uniq
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

func containsNodeID(selectedIDs []string, nodeID string) bool {
	for i := 0; i < len(selectedIDs); i++ {
		if selectedIDs[i] == nodeID {
			return true
		}
	}
	return false
}

func findNodeByID(nodes []types.NodeSnapshot, nodeID string) (types.NodeSnapshot, bool) {
	for i := 0; i < len(nodes); i++ {
		if nodes[i].NodeID == nodeID {
			return nodes[i], true
		}
	}
	return types.NodeSnapshot{}, false
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
