# go-loadbalancer 架构评审

日期：2026-03-21

状态：进行中

范围：对当前 `go-loadbalancer` 仓库做架构级评审，并与 GitHub 上 star 大于 `500` 的传统负载均衡项目和 LLM 路由项目进行对照，识别逻辑不一致、架构短板和可执行优化项。

---

## 1. 评审目标

- 目标不是把当前仓库强行改造成 Envoy、Traefik 这类完整代理。
- 目标是判断：在“SDK 级路由决策内核”这个边界下，当前设计哪些地方是合理取舍，哪些地方已经偏离了优秀项目的核心架构模式。
- 输出物是讨论底稿，不是代码改造。

### 1.1 本轮结论的适用边界

- 本仓库当前定位是“路由决策 SDK”，不是数据面代理。
- 因此以下能力不作为本轮缺陷：
  - 服务发现
  - 主动健康检查
  - HTTP / gRPC 转发
  - 控制面编排
  - 自动扩缩容
- 但是，一旦项目明确把“探测、状态更新、冷却、配额”等逻辑交给外部系统，就必须把输入状态契约和扩展边界定义得更强，否则 SDK 会变成“只够 demo，不够长期演进”的中间层。

---

## 2. 问题定义

### 2.1 问题叙述

- 我是：一个维护 Go 路由内核的开发者，希望同一套核心同时支持通用流量和 LLM 推理流量。
- 我想做到：在保持核心简单、热路径稳定的同时，让系统具备长期演进能力。
- 但现在的问题是：仓库的主链路虽然清晰，但部分契约已经弱于它对外宣称的架构能力，而且少数注释与实现已经漂移。
- 根因是：项目从更早的模型语义路由思路，逐步收敛成更泛化的 SDK 边界后，状态语义、插件接口、降级模型、反馈闭环没有同步完成收口。

### 2.2 最终问题陈述

`go-loadbalancer` 当前最需要解决的，不是“补更多算法”，而是把路由状态、插件扩展、回退语义和会话亲和这些核心契约做得更清晰、更稳定，否则它虽然已经具备可用的热路径实现，但还不足以支撑长期的生产级演进。

---

## 3. 当前仓库的真实边界

README 已经明确给出了仓库定位：

- 这是一个 SDK 级路由决策库。
- 上游探测由外部系统实现。
- 外部系统负责把节点状态写入 `NodeSnapshot`。

这个边界本身没有问题。真正的问题是：

- 一旦边界成立，`NodeSnapshot` 和插件接口就必须足够强；
- 否则你只是把复杂度转移给上游，但 SDK 自己没有给出稳定接入契约。

---

## 4. 本地实现证据

### 4.1 核心热路径

核心链路非常明确：

`filter -> algorithm -> policies -> optional objective -> fallback`

```mermaid
flowchart LR
    A[输入 RequestContext + NodeSnapshot] --> B[filter]
    B --> C[algorithm.SelectCandidates]
    C --> D[policy.ReRank 链]
    D --> E{objective 是否启用}
    E -- 是 --> F[objective.Choose]
    E -- 否 --> G[返回 ranked[0]]
    F -->|成功| H[返回 candidate]
    F -->|失败| I[fallback]
    C -->|失败或空候选| I
    D -->|软策略失败| I
    D -->|硬策略失败| J[直接失败]
    I --> K[policy_ranked 或 fallback algorithm]
```

图示说明：这张图回答的是“当前路由链路的控制流到底长什么样，以及失败是在哪些节点被拦截或降级的”。

证据锚点：

- Balancer 初始化：
  - `balancer/balancer.go:66`
- 路由主流程：
  - `balancer/balancer.go:235`
- 回退链：
  - `balancer/balancer.go:484`
- 硬约束过滤：
  - `balancer/balancer.go:568`

### 4.2 配置与路由类覆盖

当前仓库已经支持按 `RouteClass` 覆盖算法、策略和降级链，这一点是好的。

证据锚点：

- 路由类配置：
  - `config/route_profile.go:15`
- 路由类合并逻辑：
  - `config/route_profile.go:27`

### 4.3 输入状态模型

请求侧状态：

- `types.RequestContext`
  - `types/types.go:12`

