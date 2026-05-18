# 2026-03-21 DecisionTrace / ReasonCode 最小实现

## 目标
- 在不破坏现有 `Balancer` / `Route` API 的前提下，为调用方提供结构化 explain 能力。

## 已实现内容
- 新增 `types/decision_trace.go`。
- 新增 `types.ReasonCode` 枚举，当前覆盖：
  - `filter_passed`
  - `filter_rejected`
  - `algorithm_selected`
  - `algorithm_error`
  - `policy_applied`
  - `policy_rejected`
  - `objective_selected`
  - `objective_timeout`
  - `objective_error`
  - `fallback_applied`
  - `route_selected`
- 新增 `types.DecisionTrace`：
  - `RouteClass`
  - `InputNodeCount`
  - `FilteredNodeCount`
  - `SelectedNodeID`
  - `FallbackUsed`
  - `FailureReason`
  - `Steps`
- 新增 `types.DecisionTraceStep`：
  - `Stage`
  - `Code`
  - `Plugin`
  - `Detail`
  - `CandidateNodeIDs`
- 在 `balancer/balancer.go` 中新增 `TraceBalancer` 扩展接口：
  - `RouteWithTrace(ctx, req, nodes) (types.Candidate, types.DecisionTrace, error)`
- `a2xBalancer` 额外实现 `RouteWithTrace`，并将原 `Route` 主流程抽到内部 `route(..., trace *types.DecisionTrace)`，只在 `trace != nil` 时记录结构化步骤。
- fallback 逻辑可写入 `FallbackUsed`、`FailureReason` 和 `fallback_applied` step。

## TDD 证据
- RED：先在 `balancer/balancer_test.go` 新增 `TestRouteWithTraceSuccess` 与 `TestRouteWithTraceFallbackOnAlgorithmError`，观察到编译失败：缺少 `TraceBalancer`、`ReasonCode`、`DecisionTraceStep`。
- GREEN：新增最小结构与实现后，定向测试通过。

## 验证命令
- `go test ./balancer -run "Trace|DecisionTrace" -v`
- `go test ./balancer ./types -v`
- `go test ./...`

## 文档
- 新增 runbook：`docs/superpowers/plans/2026-03-21-decision-trace-runbook.md`
- `README.md` 新增 `Structured Explain` 段，展示 `TraceBalancer` type assert + `RouteWithTrace` 最小用法。

## 非目标
- 当前不提供 `CandidateDiff`
- 不做完整排序变化可视化
- 不替代 telemetry schema

## 后续建议
- 若下一轮继续增强 explain，可优先补：
  1. policy/objective 更多 step 细节测试
  2. 成功/失败路径的更细粒度 detail 语义
  3. CandidateDiff 或排序变化摘要，但应独立评估性能与结构膨胀风险