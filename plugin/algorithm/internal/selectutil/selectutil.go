package selectutil

import (
	"container/heap"
	"sort"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

const smallKThreshold = 16

// LessNode 按既有优先级比较两个节点，返回 a 是否优于 b。
func LessNode(a, b types.NodeSnapshot) bool {
	return lessNodePtr(&a, &b)
}

func lessNodePtr(a, b *types.NodeSnapshot) bool {
	if a.Inflight != b.Inflight {
		return a.Inflight < b.Inflight
	}
	if a.QueueDepth != b.QueueDepth {
		return a.QueueDepth < b.QueueDepth
	}
	if a.P95LatencyMs != b.P95LatencyMs {
		return a.P95LatencyMs < b.P95LatencyMs
	}
	if a.ErrorRate != b.ErrorRate {
		return a.ErrorRate < b.ErrorRate
	}
	return a.NodeID < b.NodeID
}

// SelectTopK 返回按 LessNode 排序后的前 topK 个节点。
func SelectTopK(nodes []types.NodeSnapshot, topK int) []types.NodeSnapshot {
	idx := SelectTopKIndices(nodes, topK)
	if len(idx) == 0 {
		return nil
	}
	out := make([]types.NodeSnapshot, len(idx))
	for i := 0; i < len(idx); i++ {
		out[i] = nodes[idx[i]]
	}
	return out
}

// SelectTopKExcludeNodeID 返回排除指定节点后按 LessNode 排序的前 topK 个节点。
func SelectTopKExcludeNodeID(nodes []types.NodeSnapshot, excludedNodeID string, topK int) []types.NodeSnapshot {
	idx := SelectTopKExcludeNodeIDIndices(nodes, excludedNodeID, topK)
	if len(idx) == 0 {
		return nil
	}
	out := make([]types.NodeSnapshot, len(idx))
	for i := 0; i < len(idx); i++ {
		out[i] = nodes[idx[i]]
	}
	return out
}

// SelectTopKIndices 返回按 LessNode 排序后的前 topK 个节点索引。
func SelectTopKIndices(nodes []types.NodeSnapshot, topK int) []int {
	return selectTopKIndices(nodes, topK, "")
}

// SelectTopKExcludeNodeIDIndices 返回排除指定节点后按 LessNode 排序的前 topK 个节点索引。
func SelectTopKExcludeNodeIDIndices(nodes []types.NodeSnapshot, excludedNodeID string, topK int) []int {
	return selectTopKIndices(nodes, topK, excludedNodeID)
}

// selectTopKIndices 使用固定容量最大堆选出最优 topK 节点索引并排序输出。
func selectTopKIndices(nodes []types.NodeSnapshot, topK int, excludedNodeID string) []int {
	if topK <= 0 || len(nodes) == 0 {
		return nil
	}
	if topK > len(nodes) {
		topK = len(nodes)
	}
	if excludedNodeID == "" && topK >= len(nodes) {
		out := make([]int, len(nodes))
		for i := 0; i < len(nodes); i++ {
			out[i] = i
		}
		sort.Slice(out, func(i, j int) bool {
			return lessNodePtr(&nodes[out[i]], &nodes[out[j]])
		})
		return out
	}
	if topK <= smallKThreshold {
		return selectTopKSmallKIndices(nodes, topK, excludedNodeID)
	}

	h := &nodeIndexMaxHeap{
		nodes: nodes,
		items: make([]int, 0, topK),
	}
	for i := 0; i < len(nodes); i++ {
		if excludedNodeID != "" && nodes[i].NodeID == excludedNodeID {
			continue
		}
		if len(h.items) < topK {
			heap.Push(h, i)
			continue
		}
		if lessNodePtr(&nodes[i], &nodes[h.items[0]]) {
			h.items[0] = i
			heap.Fix(h, 0)
		}
	}

	out := append([]int(nil), h.items...)
	sort.Slice(out, func(i, j int) bool {
		return lessNodePtr(&nodes[out[i]], &nodes[out[j]])
	})
	return out
}

// selectTopKSmallKIndices 适用于 topK 很小的热路径，避免 container/heap 的额外开销。
func selectTopKSmallKIndices(nodes []types.NodeSnapshot, topK int, excludedNodeID string) []int {
	selectedIdx := make([]int, 0, topK)
	worstPos := -1
	for i := 0; i < len(nodes); i++ {
		if excludedNodeID != "" && nodes[i].NodeID == excludedNodeID {
			continue
		}
		if len(selectedIdx) < topK {
			selectedIdx = append(selectedIdx, i)
			if len(selectedIdx) == topK {
				worstPos = findWorstPos(nodes, selectedIdx)
			}
			continue
		}
		if !lessNodePtr(&nodes[i], &nodes[selectedIdx[worstPos]]) {
			continue
		}
		selectedIdx[worstPos] = i
		worstPos = findWorstPos(nodes, selectedIdx)
	}

	if len(selectedIdx) == 0 {
		return nil
	}
	for i := 1; i < len(selectedIdx); i++ {
		idx := selectedIdx[i]
		j := i - 1
		for ; j >= 0 && lessNodePtr(&nodes[idx], &nodes[selectedIdx[j]]); j-- {
			selectedIdx[j+1] = selectedIdx[j]
		}
		selectedIdx[j+1] = idx
	}
	return selectedIdx
}

func findWorstPos(nodes []types.NodeSnapshot, selectedIdx []int) int {
	worst := 0
	for i := 1; i < len(selectedIdx); i++ {
		if lessNodePtr(&nodes[selectedIdx[worst]], &nodes[selectedIdx[i]]) {
			worst = i
		}
	}
	return worst
}

// nodeIndexMaxHeap 是容量受限的最大堆实现，用于维护当前最差候选索引。
type nodeIndexMaxHeap struct {
	nodes []types.NodeSnapshot
	items []int
}

// Len 返回堆中元素数量。
func (h nodeIndexMaxHeap) Len() int { return len(h.items) }

// Less 反转比较规则，构建“最差节点在堆顶”的固定容量堆。
func (h nodeIndexMaxHeap) Less(i, j int) bool {
	return lessNodePtr(&h.nodes[h.items[j]], &h.nodes[h.items[i]])
}

// Swap 交换堆中两个元素位置。
func (h nodeIndexMaxHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
}

// Push 向堆尾追加元素，由 heap 包触发上滤。
func (h *nodeIndexMaxHeap) Push(x any) {
	h.items = append(h.items, x.(int))
}

// Pop 弹出堆尾元素，由 heap 包在调整后调用。
func (h *nodeIndexMaxHeap) Pop() any {
	last := len(h.items) - 1
	item := h.items[last]
	h.items = h.items[:last]
	return item
}
