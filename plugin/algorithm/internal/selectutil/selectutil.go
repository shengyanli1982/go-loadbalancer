package selectutil

import (
	"container/heap"
	"sort"

	"github.com/shengyanli1982/go-loadbalancer/types"
)

// LessNode 按既有优先级比较两个节点，返回 a 是否优于 b。
func LessNode(a, b types.NodeSnapshot) bool {
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
	return selectTopK(nodes, topK, "")
}

// SelectTopKExcludeNodeID 返回排除指定节点后按 LessNode 排序的前 topK 个节点。
func SelectTopKExcludeNodeID(nodes []types.NodeSnapshot, excludedNodeID string, topK int) []types.NodeSnapshot {
	return selectTopK(nodes, topK, excludedNodeID)
}

// selectTopK 使用固定容量最大堆选出最优的 topK 节点并排序输出。
func selectTopK(nodes []types.NodeSnapshot, topK int, excludedNodeID string) []types.NodeSnapshot {
	if topK <= 0 || len(nodes) == 0 {
		return nil
	}
	if topK > len(nodes) {
		topK = len(nodes)
	}
	if excludedNodeID == "" && topK >= len(nodes) {
		out := make([]types.NodeSnapshot, 0, len(nodes))
		for _, node := range nodes {
			out = append(out, node)
		}
		sort.Slice(out, func(i, j int) bool {
			return LessNode(out[i], out[j])
		})
		return out
	}

	h := &nodeMaxHeap{
		items: make([]types.NodeSnapshot, 0, topK),
	}
	for _, node := range nodes {
		if excludedNodeID != "" && node.NodeID == excludedNodeID {
			continue
		}
		if len(h.items) < topK {
			heap.Push(h, node)
			continue
		}
		if LessNode(node, h.items[0]) {
			h.items[0] = node
			heap.Fix(h, 0)
		}
	}

	out := append([]types.NodeSnapshot(nil), h.items...)
	sort.Slice(out, func(i, j int) bool {
		return LessNode(out[i], out[j])
	})
	return out
}

// nodeMaxHeap 是容量受限的最大堆实现，用于维护当前最差候选。
type nodeMaxHeap struct {
	items []types.NodeSnapshot
}

// Len 返回堆中元素数量。
func (h nodeMaxHeap) Len() int { return len(h.items) }

// Less 反转比较规则，构建“最差节点在堆顶”的固定容量堆。
func (h nodeMaxHeap) Less(i, j int) bool {
	return LessNode(h.items[j], h.items[i])
}

// Swap 交换堆中两个元素位置。
func (h nodeMaxHeap) Swap(i, j int) {
	h.items[i], h.items[j] = h.items[j], h.items[i]
}

// Push 向堆尾追加元素，由 heap 包触发上滤。
func (h *nodeMaxHeap) Push(x any) {
	h.items = append(h.items, x.(types.NodeSnapshot))
}

// Pop 弹出堆尾元素，由 heap 包在调整后调用。
func (h *nodeMaxHeap) Pop() any {
	last := len(h.items) - 1
	item := h.items[last]
	h.items = h.items[:last]
	return item
}
