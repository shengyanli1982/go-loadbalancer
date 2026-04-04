# 2026-03-21 官方命名 profile 落地

## 背景
- 基于最新 PRD/roadmap 与当前代码真相，选择 P0 中最小可交付代码切片：官方命名场景 profile。
- 未在本轮实现 decision trace / reason code 新 API；该议题保留到下一轮独立设计。

## 已实现内容
- 新增 `config/official_profile.go`。
- 提供 `type OfficialProfile string` 及以下常量：
  - `OfficialProfileSafeDefault`
  - `OfficialProfileLowLatency`
  - `OfficialProfileLLMSessionAffinity`
- 提供 `DefaultConfigForProfile(profile OfficialProfile) (Config, error)`：
  - `SafeDefault`：保留当前 `DefaultConfig()` 的保守默认行为。
  - `LowLatency`：为 `generic` / `llm-prefill` / `llm-decode` 统一应用 `least_request + health_gate`，降级链为 `policy_ranked -> least_request`。
  - `LLMSessionAffinity`：
    - `llm-prefill`：`health_gate + tenant_quota + llm_budget_gate + llm_kv_affinity + llm_stage_aware`
    - `llm-decode`：`health_gate + tenant_quota + llm_budget_gate + llm_session_affinity + llm_stage_aware`
    - 降级链为 `policy_ranked -> least_request`
- 复用现有 `DefaultConfig`、`WithRouteProfile`、`RouteProfileFor` 机制，不改 `balancer` 热路径。

## 测试与验证
- 先在 `config/config_test.go` 新增 RED 用例，观察到编译失败：缺少 `DefaultConfigForProfile` / `OfficialProfile`。
- 新增测试覆盖：
  - `SafeDefault`
  - `LowLatency`
  - `LLMSessionAffinity`
  - 未知 profile 返回 `ErrInvalidConfig`
  - profile 配置允许后续显式 override
- 关键验证命令：
  - `go test ./config -run "OfficialProfile|Profile" -v`
  - `go test ./config ./balancer -v`
  - `go run ./examples/basic-routing`
  - `go test ./...`
- 上述命令在 2026-03-21 均通过。

## 文档/示例更新
- 新增 runbook：`docs/superpowers/plans/2026-03-21-official-profile-runbook.md`
- `README.md` 的 Quick Start 和 Config Experience 改为 profile-first 入口。
- `examples/basic-routing/main.go` 改为使用 `DefaultConfigForProfile(OfficialProfileSafeDefault)`。

## 后续建议
- 下一轮可独立设计 `DecisionTrace` / `ReasonCode`，不要与 profile 入口混在一轮实现。
- 如后续需要更丰富的 profile 体验，可在当前 helper 基础上评估 `MustDefaultConfigForProfile` 或 profile 列表 API，但本轮刻意未做以避免过度设计。