package lb

import (
	"encoding/binary"
	"math/rand/v2"
)

// resizeSlice 复用 slice 容量，如容量不足则新分配（泛型版本）。
// 用于 weighted_rr / smooth_weighted_rr / edf / least_time / connection_tracker
// 的 rebuild 函数。
//
// 契约（调用方必须知晓，否则会静默数据损坏）：
//   - cap(s) >= n 时返回的是 **s 同一底层数组的别名**（s[:n]），**不是拷贝**。
//     对返回切片的元素写入会同步写穿任何在此之前保存的、共享该底层数组的旧切片
//     （包括 old := s 这样的 header 赋值，以及 s[a:b] 子视图）。
//   - cap(s) < n 时才 make 新数组，此时旧切片内容不受影响。
//   - 因此"先保存旧内容、再 resize、再覆写、最后回读旧内容"的模式是**错误**的：
//     回读到的已是新数据。需要在覆写后仍读取旧值时，必须在 resize 之前完成读取，
//     或显式 slices.Clone 一份（会给冷路径增加分配）。
//   - **禁止用于经 atomic.Pointer 发布为不可变快照（RCU）的状态**：
//     maglev / ringHash / rendezvous / p2c 的快照会被在途读者长期持有，
//     复用底层数组等于就地改写读者正在读的数据（对 maglev 即静默误路由，
//     且 `-race` 抓不到——写者持锁、读者读的是自己 Load 到的快照指针，
//     同步原语层面干净，错的只是语义）。这类 rebuild 必须每次 make 全新分配，
//     由 rcu_snapshot_test.go 的 TestRCU_*_SnapshotImmutableAcrossRebuild 系列钉住。
//     map 状态同理：clear() 复用桶存储也是就地改写，一并禁止。
//
// 历史缺陷：connectionTracker.rebuildIndex 曾以此模式保存 oldAddrCache，
// 导致连接计数按位置而非按地址迁移；由
// TestConnectionTrackerRebuild_MigratesCountsByAddressNotPosition 回归钉住。
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
