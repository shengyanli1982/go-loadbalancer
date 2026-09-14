package lb

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHashSelectors_EmptyKeyReturnsFirstBackend 钉住五个哈希算法共享的调用方契约：
// len(key) == 0（nil 与 []byte{} 两种形态）时 SelectByHash 确定性返回 backends[0]。
//
// 契约声明处：README「Caller contract」与 doc.go「使用约束」；此前仅 Rendezvous
// 有一个只断言 NotNil 的弱测试，其余四个算法的契约停留在纯文档承诺。
// 本测试将其升级为可执行断言，防止任一实现把空 key 分支改写为其他行为
// （如取末位、走哈希路径或 panic）而不被察觉。
//
// backends[0] 刻意取最小权重：若实现误入加权评分路径（例如挑权重最大者），
// 断言会失败，而非恰好与 backends[0] 重合。
func TestHashSelectors_EmptyKeyReturnsFirstBackend(t *testing.T) {
	backends := []Backend{
		NewWeightedBackend("h1", 1),
		NewWeightedBackend("h2", 5),
		NewWeightedBackend("h3", 3),
	}

	selectors := []struct {
		name string
		sel  HashSelector
	}{
		{"IPHash", NewIPHash()},
		{"URIHash", NewURIHash(nil)},
		{"RingHash", NewRingHash(nil)},
		{"Maglev", NewMaglev(nil)},
		{"Rendezvous", NewRendezvous()},
	}

	for _, tc := range selectors {
		t.Run(tc.name, func(t *testing.T) {
			for _, key := range [][]byte{nil, {}} {
				got := tc.sel.SelectByHash(backends, key)
				require.NotNil(t, got, "%s: 空 key %v 不应返回 nil", tc.name, key)
				assert.Equal(t, backends[0].Address(), got.Address(),
					"%s: 空 key %v 应确定性返回 backends[0]", tc.name, key)
			}
		})
	}
}
