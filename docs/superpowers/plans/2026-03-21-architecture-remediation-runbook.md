# go-loadbalancer 架构收敛实施计划

> **For agentic workers:** REQUIRED: Use `plan-runbook-execute` for development execution and add `review-spec-implementation` as the post-implementation review gate. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 在不改变当前 SDK 边界的前提下，分阶段修复架构契约问题，并为后续演进建立稳定接口。

**Architecture:** 第一阶段先修正文档和接口契约，避免继续在漂移语义上扩展；第二阶段重构失败语义、策略职责和亲和边界；第三阶段才评估反馈闭环和 registry 演进。整个过程坚持小步、可回滚、优先测试和文档先行。

**Tech Stack:** Go 1.22、testing/testify、Markdown 设计文档、现有插件注册与 Balancer 热路径实现。

---

## 文件影响图

### Stage 1 预计涉及

- 修改：`balancer/balancer.go`
- 修改：`README.md`
- 修改：`plugin/algorithm/plugin.go`
- 修改：`plugin/policy/plugin.go`
- 修改：`plugin/objective/plugin.go`
- 修改：`types/types.go`
- 修改：`balancer/balancer_test.go`
- 修改：`types/validation.go`
- 修改：`types/validation_test.go`
- 修改：`docs/architecture-review-2026-03-21.md`
- 新建：`docs/design/stage1-plugin-context-and-node-snapshot.md`

### Stage 2 预计涉及

- 修改：`balancer/balancer.go`
- 修改：`plugin/policy/plugin.go`
- 修改：`plugin/policy/healthgate/health_gate.go`
- 修改：`plugin/policy/tenantquota/tenant_quota.go`
- 修改：`plugin/policy/llmbudgetgate/llm_budget_gate.go`
- 修改：`plugin/policy/llmkvaffinity/llm_kv_affinity.go`
- 修改：`plugin/policy/llmsessionaffinity/llm_session_affinity.go`
- 修改：`plugin/policy/llmstageaware/llm_stage_aware.go`
- 修改：`plugin/policy/llmtokenqueue/llm_token_aware_queue.go`
- 修改：`balancer/balancer_test.go`
- 修改：`README.md`
- 新建：`types/failure_reason.go`
- 新建：`balancer/affinity_store.go`
- 新建：`docs/design/stage2-fallback-policy-affinity.md`

### Stage 3 预计涉及

- 修改：`registry/registry.go`
- 修改：`telemetry/telemetry.go`
- 修改：`balancer/balancer.go`
- 修改：`README.md`
- 修改：`registry/registry_test.go`
- 新建：`balancer/report_result.go`
- 新建：`balancer/state_store.go`
- 新建：`docs/design/stage3-feedback-and-registry.md`

---

## Chunk 1: Stage 1

### Task 1: 清理语义漂移

**Files:**

- Modify: `balancer/balancer.go`
- Modify: `README.md`
- Modify: `docs/architecture-review-2026-03-21.md`

- [ ] **Step 1: 搜索已失效的模型语义描述**

Run: `rg -n "模型|target model|supports target model|model availability|目标模型" balancer README.md docs/architecture-review-2026-03-21.md`
Expected: 列出所有仍然残留的旧语义位置。

- [ ] **Step 2: 为每一处漂移语义建立替换清单**

输出内容：

- 原描述
- 新描述
- 影响文件

Expected: 形成一份可逐条执行的文案修正清单。

- [ ] **Step 3: 修改代码注释与文档**

重点修正：

- `balancer/balancer.go` 中过滤步骤注释
- README 中与当前实现不一致的能力描述

Expected: 文档与实现语义一致，不再出现“模型可用性过滤”之类过时表达。

- [ ] **Step 4: 运行文本回归检查**

Run: `rg -n "supports target model|目标模型|model availability" balancer README.md docs/architecture-review-2026-03-21.md`
Expected: 不再命中过时表述，或只保留明确标注为历史背景的说明。

- [ ] **Step 5: 运行基础测试**

Run: `go test ./balancer ./types ./config`
Expected: PASS

### Task 2: 统一插件 `context` 设计

**Files:**

- Modify: `plugin/algorithm/plugin.go`
- Modify: `plugin/policy/plugin.go`
- Modify: `plugin/objective/plugin.go`
- Modify: `balancer/balancer.go`
- Modify: `balancer/balancer_test.go`
- Create: `docs/design/stage1-plugin-context-and-node-snapshot.md`

- [ ] **Step 1: 写设计草案，先不直接改实现**

文档必须回答：

