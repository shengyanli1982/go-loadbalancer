package lb

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConcurrent_RoundRobin(t *testing.T) {
	selector := NewRoundRobin()
	backends := newTestBackends("a", "b", "c", "d", "e")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				assert.NotNil(t, b)
			}
		}()
	}
	wg.Wait()
}

// TestConcurrent_RoundRobin_Distribution 验证并发下轮询分布的精确均匀性：
// 修复 Add→Load 复合操作非原子问题后，每次 Select 获得唯一递增序号，
// 总调用数可被后端数整除时每个后端命中数必须精确相等。
func TestConcurrent_RoundRobin_Distribution(t *testing.T) {
	selector := NewRoundRobin()
	backends := newTestBackends("a", "b", "c", "d", "e")
	const goroutines = 20
	const callsPerGoroutine = 500

	addrIndex := make(map[string]int, len(backends))
	for i, b := range backends {
		addrIndex[b.Address()] = i
	}

	var counts [5]atomic.Int64
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				assert.NotNil(t, b)
				counts[addrIndex[b.Address()]].Add(1)
			}
		}()
	}
	wg.Wait()

	expectedPerBackend := goroutines * callsPerGoroutine / len(backends)
	for i, b := range backends {
		require.Equal(t, int64(expectedPerBackend), counts[i].Load(),
			"backend %s should receive exactly %d requests", b.Address(), expectedPerBackend)
	}
}

func TestConcurrent_WeightedRR(t *testing.T) {
	selector := NewWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 3),
		NewWeightedBackend("c", 2),
	}
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				assert.NotNil(t, b)
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_SmoothWeightedRR(t *testing.T) {
	selector := NewSmoothWeightedRR()
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 3),
		NewWeightedBackend("c", 2),
	}
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				assert.NotNil(t, b)
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_Random(t *testing.T) {
	selector := NewRandom()
	backends := newTestBackends("a", "b", "c", "d", "e")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				assert.NotNil(t, b)
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_LeastConn(t *testing.T) {
	selector := NewLeastConn()
	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				assert.NotNil(t, b)
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_LeastConn_Release(t *testing.T) {
	selector := NewLeastConn()
	releaser, ok := selector.(RequestReleaser)
	require.True(t, ok)

	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				assert.NotNil(t, b)
				releaser.Release(b)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				releaser.Release(backends[j%len(backends)])
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_P2C(t *testing.T) {
	selector := NewP2C()
	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				assert.NotNil(t, b)
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_P2C_Release(t *testing.T) {
	selector := NewP2C()
	releaser, ok := selector.(RequestReleaser)
	require.True(t, ok)

	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				b := selector.Select(backends)
				assert.NotNil(t, b)
				releaser.Release(b)
			}
		}()
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				releaser.Release(backends[j%len(backends)])
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_RingHash_SelectByHash(t *testing.T) {
	selector := NewRingHash(&RingHashOptions{RingSize: 1024, VirtualNodes: 50})
	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	keys := [][]byte{
		[]byte("key1"), []byte("key2"), []byte("key3"), []byte("key4"), []byte("key5"),
		[]byte("key6"), []byte("key7"), []byte("key8"), []byte("key9"), []byte("key10"),
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				key := keys[j%len(keys)]
				b := selector.SelectByHash(backends, key)
				assert.NotNil(t, b)
			}
		}()
	}
	wg.Wait()
}

func TestConcurrent_Maglev_SelectByHash(t *testing.T) {
	selector := NewMaglev(&MaglevOptions{TableSize: 1024})
	backends := newTestBackends("a", "b", "c")
	const goroutines = 20
	const callsPerGoroutine = 50

	keys := [][]byte{
		[]byte("key1"), []byte("key2"), []byte("key3"), []byte("key4"), []byte("key5"),
		[]byte("key6"), []byte("key7"), []byte("key8"), []byte("key9"), []byte("key10"),
	}

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				key := keys[j%len(keys)]
				b := selector.SelectByHash(backends, key)
				assert.NotNil(t, b)
			}
		}()
	}
	wg.Wait()
}