节点侧状态：

- `types.NodeSnapshot`
  - `types/types.go:29`

### 4.4 插件接口

- 算法插件：
  - `plugin/algorithm/plugin.go:5`
- 策略插件：
  - `plugin/policy/plugin.go:5`
- 目标函数插件：
  - `plugin/objective/plugin.go:9`
- 仅 objective 具备 `context` 感知扩展：
  - `plugin/objective/plugin.go:15`

### 4.5 会话亲和与可靠性试验逻辑

- decode 阶段亲和注入：
  - `balancer/balancer.go:623`
- 路由结果写回亲和：
  - `balancer/balancer.go:644`
- pool 选择与 outlier 过滤试验逻辑：
  - `balancer/balancer.go:659`
  - `balancer/balancer.go:679`
  - `balancer/balancer.go:736`

---

## 5. 外部对照样本

本轮抽样的外部项目都明显高于 `500` stars，并且覆盖两类体系：

- 传统负载均衡 / 代理：
  - Envoy
  - Traefik
  - Fabio
- LLM 路由 / AI Gateway：
  - LiteLLM
  - AIBrix

### 5.1 参考项目与关注点

| 项目 | 类型 | 本轮主要关注点 |
| --- | --- | --- |
| Envoy | 传统代理 / 上游负载均衡 | cluster、health、outlier、lb policy、retry 的分层 |
| Traefik | 动态配置代理 | provider 到 router / service / load balancer 的结构 |
| Fabio | 服务发现驱动负载均衡 | 状态变更到路由表的原子更新 |
| LiteLLM | LLM Gateway | routing strategy、fallback、retry、cooldown、共享状态 |
| AIBrix | LLM 感知路由基础设施 | prefix / KV cache / 推理调度 / 控制环 |

### 5.2 本轮核验来源

- Envoy:
  - `https://github.com/envoyproxy/envoy`
  - `https://github.com/envoyproxy/envoy/blob/main/api/envoy/config/cluster/v3/cluster.proto`
  - `https://github.com/envoyproxy/envoy/blob/main/api/envoy/config/route/v3/route_components.proto`
  - `https://github.com/envoyproxy/envoy/tree/main/source/extensions/load_balancing_policies`
- Traefik:
  - `https://github.com/traefik/traefik`
  - `https://github.com/traefik/traefik/blob/master/pkg/config/dynamic/http_config.go`
  - `https://github.com/traefik/traefik/blob/master/pkg/healthcheck/healthcheck.go`
  - `https://github.com/traefik/traefik/tree/master/pkg/server/service/loadbalancer`
- Fabio:
  - `https://github.com/fabiolb/fabio`
  - `https://github.com/fabiolb/fabio/blob/master/main.go`
- LiteLLM:
  - `https://github.com/BerriAI/litellm`
  - `https://github.com/BerriAI/litellm/blob/main/litellm/router.py`
  - `https://github.com/BerriAI/litellm/blob/main/docs/my-website/docs/routing.md`
  - `https://github.com/BerriAI/litellm/blob/main/docs/my-website/docs/proxy/architecture.md`
- AIBrix:
  - `https://github.com/vllm-project/aibrix`
  - `https://github.com/vllm-project/aibrix/tree/main/pkg/plugins/gateway/algorithms`

---

## 6. 对优秀项目应该学什么

这一节很重要，因为很多评审容易误判成“别人有这个功能，你没有，所以你差”。这不对。

真正值得学习的是架构主干，不是功能表。

### 6.1 Envoy

Envoy 的关键价值不是算法多，而是它把这些概念分得很清楚：

- cluster
- host set
- active health check
- outlier detection
- load balancing policy
- priority / locality
- retry policy

对当前仓库的启发：

- “节点是否可用”
- “节点如何排序”
- “失败后是否重试”
- “异常节点如何处理”

这些最好不要全部挤进同一个 `policy.ReRank()` 抽象里。

### 6.2 Traefik

Traefik 的价值在于：

- provider 负责拿配置
- router 负责匹配流量
- service 负责组织后端
- load balancer / health check / sticky 各自有边界

对当前仓库的启发：

- 状态输入来源
- 路由决策
- 亲和逻辑
- 健康语义