- 算法、策略、目标函数是否都引入 `context.Context`
- 旧接口如何兼容
- 兼容窗口内 Balancer 如何做适配

Expected: 产出一页可评审设计草案。

- [ ] **Step 2: 为接口兼容性写失败测试或设计用例**

建议覆盖：

- 旧插件仍可工作
- 新插件可以收到 `ctx`
- `ctx` 取消时整条链路行为可预期

Expected: 测试名称和场景列表明确。

- [ ] **Step 3: 修改接口定义**

建议路径：

- 先增加新的带 `ctx` 接口
- 再在 Balancer 内做适配层
- 避免直接删旧接口

Expected: 代码编译通过，兼容逻辑清晰。

- [ ] **Step 4: 在热路径中接入 `ctx`**

重点检查：

- `Route()`
- `fallback()`
- objective 路径是否还能保持现有超时语义

Expected: `ctx` 能贯穿主要决策链。

- [ ] **Step 5: 运行定向测试**

Run: `go test ./balancer -run "Route|Objective" -v`
Expected: PASS

- [ ] **Step 6: 运行全量测试**

Run: `go test ./...`
Expected: PASS

### Task 3: 产出增强版 `NodeSnapshot` 契约草案

**Files:**

- Modify: `types/types.go`
- Modify: `types/validation.go`
- Modify: `types/validation_test.go`
- Modify: `README.md`
- Create: `docs/design/stage1-plugin-context-and-node-snapshot.md`

- [ ] **Step 1: 明确字段分层**

必须区分：

- 核心字段
- 可选字段
- 暂不进入核心类型、继续放 `Metadata` 的字段

Expected: 在设计文档中形成表格。

- [ ] **Step 2: 为新增或调整的字段写验证规则**

重点考虑：

- 时间字段是否允许空
- 冷却字段如何表达
- 失败原因是枚举还是自由文本

Expected: 验证规则明确，边界条件写清楚。

- [ ] **Step 3: 修改类型定义**

Expected: `types/types.go` 中的状态字段表达更清晰，但不过度膨胀。

- [ ] **Step 4: 补齐校验测试**

Run: `go test ./types -v`
Expected: PASS，新增字段的非法输入有测试覆盖。

- [ ] **Step 5: 更新 README 契约说明**

Expected: README 里明确写出外部系统应提供哪些状态语义。

### Stage 1 验收

- [ ] `go test ./...` 全部通过
- [ ] 过时语义已清理
- [ ] 插件上下文设计已落地且兼容旧插件
- [ ] 节点状态契约有代码和文档双重落地

---

## Chunk 2: Stage 2

### Task 4: 失败原因模型化

**Files:**

- Create: `types/failure_reason.go`
- Modify: `balancer/balancer.go`
- Modify: `balancer/balancer_test.go`
- Modify: `README.md`
- Create: `docs/design/stage2-fallback-policy-affinity.md`

- [ ] **Step 1: 设计失败原因枚举**

至少考虑：

- `NoCandidate`
- `AlgorithmError`
- `PolicyReject`
- `ObjectiveTimeout`
- `ObjectiveError`
- `AffinityMiss`

Expected: 失败原因有清晰命名和触发边界。

- [ ] **Step 2: 在 Balancer 中标注失败来源**

Expected: 算法失败、策略失败、目标函数失败不再只是一段字符串，而是结构化原因。

- [ ] **Step 3: 调整 fallback 入口参数**

Expected: fallback 不只接收 `error`，还能知道“失败类型”。

- [ ] **Step 4: 补定向测试**

Run: `go test ./balancer -run "Fallback|Objective|Policy" -v`
Expected: PASS，并能覆盖不同失败类型。

### Task 5: 拆清 policy 角色

**Files:**

- Modify: `plugin/policy/plugin.go`
- Modify: `plugin/policy/healthgate/health_gate.go`
- Modify: `plugin/policy/tenantquota/tenant_quota.go`
- Modify: `plugin/policy/llmbudgetgate/llm_budget_gate.go`
- Modify: `plugin/policy/llmkvaffinity/llm_kv_affinity.go`
- Modify: `plugin/policy/llmsessionaffinity/llm_session_affinity.go`
- Modify: `plugin/policy/llmstageaware/llm_stage_aware.go`
- Modify: `plugin/policy/llmtokenqueue/llm_token_aware_queue.go`
- Modify: `balancer/balancer.go`

- [ ] **Step 1: 出一版职责映射表**

必须标清：

- 哪些策略属于过滤
- 哪些属于排序
- 哪些属于粘性命中
- 哪些属于硬约束