// ============================================================================
// P2-19 补齐：EDF/LeastTime/ARB/Rendezvous/IPHash/URIHash 六个缺失算法的并发测试
// （并发覆盖从 8/14 → 14/14；其中 EDF/LeastTime/ARB 有锁保护的可变状态，
// 是 -race 真正该覆盖的对象——concurrent_bench_test.go 的并发 benchmark 不进 -race 门），
// 以及 LeastConn/P2C 的 Select/Release 守恒断言。
//
// 断言范式以 TestConcurrent_RoundRobin_Distribution 为准（既有 11 个并发测试中
// 9 个只在 goroutine 内 assert.NotNil，仅验证「不 panic 且非 nil」）。
// 以下新增测试统一断言三条基本不变量（见 runConcurrentSelectConservation）：
//  1. 返回值非 nil；
//  2. 返回值属于输入后端集合；
//  3. 每后端计数之和 == 总迭代次数（选择无丢失/重复计数）。
//
// goroutine 内一律用 assert（goroutine-safe）；require（FailNow 仅限测试
// goroutine）只在 wg.Wait() 之后使用——与既有测试的处理方式一致。
// ============================================================================

const (
	concurrentTestGoroutines      = 20
	concurrentTestCallsPerRoutine = 50
	concurrentTestTotalCalls      = concurrentTestGoroutines * concurrentTestCallsPerRoutine
)

// runConcurrentSelectConservation 并发调用 selectFn 共 concurrentTestTotalCalls 次，
// 断言上述三条基本不变量，返回每后端（按 backends 顺序）的选中计数，
// 供调用方叠加算法语义级的更强断言（oracle 对拍 / 恒胜者等）。
func runConcurrentSelectConservation(t *testing.T, backends []Backend, selectFn func([]Backend) Backend) []int64 {
	t.Helper()

	addrIndex := make(map[string]int, len(backends))
	for i, b := range backends {
		addrIndex[b.Address()] = i
	}

	counts := make([]atomic.Int64, len(backends))
	var wg sync.WaitGroup
	wg.Add(concurrentTestGoroutines)
	for g := 0; g < concurrentTestGoroutines; g++ {
		go func() {
			defer wg.Done()
			for j := 0; j < concurrentTestCallsPerRoutine; j++ {
				got := selectFn(backends)
				if !assert.NotNil(t, got, "并发 Select 不得返回 nil") {
					continue
				}
				idx, ok := addrIndex[got.Address()]
				if !assert.True(t, ok, "返回了入参集合之外的后端 %s", got.Address()) {
					continue
				}
				counts[idx].Add(1)
			}
		}()
	}
	wg.Wait()

	result := make([]int64, len(backends))
	var total int64
	for i := range counts {
		result[i] = counts[i].Load()
		total += result[i]
	}
	require.Equal(t, int64(concurrentTestTotalCalls), total,
		"总选择次数必须等于总迭代次数（无丢失/重复计数）")
	return result
}

// sequentialSelectCounts 在一个独立 selector 实例上顺序调用 selectFn total 次并按地址计数。
// 对「全部可变状态在锁内、状态转移确定」的 selector（EDF/ARB：无随机源、无时间依赖），
// 并发下的每次 Select 等价于按某种串行次序推进同一台确定性状态机，
// 因此 N 次并发选择的 multiset 必等于 N 次顺序选择的 multiset——可作精确参照。
func sequentialSelectCounts(backends []Backend, selectFn func([]Backend) Backend, total int) map[string]int64 {
	counts := make(map[string]int64, len(backends))
	for i := 0; i < total; i++ {
		counts[selectFn(backends).Address()]++
	}
	return counts
}