最好不要全都糅在一条热路径里靠约定维持。

### 6.3 Fabio

Fabio 值得学的是：

- 路由表生成和运行态结构是分开的
- 后端状态变化后，通过原子切换更新运行态

对当前仓库的启发：

- “外部状态写入”和“运行态决策结构”最好明确分层；
- 不然以后做热更新、共享状态或多实例一致性时，会越来越难。

### 6.4 LiteLLM

LiteLLM 的关键不是它支持多少模型，而是它把这些能力显式建模了：

- routing strategy
- retries
- fallbacks
- cooldowns
- RPM / TPM 共享状态

对当前仓库的启发：

- LLM 路由不是“再加几个策略插件”就够了；
- 失败原因、冷却状态、预算状态、共享计数，通常都需要一等公民的语义。

### 6.5 AIBrix

AIBrix 的价值是：

- 它把 LLM 特有路由因素直接做成命名明确的路由原语；
- 比如 prefix-cache、prefill/decode 分离、推理指标驱动。

对当前仓库的启发：

- 当 LLM 语义变复杂时，最好把关键语义提升到明确概念层，而不是长期塞进泛化的排序函数。

---

## 7. 当前仓库与优秀项目的差距矩阵

| 维度 | 当前实现 | 优秀项目常见模式 | 评估 |
| --- | --- | --- | --- |
| 热路径可读性 | 很清晰 | 也通常清晰 | 优点 |
| SDK 边界是否明确 | 明确 | 不同项目不同 | 合理取舍 |
| 输入状态契约 | 偏轻 | 往往更强，带时间、失败语义、来源语义 | 偏弱 |
| 插件分层 | 算法 / 策略 / 目标函数 | 更细分，角色更明确 | 偏弱 |
| 上下文传播 | 只有 objective 支持 `context` | 通常整条链都可取消 | 偏弱 |
| 回退模型 | 退化链 | 往往按失败原因分类 | 偏弱 |
| 亲和状态 | 进程内 map | 通常有边界、TTL、共享或外置存储 | 偏弱 |
| 故障隔离 | `Healthy` / `Outlier` / TTL | 更丰富的健康与 ejection 语义 | 偏弱 |
| 可演进性 | 现在够用 | 中长期扩展会吃抽象红利 | 有隐患 |

---

## 8. 问题与外部模式映射

| 当前议题 | 对应外部模式 | 借鉴点 |
| --- | --- | --- |
| 插件接口没有统一 `context` | Envoy、LiteLLM | 整条决策链应具备统一超时、取消和外部状态读取能力 |
| `NodeSnapshot` 契约过薄 | Envoy、AIBrix | 健康、异常、冷却、观测窗口最好有更明确的状态语义 |
| fallback 不区分失败原因 | LiteLLM | 超时、预算、冷却、策略拒绝通常应走不同降级路径 |
| policy 抽象过载 | Envoy、Traefik | filter、sticky、lb、health、guard 最好不要长期混成一个接口 |
| 亲和状态仅进程内存 | Traefik、LiteLLM | sticky / affinity 往往要有 TTL、共享或至少明确存储边界 |
| 状态输入与运行态结构耦合偏紧 | Fabio | 外部状态变化最好通过更稳定的中间结构进入运行时 |
| LLM 语义主要靠策略排序承载 | AIBrix | prefix / cache / stage 等关键语义在复杂场景下应逐步提升为明确原语 |

---

## 9. 发现的问题

### 9.1 P0：逻辑不一致或立即会误导后续设计的问题

#### P0-1 注释和实现曾经漂移（Stage 1 已修复）

本轮评审时，`Route()` 中的注释仍然把过滤步骤描述成“健康节点 + 额外模型能力门槛”。

而当前 `filterNodes()` 实现只做了：

- `Healthy`
- `FreshnessTTLms`

而模型语义路由已经在之前版本里移除。

这个问题需要优先修复，因为它会：

- 误导后来者继续沿着不存在的模型语义理解代码；
- 让设计讨论建立在错误前提上。

证据：

- `balancer/balancer.go:285`
- `balancer/balancer.go:568`

修复状态：

