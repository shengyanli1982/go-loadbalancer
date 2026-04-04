# 2026-03-21 DecisionTrace 增量完善：CandidateDiff + 更细粒度步骤

## 本轮增量
- 在 `types.DecisionTraceStep` 中新增 `CandidateDiff *types.CandidateDiff`。
- 新增 `types.CandidateDiff`，当前仅保留：
  - `BeforeNodeIDs`
  - `AfterNodeIDs`
- `balancer` 内部 trace 记录由简单 node list 升级为前后状态对比：
  - filter：输入节点 -> 过滤后节点
  - algorithm：过滤后节点 -> 候选节点
  - policy：重排前候选 -> 重排后候选
  - objective success：候选列表 -> 选中节点
  - objective timeout/error：保留失败前候选列表
  - fallback：原输入集合或 ranked 集合 -> 最终回退选中节点
- 对失败步骤的 `CandidateNodeIDs` 语义调整为：若没有 after 集合，则回落使用 before 集合作为“当时参与判断的候选节点”。

## 测试
- 新增 `TestRouteWithTracePolicyStepIncludesCandidateDiff`
- 新增 `TestRouteWithTraceObjectiveTimeoutIncludesObjectiveStepDetail`
- 定向验证：`go test ./balancer -run "Trace|DecisionTrace" -v`
- 全量验证：`go test ./balancer ./types -v` 和 `go test ./...`

## 文档
- `README.md` 中 `Structured Explain` 段已更新，说明 trace 现已包含 per-step `CandidateDiff`。
- `docs/superpowers/plans/2026-03-21-decision-trace-runbook.md` 已同步把 `CandidateDiff` 标记为本轮范围。