// runConcurrentHashDeterminism 并发以固定 key 集调用 SelectByHash，在三条基本不变量之上
// 叠加哈希选择器的语义级断言：同一 key 在所有 goroutine 中结果一致、且与单线程 oracle
// 完全相同（哈希选择器是 (backends, key) 的纯函数，结果不一致即意味着读到撕裂状态）。
// oracle 阶段在 goroutine 启动前于测试 goroutine 内完成（wg.Add/go 建立 happens-before）。
func runConcurrentHashDeterminism(t *testing.T, selector HashSelector, backends []Backend, keys [][]byte) {
	t.Helper()

	// 单线程 oracle：固定拓扑下预先算出每个 key 的唯一正确结果
	want := make([]string, len(keys))
	for i, key := range keys {
		got := selector.SelectByHash(backends, key)
		require.NotNil(t, got)
		want[i] = got.Address()
	}

	addrSet := make(map[string]struct{}, len(backends))
	for _, b := range backends {
		addrSet[b.Address()] = struct{}{}
	}

	var total atomic.Int64
	var wg sync.WaitGroup
	wg.Add(concurrentTestGoroutines)
	for g := 0; g < concurrentTestGoroutines; g++ {
		go func() {
			defer wg.Done()
			for j := 0; j < concurrentTestCallsPerRoutine; j++ {
				ki := j % len(keys)
				got := selector.SelectByHash(backends, keys[ki])
				if !assert.NotNil(t, got, "并发 SelectByHash 不得返回 nil") {
					continue
				}
				_, ok := addrSet[got.Address()]
				if !assert.True(t, ok, "返回了入参集合之外的后端 %s", got.Address()) {
					continue
				}
				assert.Equal(t, want[ki], got.Address(),
					"同一 key 在并发下出现不一致路由（撕裂状态或缓存损坏）")
				total.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int64(concurrentTestTotalCalls), total.Load(),
		"总选择次数必须等于总迭代次数（无丢失/重复计数）")
}

// TestConcurrent_EDF 并发覆盖 EDF（有锁保护的 deadline 堆状态）。
// 确定性状态机 → 并发选择 multiset 与顺序 oracle 精确一致（抓「堆损坏导致
// 分布漂移/某后端饿死」，NotNil 断言对此完全无鉴别力）。
func TestConcurrent_EDF(t *testing.T) {
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 3),
		NewWeightedBackend("c", 2),
	}

	oracle := NewEDF()
	want := sequentialSelectCounts(backends, oracle.Select, concurrentTestTotalCalls)

	counts := runConcurrentSelectConservation(t, backends, NewEDF().Select)
	for i, b := range backends {
		assert.Equal(t, want[b.Address()], counts[i],
			"backend %s 并发选择计数应与顺序 oracle 一致（权重 %d 的确定性分配）",
			b.Address(), b.(WeightedBackend).Weight())
	}
}

// TestConcurrent_LeastTime 并发覆盖 LeastTime（有锁保护的 rrIndex/tiedIndices 状态）。
// 三个后端 score 互异（10*1 < 20*1 < 30*1，等权比较 latency*(1+conns)）→
// 无平局时全局最优必须每次胜出（精确断言，抓并发下的评分竞争/状态撕裂）。
func TestConcurrent_LeastTime(t *testing.T) {
	backends := newLatencyBackends([]ltConfig{
		{"svc-a:80", 1, 10.0, 0}, // score=10 ← 全局最优
		{"svc-b:80", 1, 20.0, 0}, // score=20
		{"svc-c:80", 1, 30.0, 0}, // score=30
	})

	counts := runConcurrentSelectConservation(t, backends, NewLeastTime().Select)
	assert.Equal(t, int64(concurrentTestTotalCalls), counts[0],
		"无平局时最低 score 后端 svc-a 应获得全部流量")
	assert.Zero(t, counts[1], "svc-b 不应获得流量")
	assert.Zero(t, counts[2], "svc-c 不应获得流量")
}

// TestConcurrent_ARB 并发覆盖 ActiveRequestBias（有锁保护的 connByIndex + 索引堆状态）。
// Select-only（不配对 Release）：N 次 Select 是同一确定性状态机的 N 次推进，
// 并发 multiset 必等于顺序 oracle multiset。
func TestConcurrent_ARB(t *testing.T) {
	backends := []Backend{
		NewWeightedBackend("a", 1),
		NewWeightedBackend("b", 3),
		NewWeightedBackend("c", 2),
	}

	oracle := NewActiveRequestBias()
	want := sequentialSelectCounts(backends, oracle.Select, concurrentTestTotalCalls)

	counts := runConcurrentSelectConservation(t, backends, NewActiveRequestBias().Select)
	for i, b := range backends {
		assert.Equal(t, want[b.Address()], counts[i],
			"backend %s 并发选择计数应与顺序 oracle 一致", b.Address())
	}
}

// TestConcurrent_Rendezvous 并发覆盖 Rendezvous（RCU 快照 + 锁保护的重建路径）。
func TestConcurrent_Rendezvous(t *testing.T) {
	selector := NewRendezvous()
	backends := newTestBackends("a", "b", "c")
	keys := make([][]byte, 10)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("session-key-%d", i))
	}
	runConcurrentHashDeterminism(t, selector, backends, keys)
}

// TestConcurrent_IPHash 并发覆盖 IPHash（无状态纯哈希，key 用 IP 字面量贴近真实用法）。
func TestConcurrent_IPHash(t *testing.T) {
	selector := NewIPHash()
	backends := newTestBackends("a", "b", "c")
	keys := make([][]byte, 10)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("10.0.%d.%d", i/5, i+1))
	}
	runConcurrentHashDeterminism(t, selector, backends, keys)
}

