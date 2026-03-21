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

const pluginName = "ch"

const (
	fnvOffset64 = 14695981039346656037
	fnvPrime64  = 1099511628211

	baseReplicasPerWeight = 32
	maxReplicasPerNode    = 512

	reasonAlgorithmConsistentHash = "algorithm=ch"
	reasonRingLookup              = "selected_by_ch_ring"
)

type ringEntry struct {
	hash      uint64
	nodeIndex int
}

type nodeKey struct {
	nodeID    string
	weight    int
	nodeIndex int
}

type nodeState struct {
	nodeID string
	weight int
}

type ringCache struct {
	state       []nodeState
	ring        []ringEntry
	uniqueNodes int
}

type Plugin struct {
	mu    sync.RWMutex
	cache ringCache
}

func init() {
	registry.MustRegisterAlgorithm(&Plugin{})
}

func (*Plugin) Name() string {
	return pluginName
}

func (p *Plugin) SelectCandidates(req types.RequestContext, nodes []types.NodeSnapshot, topK int) ([]types.Candidate, error) {
	if topK <= 0 {
		return nil, fmt.Errorf("topK=%d: %w", topK, lberrors.ErrPluginMisconfigured)
	}
	if len(nodes) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	cache := p.loadOrBuildRing(nodes)
	if cache.uniqueNodes == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	limit := topK
	if limit > cache.uniqueNodes {
		limit = cache.uniqueNodes
	}

	ring := cache.ring
	if len(ring) == 0 {
		return nil, lberrors.ErrNoCandidate
	}

	keyHash, ok := hashSessionKey(req)
	if !ok {
		return nil, lberrors.ErrNoCandidate
	}

	start := sort.Search(len(ring), func(i int) bool { return ring[i].hash >= keyHash })
	if start == len(ring) {
		start = 0
	}

	seen := make([]uint64, (len(nodes)+63)>>6)
	out := make([]types.Candidate, 0, limit)
	reasonBuffer := make([]string, limit*2)
	for i := range len(ring) {
		if len(out) >= limit {
			break
		}
		idx := (start + i) % len(ring)
		nodeIndex := ring[idx].nodeIndex
		seenWord := nodeIndex >> 6
		seenBit := uint64(1) << (uint(nodeIndex) & 63)
		if (seen[seenWord] & seenBit) != 0 {
			continue
		}
		seen[seenWord] |= seenBit
		node := nodes[nodeIndex]
		reasonOffset := len(out) * 2
		reason := reasonBuffer[reasonOffset : reasonOffset+2 : reasonOffset+2]
		reason[0] = reasonAlgorithmConsistentHash
		reason[1] = reasonRingLookup
		out = append(out, types.Candidate{
			Node:   node,
			Score:  float64(ring[idx].hash),
			Reason: reason,
		})
	}
	if len(out) == 0 {
		return nil, lberrors.ErrNoCandidate
	}
	return out, nil
}

func (p *Plugin) loadOrBuildRing(nodes []types.NodeSnapshot) ringCache {
	p.mu.RLock()
	if nodesEqualState(nodes, p.cache.state) {
		cache := p.cache
		p.mu.RUnlock()
		return cache
	}
	p.mu.RUnlock()

	state, keys := snapshotStateAndCanonicalKeys(nodes)
	ring := buildRing(keys)
	newCache := ringCache{
		state:       state,
		ring:        ring,
		uniqueNodes: len(keys),
	}

	p.mu.Lock()
	defer p.mu.Unlock()
	if nodesEqualState(nodes, p.cache.state) {
		return p.cache
	}
	p.cache = newCache
	return p.cache
}

func buildRing(keys []nodeKey) []ringEntry {
	capacity := 0
	for i := range keys {
		capacity += replicasForWeight(keys[i].weight)
	}
	ring := make([]ringEntry, 0, capacity)

	for _, key := range keys {
		replicas := replicasForWeight(key.weight)
		for replica := range replicas {
			ring = append(ring, ringEntry{
				hash:      hashString64a(fnvOffset64, key.nodeID+"#"+strconv.Itoa(replica)),
				nodeIndex: key.nodeIndex,
			})
		}
	}

	sort.Slice(ring, func(i, j int) bool {
		if ring[i].hash != ring[j].hash {
			return ring[i].hash < ring[j].hash
		}
		return ring[i].nodeIndex < ring[j].nodeIndex
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

func snapshotStateAndCanonicalKeys(nodes []types.NodeSnapshot) ([]nodeState, []nodeKey) {
	state := make([]nodeState, 0, len(nodes))
	keys := make([]nodeKey, 0, len(nodes))
	firstIndex := make(map[string]int, len(nodes))
	for idx := range nodes {
		node := &nodes[idx]
		if node.NodeID == "" {
			continue
		}
		weight := effectiveWeight(node)
		state = append(state, nodeState{
			nodeID: node.NodeID,
			weight: weight,
		})
		keys = append(keys, nodeKey{
			nodeID:    node.NodeID,
			weight:    weight,
			nodeIndex: idx,
		})
		if _, exists := firstIndex[node.NodeID]; !exists {
			firstIndex[node.NodeID] = idx
		}
	}
	if len(keys) == 0 {
		return nil, nil
	}

	sort.Slice(keys, func(i, j int) bool {
		if keys[i].nodeID != keys[j].nodeID {
			return keys[i].nodeID < keys[j].nodeID
		}
		return keys[i].weight > keys[j].weight
	})

	uniq := keys[:0]
	uniq = append(uniq, nodeKey{
		nodeID:    keys[0].nodeID,
		weight:    keys[0].weight,
		nodeIndex: firstIndex[keys[0].nodeID],
	})
	for i := 1; i < len(keys); i++ {
		if keys[i].nodeID == keys[i-1].nodeID {
			continue
		}
		uniq = append(uniq, nodeKey{
			nodeID:    keys[i].nodeID,
			weight:    keys[i].weight,
			nodeIndex: firstIndex[keys[i].nodeID],
		})
	}
	return state, uniq
}

func nodesEqualState(nodes []types.NodeSnapshot, state []nodeState) bool {
	if len(nodes) == len(state) {
		for i := range nodes {
			if nodes[i].NodeID != state[i].nodeID {
				return false
			}
			if effectiveWeight(&nodes[i]) != state[i].weight {
				return false
			}
		}
		return true
	}

	stateIdx := 0
	for i := range nodes {
		if nodes[i].NodeID == "" {
			continue
		}
		if stateIdx >= len(state) {
			return false
		}
		if nodes[i].NodeID != state[stateIdx].nodeID {
			return false
		}
		if effectiveWeight(&nodes[i]) != state[stateIdx].weight {
			return false
		}
		stateIdx++
	}
	return stateIdx == len(state)
}

func effectiveWeight(node *types.NodeSnapshot) int {
	if node.StaticWeight <= 0 {
		return 1
	}
	return node.StaticWeight
}

func hashSessionKey(req types.RequestContext) (uint64, bool) {
	if req.SessionID == "" {
		return 0, false
	}
	return hashString64a(uint64(fnvOffset64), "sid:"+req.SessionID), true
}

func hashString64a(h uint64, s string) uint64 {
	for i := range len(s) {
		h ^= uint64(s[i])
		h *= fnvPrime64
	}
	return h
}

var _ algorithm.Plugin = (*Plugin)(nil)