- Stage 1 已将 `Route()` 注释改为“健康状态 + 快照新鲜度约束”的当前语义。

#### P0-2 插件接口没有统一的 `context` 传播

现在只有 objective 层允许 `ContextPlugin`：

- 算法层没有 `ctx`
- 策略层没有 `ctx`
- objective 才有 `ctx`

这会带来几个直接问题：

- 未来策略层做远程状态读取会很 awkward
- 无法统一超时与取消语义
- 整条路由链不能做一致的 deadline 管理

这类问题在早期不明显，但一旦插件复杂度上来，就会变成接口级债务。

证据：

- `plugin/algorithm/plugin.go:5`
- `plugin/policy/plugin.go:5`
- `plugin/objective/plugin.go:15`

#### P0-3 会话亲和状态只是进程内临时状态

当前实现是：

- `map[sessionID]nodeID`
- 无 TTL
- 无容量限制
- 无淘汰策略
- 无跨实例共享语义

这意味着：

- 单进程内可用
- 多实例部署时语义不稳定
- 长生命周期进程会持续堆积状态

对一个“准备走向生产的路由内核”来说，这个边界太弱。

证据：

- `balancer/balancer.go:623`
- `balancer/balancer.go:644`

### 9.2 P1：结构上已经偏弱，建议进入下一阶段重构的点

#### P1-1 `NodeSnapshot` 契约过薄

当前 `NodeSnapshot` 只有：

- 健康布尔值
- outlier 布尔值
- TTL
- 一组即时指标

问题在于，既然你已经明确“探测和状态更新在外部系统”，那么 SDK 应该对“状态是什么”给出更稳定的契约。现在缺少的可能包括：

- 观测时间
- 状态版本
- 状态来源
- 异常原因
- 冷却截止时间
- 统计窗口信息

否则上游系统和 SDK 之间的接口只是“字段凑得差不多就行”，这不够稳。

证据：

- `types/types.go:29`
- `README.md:17`

#### P1-2 fallback 只有“步骤链”，没有“原因分类”

现在 fallback 的基本含义是：

- `policy_ranked`
- 或者换一个算法重新选

这在传统 LB 里能工作，但在 LLM 场景里通常不够，因为失败原因经常不同：

- 超时
- 预算超限
- 节点冷却
- 上下文窗口不足
- 供应商失败
- 会话亲和 miss

优秀项目通常会把“因为什么失败”做成显式路由条件，而不是只做一条退化链。

证据：

- `balancer/balancer.go:484`

#### P1-3 `policy.ReRank()` 承载了过多不同语义

现在很多不同职责都被塞进 `ReRank()`：

- 健康门控
- 租户配额
- LLM 预算门控
- KV 亲和
- Session 亲和
- stage-aware
- token-aware queue

这些职责里有些本质是：

- filter
- 有些是 sticky
- 有些是 score adjust
- 有些是 hard gate

长期都用 `ReRank()` 表达，会让插件生态越来越难看懂，也难组合。

证据：

- `plugin/policy/plugin.go:5`
- `balancer/balancer.go:327`

#### P1-4 ReliabilityPilot 的故障语义是隐式的

当前 `filterOutliers()` 的语义是：

- 能过滤就过滤
- 如果全都 outlier，则退回原集合

这不一定错，但它其实代表一种很重要的设计选择：

- 是否 fail-open
- 是否 fail-closed
- panic threshold 是多少

现在这些选择都藏在实现里，不在配置契约里。

证据：

- `balancer/balancer.go:659`
- `balancer/balancer.go:736`

### 9.3 P2：当前不一定马上要改，但未来一定会碰到的扩展瓶颈

#### P2-1 注册中心过于全局，工厂过于轻量

现在是：

- 全局默认 registry
- `func() Plugin` 无参工厂

这很简单，但限制了未来几件事：

- typed plugin config 注入
- 插件实例级生命周期
- 多实例隔离测试
- 更细粒度的热更新

证据：

- `registry/registry.go:13`

#### P2-2 观测只有输出，没有反馈闭环

现在只有 `Telemetry Sink` 出口，没有配套的：

- `ReportResult`
- cooldown 写回
- adaptive penalty
- 共享状态更新接口

