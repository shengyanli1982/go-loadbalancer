// Package lb 提供生产级负载均衡算法实现
//
// 支持以下算法：
//   - RoundRobin: 轮询，最简单高效
//   - WeightedRR: 加权轮询，按权重比例分配
//   - SmoothWeightedRR: 平滑加权轮询（nginx 风格），权重分布更均匀
//   - Random: 随机选择
//   - LeastConn: 最少连接数（支持加权，对标 nginx least_conn）
//   - P2C: Power of Two Choices，适合大规模分布式系统
//   - IPHash: IP 哈希，基于客户端 IP 会话保持
//   - URIHash: URI 哈希，基于请求 URI 一致性路由
//   - RingHash: 一致性哈希环，后端变化时最小化 key 迁移
//   - Maglev: Google Maglev 一致性哈希，O(1) 查找
//
// 所有算法均为并发安全，支持动态后端列表变化检测。
package lb
