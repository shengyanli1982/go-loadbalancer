package lb

import (
	"encoding/binary"

	"github.com/cespare/xxhash/v2"
)

// hash64 使用 xxhash 对字节切片进行哈希计算
// xxhash 是一种高性能的哈希算法，适合负载均衡场景
func hash64(data []byte) uint64 {
	return xxhash.Sum64(data)
}

// hash64String 使用 xxhash 对字符串进行哈希计算
func hash64String(data string) uint64 {
	return xxhash.Sum64String(data)
}

// computeBackendsFingerprint 计算后端列表的指纹
// 用于快速检测后端列表是否发生变化，避免每次都重新构建内部数据结构
func computeBackendsFingerprint(backends []Backend) uint64 {
	h := xxhash.New()
	for _, b := range backends {
		addr := b.Address()
		var lenBuf [8]byte
		binary.BigEndian.PutUint64(lenBuf[:], uint64(len(addr)))
		h.Write(lenBuf[:])
		h.WriteString(addr)
	}
	return h.Sum64()
}