也就是说，SDK 只能消费外部状态，不能帮助形成“状态反馈闭环”。

对通用 LB 来说，这可以接受一段时间；
对 LLM 路由来说，迟早会成为能力上限。

证据：

- `telemetry/telemetry.go:32`

---

## 10. 哪些不是缺陷

这一节是为了避免过度设计。

以下内容本轮明确不认定为缺陷：

- 没有内建服务发现
- 没有主动健康检查器
- 没有内建 HTTP 代理
- 没有控制面控制器
- 没有自动扩缩容

原因很简单：

- 这些能力属于另一个产品形态；
- 当前仓库没有承诺这些能力；
- 把这些能力直接要求进来，会破坏当前 SDK 边界。

---

## 11. 优化清单

### 11.1 第一阶段：必须先做的事

#### 议题 A：修正语义漂移

目标：

- 清理所有仍然残留的“模型语义路由”描述
- 保证注释、README、示例、测试命名与当前实现一致

原因：

- 这是最便宜、但收益极高的架构清障动作

#### 议题 B：统一插件上下文模型

目标：

- 让算法、策略、目标函数都具备统一 `context` 语义

原因：

- 这是后续支持 deadline、外部状态查询、共享状态访问、可取消执行的基础

#### 议题 C：定义更强的节点状态契约

目标：

- 把“外部系统如何提供状态”从隐式约定变成显式契约

建议至少讨论这些字段是否应该进入契约：

- `ObservedAt`
- `Version`
- `Source`
- `CooldownUntil`
- `OutlierReason`
- `Window`

### 11.2 第二阶段：建议进入架构重构的事

#### 议题 D：把 policy 拆成更明确的角色

建议方向：

- `FilterPlugin`
- `ReRankPlugin`
- `AffinityPlugin`
- `GuardPlugin`

不是说一定要拆成这么多接口，但至少应该开始讨论：

- 哪些行为只是排序
- 哪些行为本质是准入门槛
- 哪些行为本质是粘性命中

#### 议题 E：把 fallback 从步骤链升级为原因感知模型

建议方向：

- 按失败类型决定 degrade 路径
- 而不是一律走同一条链

例如后续可以讨论的失败原因：

- `algorithm_error`
- `policy_reject`
- `objective_timeout`
- `budget_exceeded`
- `affinity_miss`
- `cooldown`

#### 议题 F：把 session affinity 改成接口

建议方向：

- 定义 `AffinityStore`
- 默认实现仍可保留进程内版本
- 但接口要支持：
  - TTL
  - 删除
  - 容量边界
  - 外置共享实现

### 11.3 第三阶段：中长期增强

#### 议题 G：引入反馈闭环边界

建议方向：

- `ReportResult`
- `StateStore`
- `CooldownStore`

目标：

- 让 SDK 不只是“读状态做决策”
- 还可以在不侵入数据面的前提下，形成可选的自适应路由闭环

#### 议题 H：升级插件注册与实例化模型

建议方向：

- 从全局默认 registry 逐步过渡到实例级 registry
- 从无参工厂逐步过渡到 typed config / typed constructor

---

## 12. 重构议题卡片

这一节把上面的优化清单收敛成更适合讨论和排期的格式。

### 12.1 议题 A：修正语义漂移

**最小改造范围**

- 清理 `balancer.go` 中仍引用旧模型路由语义的注释
- 检查 README、examples、测试命名、错误描述中是否还有已失效的旧语义

**预期收益**

- 减少误读
- 降低后续设计讨论建立在错误前提上的风险

**潜在风险**

- 风险极低
- 主要风险是遗漏旧文案，导致“局部已修、整体仍漂移”

### 12.2 议题 B：统一插件 `context`

**最小改造范围**

- 为算法和策略插件引入带 `context.Context` 的新接口
- 在 Balancer 热路径中统一传递 `ctx`
- 保留兼容层，避免一次性打断现有插件

**预期收益**

- 统一超时和取消语义
- 为未来外部状态查询、共享状态读取、链路级 deadline 打基础

**潜在风险**

- 属于公共接口改动
- 如果处理不好兼容层，会让现有插件实现断裂

