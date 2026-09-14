// Package lb provides production-grade load balancing algorithms.
//
// Package lb 提供生产级负载均衡算法实现。
//
// 支持以下 14 种算法：
//   - RoundRobin: 轮询，最简单高效
//   - WeightedRR: 加权轮询，按权重比例分配（前缀和 + 二分查找 O(log n)）
//   - SmoothWeightedRR: 平滑加权轮询（nginx 风格），权重分布更均匀，避免突发
//   - EDF: 最早截止时间优先加权轮询（Traefik 风格），min-heap 实现严格比例调度。
//     已知确定性偏差：deadline 为 float64 逐项累加（+= 1/weight），理论平局可能
//     因累加误差而不严格相等（如 3×⅓ 精确等于 1.0，而 7×⅐ 累加为 0.999…8，
//     两者理论上同归 1），此时按 deadline 大小裁决而非 index 序；
//     与 Envoy 的 float 实现同源，对分布影响可忽略
//   - Random: 随机选择（NewRandomWithSeed 可复现）
//   - LeastConn: 最少连接（对标 nginx least_conn）。等权时取 conn 最小者；
//     加权时按 conn/weight 交叉乘法比较，取商最小者
//   - P2C: Power of Two Choices，随机取两个后端比较衰减负载（EWMA inflight），
//     适合大规模分布式系统；读路径无锁
//   - LeastTime: 延迟感知路由（对标 Traefik v3.6/Envoy），
//     score = latency*(1+conns)/weight，取 score 最小者。
//     latency 与 conns 均由调用方通过 LatencyBackend 注入，算法自身不维护连接计数。
//     weight 则来自 WeightedBackend：加权评分要求后端同时实现两个接口，
//     仅实现 LatencyBackend 的后端权重恒为 1，除以 1 为恒等运算，
//     混合权重下该后端仍走加权评分路径
//   - ActiveRequestBias: 柔性 LeastConn（对标 Envoy WLR），
//     score = weight/(conns+1)^bias，取 score 最大者。bias ∈ (0, 1]：
//     bias=1 等价于标准 LeastConn；0<bias<1 为权重与连接数的平滑过渡。
//     bias=0 不是有效取值（省略、<=0 或 >1 一律回落为 1.0）：按公式此时 score 恒等于 weight，
//     会永远选中权重最大的后端，而非按权重比例分配。需要按权重比例分配请使用 WeightedRR
//   - IPHash: IP 哈希，基于客户端 IP 会话保持
//   - URIHash: URI 哈希，基于请求 URI 一致性路由
//   - Rendezvous: 会合点哈希（HRW），天然支持权重，增删节点仅影响约 1/N 的 key；读路径无锁
//   - RingHash: 一致性哈希环，后端变化时最小化 key 迁移；读路径无锁
//   - Maglev: Google Maglev 一致性哈希，O(1) 查表；读路径无锁
//
// # LeastTime 的混合后端语义
//
// 后端集合中可以混有未实现 LatencyBackend 的后端。这类后端没有可观测的延迟数据，
// 其 score 取惩罚哨兵 math.Inf(1)（而非 0）：
//
//   - 只要集合中存在任意一个可观测后端，无观测数据者恒败，不会被选中；
//   - 仅当集合中不存在任何可观测后端时才会选中它们，此时所有 score 同为 +Inf，
//     判定为平局，退化为按 rrIndex 轮转（RR）。
//
// 哨兵取 +Inf 而非某个有限大值（如 MaxFloat64）的原因：加权路径会执行 score/weight，
// 有限哨兵被不同权重缩放后彼此不再相等，平局被破坏，退化为「权重最高者恒胜」；
// 而 +Inf 除以任意有限正权重仍为 +Inf，等权与加权两条路径上平局判定都成立。
//
// # 导出接口
//
// 共 8 个。选择器侧 5 个（selector.go）：
//
//	Selector               Select 能力，除 IPHash、URIHash（纯 HashSelector，仅 SelectByHash）外的 12 种算法满足
//	HashSelector           SelectByHash 能力：IPHash、URIHash、Maglev、RingHash、Rendezvous
//	ConsistentHashSelector Selector + HashSelector，一致性哈希类构造函数的返回类型
//	RequestReleaser        Release 能力：LeastConn、P2C、ActiveRequestBias
//	TrackedSelector        Selector + RequestReleaser，在途请求跟踪类构造函数的返回类型
//
// 后端侧 3 个（backend.go）：
//
//	Backend         Address()，所有算法的最小后端接口
//	WeightedBackend Backend + Weight()
//	LatencyBackend  Backend + ActiveConnections() + AverageLatency()，仅 LeastTime 使用
//
// 需要记忆的核心词汇只有 4 个（Selector、HashSelector、Backend、WeightedBackend）；
// ConsistentHashSelector 与 TrackedSelector 只是既有能力的组合，
// RequestReleaser 与 LatencyBackend 只是单项能力的声明。
//
// 命名为何用 Request 而非 Conn：三个实现 RequestReleaser 的算法所计的量并不相同
// （LeastConn 计在途连接数、P2C 计衰减负载、ActiveRequestBias 计活跃请求数），
// 「Conn」一词对 P2C 不成立，故统一取「在途请求」语义。
//
// # Select / Release 配对
//
// 返回 TrackedSelector 的构造函数（NewLeastConn、NewP2C、NewP2CWithOptions、
// NewActiveRequestBias、NewActiveRequestBiasWithOptions）会在 Select 时递增所选后端的
// 内部计数，调用方必须在请求结束时对**同一个** backend 调用一次 Release 递减它。
// Release 是接口上的静态方法，无需类型断言。
//
// 漏掉 Release 会让计数只增不减，被抬高计数的后端遭系统性饿死；
// 释放错后端（例如硬编码 backends[0]）同样会破坏计数。正确写法：
//
//	sel := lb.NewLeastConn()
//	backend := sel.Select(backends)
//	defer sel.Release(backend) // 与上面的 Select 一一对应
//
// LeastTime 不在此列：它的三个评分输入（latency 与 conns 来自 LatencyBackend，
// weight 来自 WeightedBackend）全部来自调用方注入的 backend 对象自身，
// Select 从不递增任何内部计数，因此 NewLeastTime 返回纯 Selector，没有 Release。
// 需要递减自己的指标由调用方自行处理。
//
// # 构造函数惯例
//
// 以下 4 种惯例并存，本包有意不做统一（各自的调用点都已稳定，强行归一只会制造无谓的破坏性变更）：
//
//  1. 无参：NewRoundRobin()、NewRandom()、NewWeightedRR()、NewSmoothWeightedRR()、
//     NewEDF()、NewLeastConn()、NewP2C()、NewLeastTime()、NewActiveRequestBias()、
//     NewIPHash()、NewRendezvous()
//  2. opts 直传，传 nil 表示使用默认配置：NewMaglev(nil)、NewRingHash(nil)、NewURIHash(nil)
//  3. WithOptions 变体，与无参版本并存：NewP2CWithOptions(opts)、NewActiveRequestBiasWithOptions(opts)
//  4. WithSeed 变体，用于需要可复现随机序列的场景（如测试）：NewRandomWithSeed(seed)
//
// 惯例 2 与惯例 3 的区别：前者只有一个构造函数、以 nil 表达默认；
// 后者同时提供无参与带参两个入口。选择哪一种取决于该算法的参数是否常被调整。
//
// 所有构造函数都不返回 error：非法配置一律回落到默认值而非报错
// （例如 ARBOptions.Bias <= 0 回落为 1.0，MaglevOptions.TableSize 非质数上修至最近质数）。
//
// 所有算法的 Select/SelectByHash 方法均为线程安全，并支持动态后端列表变化检测。
// 稳态热路径（后端集合未变化时的 Select/SelectByHash/Release）零内存分配。
//
// # 使用约束
//
//   - backends slice 的元素不得在传入 Select 后被原地修改。变化检测的快速路径依据
//     「底层数组地址 + 长度」判等，命中即跳过指纹计算与内部结构重建；原地替换元素
//     既不改变地址也不改变长度，选择器会继续沿用陈旧的内部状态
//   - 后端列表变更应通过创建新的 slice 实现，而非修改现有 slice 的元素。
//     长度变化或底层数组变化都能被正确识别；仅内容变化时需依赖指纹，
//     而指纹只在未命中上述快速路径时才会计算
//   - 请复用同一个 []Backend slice，仅在其内容真正变化时才整体替换。
//     每次调用都新建 slice（即使内容与上次完全相同）会使快速路径的
//     「底层数组地址 + 长度」判等必然失配，所有调用落入慢路径：
//     先做一次 O(n) 指纹计算，再更新缓存的 slice 元数据。
//     两类选择器的慢路径并发代价不同——
//     RCU 型（Maglev、RingHash、Rendezvous、P2C）快路径本为 atomic.Pointer
//     无锁读，慢路径则令每个请求串行经过 rebuild 互斥锁并原子发布一份
//     新的快照副本，无锁读优势完全失效；
//     Mutex 型（LeastConn、ARB、LeastTime、EDF、WeightedRR、SmoothWeightedRR）
//     的 Select 全程本就持有互斥锁，额外代价是锁内的 O(n) 指纹计算。
//     内容相同时两类都不会重建内部数据结构，但每请求的慢路径开销本身
//     已足以让并发吞吐显著退化
//   - backends 中各后端的 Address() 必须唯一。重复地址不受支持：
//     Select 按位置对每个重复位分别递增计数，而 Release 经「地址 → 位置」
//     映射递减，该映射对重复地址只保留最后一个位置，靠前重复位的计数
//     因此只增不减（幽灵计数），且不随后续运行自愈。
//     适用于 LeastConn、ARB（经 connectionTracker）与 P2C
//   - LeastTime 的 Select 在其内部互斥锁临界区内逐后端调用
//     AverageLatency()/ActiveConnections()——本包唯一在热路径锁内执行
//     用户实现代码的算法。两方法的实现必须是非阻塞的纯读取
//     （如 atomic.Load 或读取已维护好的字段）：回调阻塞会让同一选择器上
//     所有并发 Select 排队停摆；回调中也不得再调用同一选择器
//     （sync.Mutex 不可重入，重入立即死锁）
//   - Weight() 应保持在 int32 量级（建议 <= 2^31-1）。库不做权重上界校验：
//     LeastConn/ARB 的加权路径以 int64 交叉相乘比较（conn*weight 与
//     weight*(conn+1)），SmoothWeightedRR 以 int 累加 totalWeight；
//     超大权重会使乘积或累加和越过 int64 上界发生溢出，比较与减法语义
//     随之彻底错乱。权重 <= 2^31-1 时，除非单后端在途计数达到 2^32
//     量级（现实中不可能），上述运算均不溢出
//   - 五个哈希算法（IPHash、URIHash、RingHash、Maglev、Rendezvous）对空 key
//     （len(key) == 0）的行为一致且确定：返回 backends[0]。这不是缺陷，
//     但空 key 流量会集中到第一个后端形成热点；调用方应保证 key 非空
//   - Maglev 的 TableSize 必须为质数，非质数会被自动修正为最近质数——
//     该修正仅在 [2, 1<<23] 采纳区间内成立；越出区间回落 DefaultMaglevTableSize
//
// # 内部约定
//
// 比较函数命名（新增算法时遵循；现有命名极性不同是语义使然，不做统一改名）：
//
//   - 动词表达极性：less* 为「key 小者优」（LeastConn 比 conn 或 conn/weight，
//     EDF 比 deadline），better* 为「score 大者优」（ARB 比 weight/(conns+1)^bias）
//   - 包级纯函数的参数为计数/权重/索引元组，按 (ci, [wi,] i, cj, [wj,] j) 排列，
//     后缀说明比较依据：lessConn（仅 conn，由 leastConn 与 ARB 的等权 bias=1
//     路径共用）、lessWeighted（conn/weight 交叉相乘）、betterARBWeighted
//     （ARB 加权且 bias=1）
//   - 使用 idxHeap 的选择器提供 heapLess 方法作为堆重建（reset）的比较闭包，
//     语义必须与其热路径内联的 siftDown/siftUp 比较严格一致；
//     热路径内联比较是为避免闭包间接调用开销，有意不复用 heapLess
//   - 平局裁决统一为 index 小者优先
//   - edfItem.less 是值接收者方法：EDF 用值堆（deadline 随堆元素一并搬移），
//     不经 idxHeap 的间接寻址
//
// rebuilds 观测字段：Maglev、RingHash、Rendezvous 三个 RCU 型选择器各带一个
// rebuilds int 字段，记录实际重建次数。该字段只为失效协议测试而存在
// （cache_rebuild_test.go 据此验证「同内容新 slice 不触发全量重建」），
// 不参与任何选择逻辑；写入必须持有该选择器的 rebuild 互斥锁。
// 其余选择器有意不带同类字段：为测试可观测性给生产结构体加字段
// 是仅限这三个 RCU 实现的例外折中，不是通用模式，新增算法不应效仿。
package lb
