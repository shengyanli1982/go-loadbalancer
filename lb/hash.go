package lb

import (
	"hash/maphash"
)

// 全局哈希种子，用于所有哈希计算
var seed = maphash.MakeSeed()

// hash64 计算数据的安全哈希值
// 使用 maphash 包提供的安全哈希算法
func hash64(data []byte) uint64 {
	var h maphash.Hash
	h.SetSeed(seed)
	h.Write(data)
	return h.Sum64()
}