### 12.3 议题 C：增强 `NodeSnapshot` 契约

**最小改造范围**

- 先不急着一次性加很多字段
- 先产出契约草案，明确哪些字段属于核心类型，哪些字段允许继续放 `Metadata`

**建议优先讨论字段**

- `ObservedAt`
- `Version`
- `Source`
- `CooldownUntil`
- `OutlierReason`

**预期收益**

- 让“外部系统如何把状态交给 SDK”变得可验证
- 降低不同上游系统接入时的歧义

**潜在风险**

- 字段一旦进入核心类型，未来修改成本更高
- 如果加得过多，会让类型膨胀

### 12.4 议题 D：拆分 policy 角色

**最小改造范围**

- 先不强推多个接口同时上线
- 先在设计层明确四类行为：
  - filter
  - rerank
  - guard
  - affinity

**预期收益**

- 让插件职责更清楚
- 减少未来把所有新逻辑都塞进 `ReRank()` 的冲动

**潜在风险**

- 这是抽象层重构
- 如果拆分太早，会让 API 复杂度提前上升

### 12.5 议题 E：让 fallback 具备原因语义

**最小改造范围**

- 先不推翻现有 degrade chain
- 先定义失败原因枚举，再决定 degrade chain 如何引用这些原因

**建议优先考虑的失败原因**

- `algorithm_error`
- `no_candidate`
- `policy_reject`
- `objective_timeout`
- `objective_error`
- `affinity_miss`

**预期收益**

- 让 fallback 更适合 LLM 场景
- 降低“所有失败都走同一条路”的粗糙性

**潜在风险**

- 失败原因分类一旦设计得不好，后面会出现大量边界例外

### 12.6 议题 F：抽象 `AffinityStore`

**最小改造范围**

- 定义接口
- 保留当前进程内 map 作为默认实现
- 接口最少支持：
  - `Get`
  - `Set`
  - `Delete`
  - TTL 语义

**预期收益**

- 为多实例和长生命周期进程留出口
- 不改变当前默认部署方式

**潜在风险**

- 如果接口设计得过宽，会把状态存储细节泄漏进路由层

### 12.7 议题 G：引入反馈闭环边界

**最小改造范围**

- 先不接 Redis 或数据库
- 先定义路由后反馈接口，例如 `ReportResult`

**预期收益**

- 给后续 cooldown、自适应路由、结果回灌留出正式扩展点

**潜在风险**

- 如果没有明确边界，容易把 SDK 带向“半个控制面”

### 12.8 议题 H：升级 registry

**最小改造范围**

- 保留默认 registry
- 增加实例级 registry 注入路径
- 评估工厂是否需要升级为 typed constructor

**预期收益**

- 更利于隔离测试
- 更利于插件实例化扩展

**潜在风险**

- 影响插件注册习惯
- 如果时机太早，收益未必立刻体现

---

## 13. 架构演进选项

### 13.1 方案 A：继续保持极简 SDK

特点：

- 不引入共享状态实现
- 不引入控制面
- 只强化契约，不扩大产品边界

建议包含：

- 统一插件 `context`
- 强化 `NodeSnapshot`
- 清晰化 fallback 原因模型
- 把 affinity 改成接口

优点：

- 和当前仓库定位最一致
- 改动收益高、风险相对低
- 不会把项目带偏成“半个网关”

缺点：

- 多实例共享状态能力仍然需要调用方自己补

### 13.2 方案 B：升级为状态感知路由内核

特点：

- 仍然不做数据面代理
- 但开始把共享状态、冷却、反馈写回做成正式边界

建议包含：

- `AffinityStore`
- `StateStore`
- `ReportResult`
- cooldown 语义
- 更强的失败分类

优点：

- 更适合做统一路由基础设施
- 更适合 LLM 场景

缺点：

- API 和实现复杂度会明显上升
- 项目产品定位会变重

### 13.3 当前推荐

当前更推荐先走方案 A。

原因：

- 它最符合仓库目前对外承诺
- 可以先把最危险的架构债务补上
- 也不会堵死以后演进到方案 B 的路

---

## 14. 当前建议的优先级排序