// TestConcurrent_URIHash 并发覆盖 URIHash（无状态纯哈希，key 用 URI 字面量贴近真实用法）。
func TestConcurrent_URIHash(t *testing.T) {
	selector := NewURIHash(nil)
	backends := newTestBackends("a", "b", "c")
	keys := make([][]byte, 10)
	for i := range keys {
		keys[i] = []byte(fmt.Sprintf("/api/v1/resource/%d?page=%d", i, i*10))
	}
	runConcurrentHashDeterminism(t, selector, backends, keys)
}

// TestConcurrent_LeastConn_ReleaseConservation 并发 Select/Release 严格配对后的守恒断言：
// 守恒式「内部计数总和 == Select 成功次数 − Release 次数」，配对时二者相等 → 计数必须全部归零。
// 能抓到「Release 漏减 / 重复减 / 减错后端」三类缺陷（connByIndex 元素非负，
// 漏减使总和 > 0，减错后端使被减者停在 0 而配对者残留 > 0），NotNil 断言完全无法覆盖。
// LeastConn 的有效计数在 connByIndex（白盒读取仅在 wg.Wait() 之后，无并发）。
func TestConcurrent_LeastConn_ReleaseConservation(t *testing.T) {
	selector := NewLeastConn()
	lc := selector.(*leastConn)
	backends := newTestBackends("a", "b", "c")

	var selects, releases atomic.Int64
	var wg sync.WaitGroup
	wg.Add(concurrentTestGoroutines)
	for g := 0; g < concurrentTestGoroutines; g++ {
		go func() {
			defer wg.Done()
			for j := 0; j < concurrentTestCallsPerRoutine; j++ {
				b := selector.Select(backends)
				if !assert.NotNil(t, b, "并发 Select 不得返回 nil") {
					continue
				}
				selects.Add(1)
				selector.Release(b) // 与上一行 Select 严格配对：释放同一后端
				releases.Add(1)
			}
		}()
	}
	wg.Wait()

	require.Equal(t, int64(concurrentTestTotalCalls), selects.Load(),
		"全部 Select 都应成功返回")
	require.Equal(t, selects.Load(), releases.Load(), "Select/Release 必须严格配对")
	for i, c := range lc.connByIndex {
		assert.Zero(t, c,
			"connByIndex[%d] 应归零：守恒式 Select−Release==计数总和 被破坏（漏减/重复减/减错后端）", i)
	}
}

// TestConcurrent_P2C_ReleaseConservation P2C 版本的配对守恒断言，计数在
// data.Load().loadCounts[i]（atomic.Int64，白盒读取在 wg.Wait() 之后）。
// 注意：P2C 负载存在秒级指数衰减（applyDecay，构造后 1s 才可能首次触发），
// 本测试 1000 次配对调用实测毫秒级完成，处于首个衰减窗口内，守恒不被衰减破坏；
// 若未来该测试运行超过 1s 导致衰减介入，应先怀疑运行环境而非实现（衰减是算法语义）。
func TestConcurrent_P2C_ReleaseConservation(t *testing.T) {
	selector := NewP2C()
	p := selector.(*p2c)
	backends := newTestBackends("a", "b", "c")

	var selects, releases atomic.Int64
	var wg sync.WaitGroup
	wg.Add(concurrentTestGoroutines)
	for g := 0; g < concurrentTestGoroutines; g++ {
		go func() {
			defer wg.Done()
			for j := 0; j < concurrentTestCallsPerRoutine; j++ {
				b := selector.Select(backends)
				if !assert.NotNil(t, b, "并发 Select 不得返回 nil") {
					continue
				}
				selects.Add(1)
				selector.Release(b) // 与上一行 Select 严格配对：释放同一后端
				releases.Add(1)
			}
		}()
	}
	wg.Wait()

	require.Equal(t, int64(concurrentTestTotalCalls), selects.Load(),
		"全部 Select 都应成功返回")
	require.Equal(t, selects.Load(), releases.Load(), "Select/Release 必须严格配对")

	loads := p.data.Load().loadCounts
	require.Len(t, loads, len(backends))
	var sum int64
	for i := range loads {
		sum += loads[i].Load()
	}
	assert.Equal(t, selects.Load()-releases.Load(), sum,
		"守恒式：内部计数总和必须等于 Select 次数 − Release 次数")
	for i := range loads {
		assert.Zero(t, loads[i].Load(),
			"loads[%d] 应归零（漏减/重复减/减错后端都会破坏该不变量）", i)
	}
}
