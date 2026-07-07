// Package lb 提供生产级负载均衡算法实现
//
// 支持以下算法：
//   - RoundRobin: 轮询，最简单高效
//   - WeightedRR: 加权轮询，按权重比例分配
//   - SmoothWeightedRR: 平滑加权轮询（nginx 风格），权重分布更均匀
//   - EDF: 最早截止时间优先加权轮询（Traefik 风格），支持浮点权重等效
//   - Random: 随机选择
//   - LeastConn: 最少连接数（支持加权，对标 nginx least_conn）
//   - P2C: Power of Two Choices，适合大规模分布式系统
//   - LeastTime: 延迟感知路由（对标 Traefik v3.6/Envoy），score = latency*(1+conns)/weight
//   - ActiveRequestBias: 柔性 LeastConn（对标 Envoy WLR），score = weight/(conns+1)^bias
//   - IPHash: IP 哈希，基于客户端 IP 会话保持
//   - URIHash: URI 哈希，基于请求 URI 一致性路由
//   - Rendezvous: 会合点哈希（HRW），天然支持权重，增删节点仅影响 1/N keys
//   - RingHash: 一致性哈希环，后端变化时最小化 key 迁移
//   - Maglev: Google Maglev 一致性哈希，O(1) 查找
//
// 所有算法的 Select/SelectByHash 方法均为线程安全，支持动态后端列表变化检测。
//
// 使用约束：
//   - backends slice 的元素不得在传入 Select 后被原地修改
//   - 后端列表变更应通过创建新 slice 实现，而非修改现有 slice 的元素
//   - Maglev 的 TableSize 必须为质数，非质数会被自动修正为最近质数
package lb