| 优先级 | 议题 | 原因 |
| --- | --- | --- |
| P0 | 修正语义漂移 | 成本低，立即减少误导 |
| P0 | 统一插件 `context` | 是后续扩展的基础设施 |
| P0 | 强化节点状态契约 | SDK 边界成立的前提条件 |
| P1 | 让 fallback 具备原因语义 | 对 LLM 路由尤为关键 |
| P1 | 拆分 policy 角色 | 防止抽象继续恶化 |
| P1 | 亲和状态接口化 | 为多实例和 TTL 留口子 |
| P2 | 反馈闭环接口 | 属于能力上限提升 |
| P2 | registry 升级 | 重要，但不一定最先做 |

---

## 15. 建议结论

这一节不是继续发散，而是给出当前更建议的决策方向。

### 15.0 拍板结果总表

| 议题 | 结论 | 处理方式 |
| --- | --- | --- |
| 仓库继续保持 SDK 边界 | 建议采纳 | 立即确认 |
| 第一阶段优先修契约，不优先补新功能 | 建议采纳 | 立即确认 |
| 统一插件 `context` | 建议采纳 | 进入 Stage 1 |
| 强化 `NodeSnapshot` 契约 | 建议采纳 | 进入 Stage 1 |
| fallback 原因语义化 | 建议采纳 | 进入 Stage 2 |
| policy 拆层 | 建议采纳 | 进入 Stage 2 |
| `AffinityStore` 接口化 | 建议采纳 | 进入 Stage 2 |
| 反馈闭环接口 | 建议延期 | Stage 3 再评估 |
| registry 全量升级 | 建议延期 | Stage 3 再评估 |
| 引入完整控制面 / 数据面 | 不建议本轮做 | 超出当前边界 |

### 15.1 建议立即确认的结论

#### 结论 1：仓库边界保持为“路由决策 SDK”

建议：

- 保持当前产品边界
- 不在下一阶段把项目扩展成完整代理或控制面

原因：

- 当前热路径和可读性是资产
- 一旦把数据面或控制面硬塞进来，会立刻放大复杂度
- 现阶段真正的问题不是边界太小，而是边界契约还不够强

#### 结论 2：第一阶段优先修契约，不优先补功能

建议：

- 先做契约级修正
- 不优先增加新的算法或 LLM 策略

原因：

- 当前算法层已经够支撑研究和初步生产使用
- 更急迫的问题是接口和状态模型的稳定性

#### 结论 3：下一阶段主轴应是“统一上下文 + 强化状态 + 清晰失败语义”

建议：

- 把第一阶段目标聚焦成三件事：
  - 插件 `context` 统一
  - `NodeSnapshot` 契约增强
  - fallback 失败原因模型设计

原因：

- 这三项会直接决定后续 LLM 路由能力能否健康演进

### 15.2 建议暂缓的结论

#### 暂缓项 1：不要现在就做复杂控制环

包括但不限于：

- 自动 cooldown 管理
- 自适应 penalty
- 内建共享状态服务

原因：

- 当前还没有稳定的反馈接口
- 直接上控制环，容易把系统复杂度抬高但收益不稳定

#### 暂缓项 2：不要现在就大拆插件体系

包括但不限于：

- 一次性把 `policy` 拆成多个正式接口并强制迁移

原因：

- 方向是对的
- 但如果在 `context` 和状态契约还没稳定前就动大手术，容易重构两次

#### 暂缓项 3：不要现在就重做 registry

原因：

- registry 的问题存在，但不是眼前最卡主线的问题
- 它更适合在第一阶段契约收敛后再做

---

## 16. Stage 1 最小 Runbook 草案

这一部分假设后续准备从“研究讨论”进入“小步设计或小步改造”。

### 16.1 目标

在不改变当前 SDK 产品边界的前提下，完成第一阶段三项收敛：

1. 修正语义漂移
2. 统一插件 `context`
3. 产出增强版状态契约草案

### 16.2 任务拆分

#### 任务 1：清理漂移语义

范围：

- `balancer/balancer.go`
- `README.md`
- `examples/`
- 相关测试命名或注释

完成标准：

- 不再出现已失效的“模型可用性过滤”描述
- 注释、README、示例与当前实现一致

风险：

- 漏改文案

