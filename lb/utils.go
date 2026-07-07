package lb

import (
	"encoding/binary"
	"math/rand/v2"
)

// resizeSlice 复用 slice 容量，如容量不足则新分配（泛型版本）。
// 用于 weighted_rr / smooth_weighted_rr / edf / rendezvous 的 rebuild 函数。
func resizeSlice[T any](s []T, n int) []T {
	if cap(s) >= n {
		return s[:n]
	}
	return make([]T, n)
}

// allWeightsEqual 检测权重切片是否全部相等。
// 用于 connectionTracker.rebuildIndex 和 rendezvous.rebuildCache。
func allWeightsEqual(w []int) bool {
	if len(w) <= 1 {
		return true
	}
	first := w[0]
	for _, v := range w[1:] {
		if v != first {
			return false
		}
	}
	return true
}

// randomKey8 generates an 8-byte random key using math/rand/v2
// Used by Rendezvous, RingHash, and Maglev when Select() needs a random key
func randomKey8() [8]byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], rand.Uint64())
	return buf
}