Expected: 所有现有策略都能被放进明确类别。

- [ ] **Step 2: 优先做内部拆分，不强制对外 API 大爆炸**

Expected: 先在 Balancer 内部把不同职责分段执行，必要时通过适配层维持旧接口。

- [ ] **Step 3: 调整相关策略实现**

Expected: `health_gate`、`tenant_quota`、`llm_budget_gate` 这类 guard 行为表达更直接。

- [ ] **Step 4: 跑相关测试**

Run: `go test ./plugin/policy/... ./balancer -v`
Expected: PASS

### Task 6: 抽象 `AffinityStore`

**Files:**

- Create: `balancer/affinity_store.go`
- Modify: `balancer/balancer.go`
- Modify: `balancer/balancer_test.go`
- Modify: `README.md`
- Modify: `docs/design/stage2-fallback-policy-affinity.md`

- [ ] **Step 1: 设计接口**

至少包含：

- `Get`
- `Set`
- `Delete`
- TTL 说明

Expected: 接口足够小，不泄漏底层存储实现。

- [ ] **Step 2: 提供默认内存实现**

Expected: 当前行为保持兼容。

- [ ] **Step 3: 将 Balancer 的 session affinity map 替换为接口依赖**

Expected: 当前功能不变，但边界变清晰。

- [ ] **Step 4: 补测试**

Run: `go test ./balancer -run "Affinity|Session" -v`
Expected: PASS

### Stage 2 验收

- [ ] fallback 可以基于失败原因表达
- [ ] policy 职责边界比现在清晰
- [ ] 亲和状态不再硬编码为裸 map
- [ ] `go test ./...` 全部通过

---

## Chunk 3: Stage 3

### Task 7: 定义反馈闭环接口

**Files:**

- Create: `balancer/report_result.go`
- Create: `balancer/state_store.go`
- Modify: `telemetry/telemetry.go`
- Modify: `balancer/balancer.go`
- Create: `docs/design/stage3-feedback-and-registry.md`

- [ ] **Step 1: 明确反馈闭环最小边界**

只讨论接口，不先接具体后端。

Expected: 设计文档能回答“路由后哪些结果值得回灌”。

- [ ] **Step 2: 定义最小接口**

至少考虑：

- `ReportResult`
- `StateStore`
- cooldown 写回边界

Expected: 接口边界清楚，不侵入数据面。

- [ ] **Step 3: 设计但不强推默认实现**

Expected: 当前仓库仍可在无共享状态下运行。

### Task 8: 评估并升级 registry

**Files:**

- Modify: `registry/registry.go`
- Modify: `registry/registry_test.go`
- Modify: `README.md`
- Modify: `docs/design/stage3-feedback-and-registry.md`

- [ ] **Step 1: 明确现有 registry 的痛点**

Expected: 把“全局默认 registry”和“无参工厂”的限制写成清单，而不是凭感觉升级。

- [ ] **Step 2: 设计实例级 registry 注入路径**

Expected: 保留全局默认用法，同时支持实例级注入。

- [ ] **Step 3: 评估 typed constructor 是否现在就需要**

Expected: 有明确决策：进入实现、继续延期、或仅保留设计接口。

- [ ] **Step 4: 运行 registry 相关测试**

Run: `go test ./registry -v`
Expected: PASS

### Stage 3 验收

- [ ] 反馈闭环边界已定义
- [ ] registry 演进方向已明确
- [ ] 不破坏当前 SDK 轻量定位

---

## 依赖关系

- Stage 2 依赖 Stage 1
- Stage 3 依赖 Stage 2
- `AffinityStore` 抽象应建立在 Stage 1 的状态契约稳定之后
- feedback / registry 演进应建立在 Stage 2 的失败语义和策略分层稳定之后

---

## 建议提交节奏

- Stage 1 每个任务独立提交
- Stage 2 至少拆成三个提交：
  - 失败原因模型
  - policy 职责调整
  - affinity store 抽象
- Stage 3 先提交设计文档，再决定是否进入代码实现

---

## 建议验证命令

- 全量测试：`go test ./...`
- Balancer 定向：`go test ./balancer -v`
- Policy 定向：`go test ./plugin/policy/... -v`
- Types 定向：`go test ./types -v`
- Registry 定向：`go test ./registry -v`

---

## 执行交接

计划已保存到：

`docs/superpowers/plans/2026-03-21-architecture-remediation-runbook.md`

建议先执行 Stage 1，再根据结果决定是否推进 Stage 2。