#### 任务 2：设计统一插件上下文接口

范围：

- `plugin/algorithm/plugin.go`
- `plugin/policy/plugin.go`
- `plugin/objective/plugin.go`
- `balancer/balancer.go`

完成标准：

- 有一版兼容设计草案
- 明确旧接口如何兼容
- 明确 `ctx` 在热路径中的传播方式

风险：

- 如果兼容策略设计不好，会影响现有插件实现

#### 任务 3：产出状态契约草案

范围：

- `types/types.go`
- `README.md`
- 新增设计说明文档或章节

完成标准：

- 明确核心字段和可选字段
- 明确哪些状态必须由外部系统提供
- 明确 TTL / cooldown / outlier 这些语义是否正式进入契约

风险：

- 一旦字段设计过重，会造成类型膨胀

### 16.3 建议执行顺序

1. 先做任务 1
2. 再做任务 2
3. 最后做任务 3

原因：

- 任务 1 成本最低，可以先把语义噪音清掉
- 任务 2 会影响后续抽象方向
- 任务 3 需要建立在对接口边界更清楚的前提上

### 16.4 第一阶段的验收标准

- 文档和代码注释不存在明显漂移
- 插件接口有统一 `context` 设计方案
- 节点状态契约有可审阅草案
- 明确记录“哪些能力继续延期”

对应实施计划：

- `docs/superpowers/plans/2026-03-21-architecture-remediation-runbook.md`

---

## 17. 各 Stage 执行策略

### 17.1 Stage 1：先收紧契约

目标：

- 统一语言
- 统一接口基础设施
- 统一状态输入契约

适合启动条件：

- 团队已接受“先修契约，不先补功能”的拍板结论

不做的事：

- 不上新的 LLM 策略
- 不重做 registry
- 不引入共享状态服务

### 17.2 Stage 2：再优化决策抽象

目标：

- 让失败语义更清晰
- 让策略职责更明确
- 让亲和能力具备正式边界

适合启动条件：

- Stage 1 已完成
- `context` 传播方案已稳定
- 状态契约草案已定稿

不做的事：

- 不引入反馈闭环实现
- 不强推控制面

### 17.3 Stage 3：最后考虑高级能力

目标：

- 评估并引入反馈闭环边界
- 评估 registry 演进
- 评估是否需要更强的状态感知路由内核

适合启动条件：

- Stage 2 完成后，团队确认当前 SDK 边界仍不足以承载演进需求

不做的事：

- 不直接越级做完整网关

---

## 18. 最终讨论版摘要

如果只保留最重要的三句话，本轮评审的摘要是：

1. 当前项目的边界没有问题，问题在于边界契约还不够强。
2. 当前最值得优先处理的，不是增加更多策略，而是统一 `context`、增强状态契约、让失败语义更清楚。
3. 应该先走“轻量收敛”路线，而不是马上把项目做成更重的网关或控制面。

---

## 19. 本轮验证

- 本地测试：
  - `go test ./...`
  - 结果：通过
- 工作区状态：
  - 在写入本评审文档前，业务代码工作区干净

---

## 20. 下一步建议

下一轮可以继续往两个方向收敛：

### 20.1 如果继续做研究

把当前问题进一步压缩成：

- 必须修
- 建议重构
- 明确延期

并为每一项补：

- 收益
- 风险
- 最小落地方式

### 20.2 如果准备进入设计或改造

建议先只做第一阶段 Runbook：

- 修正文档与注释漂移
- 设计统一插件 `context` 接口
- 设计更强的 `NodeSnapshot` 契约草案

这三项完成后，再决定是否推进：

- fallback 原因化
- policy 拆层
- affinity store 抽象

---

## 21. 待继续研究的问题

1. `context` 统一后，是否需要同时引入 typed plugin config，还是先只改调用签名？
2. `NodeSnapshot` 的增强字段，哪些应该属于核心类型，哪些应该放到 `Metadata`？
3. 对当前仓库来说，`fallback` 更适合做“失败原因 -> 退化路径”的映射，还是“失败原因 -> 动作集合”的映射？
4. `AffinityStore` 是否应该只为 LLM decode 开放，还是做成通用能力？
