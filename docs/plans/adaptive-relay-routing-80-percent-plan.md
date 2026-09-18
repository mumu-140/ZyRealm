# Octopus 自适应中转路由计划：先解决 80% 的真实故障

> Historical snapshot: this document preserves the original design baseline. For current execution status and deferred items, use `docs/plans/README.md`; baseline-specific “当前/下一步” wording below is not live status.

状态：**设计基线 / 尚未实现**  
主题分支：`codex/adaptive-relay-routing-plan`  
基线主线：`f0617d51dccab76dc0dec068956dfb852b5996b4`  
故障证据：[`vps-infra@42dcb851/reports/octopus-failures/2026-09-13`](https://github.com/mumu-140/vps-infra/tree/42dcb851073560c318351b3b4e0a38d7d755350e/reports/octopus-failures/2026-09-13)

## 1. 目标

本轮不追求建立一个能解释所有供应商错误的完美分类器，而是针对真实中转站环境建立一个**简单、可恢复、能快速切换的自适应路由闭环**，优先解决约 80% 的实际失败与延迟放大问题。

实际中转站具有明显的时间变化：

- 上午可用、下午不可用，之后又恢复；
- 当天额度耗尽，次日或一段时间后恢复；
- 模型或账号池不定时开放、关闭；
- Cloudflare/WAF、DNS、TLS、连接和上游 5xx 往往是临时故障；
- 同一个 Channel 下可能有多个 Key，其中单个 Key 失效不代表整个站失效；
- 部分中转站明确禁止使用 `hello`、`你好`、`ping` 等探活内容，主动探测可能导致封号。

因此，本轮核心不是继续增加负载均衡模式，而是补齐：

1. 粗粒度失败判决；
2. `RetryDirective` 与运行态处罚分离；
3. Provider / Provider×Model / Credential 三层可恢复 cooldown；
4. 请求内立即跳过已确认失败的 Provider；
5. replay safety 与 downstream commit 守卫；
6. 有上限的重试预算；
7. 默认**无合成请求的被动恢复**。

## 2. 真实故障数据给出的优先级

2026-09-13 故障库覆盖 14,288 次请求，其中 2,430 次最终失败、11,858 次最终成功；另有 1,176 次最终成功请求曾经经历 failed 或本地 skip attempt。这说明单次 attempt 失败不能直接等价为永久坏站。

最终失败中：

- `context canceled`：1,105 次，45.47%；
- first-token timeout：922 次，37.94%；
- 两者合计 2,027 / 2,430，约 **83.4%**。

尝试级的高频问题还包括：

- 上游不可用：2,164；
- 限流/配额：1,290；
- Cloudflare/WAF：626；
- 连接/网络：551；
- 鉴权：386；
- 流中断/空流：214。

因此 P0 不是覆盖 100 多种错误字符串，而是让少数高频故障稳定地产生正确的**切换动作**。

## 3. 不做什么

本轮明确不做以下事情：

- 不构建几十种供应商专用错误枚举；
- 不尝试从所有中文/英文错误文本中推断精确根因；
- 不因为一次 CF、5xx 或 timeout 永久 blacklist 一个 Channel；
- 不默认发送 `ping`、`hello`、`你好` 或任何模型合成请求进行探活；
- 不新增数据库迁移；第一阶段运行态保持内存态；
- 不重写现有 Provider→Credential 两级数据模型；
- 不把 GPT-Load 的扁平 credential-centric provider 权重模型搬入 Octopus；
- 不把内容安全错误当作渠道健康错误；
- 不在已经向客户端提交流式 payload 后自动跨 Provider replay。

现有 outlier window / health score 可以保留，用于慢速观察、排序和控制面信息，但**实时路由准入不再依赖“分数足够低才停用”**。

## 4. 总体架构

```text
Request
   |
   v
Runtime Eligibility Gate
   |-- Provider cooldown
   |-- Provider x Model cooldown
   |-- Credential cooldown / failure
   |-- request-local skipped providers
   v
Provider Balancer
   v
Credential Selection
   v
Attempt
   v
Coarse Failure Classifier
   |
   +--> RetryDirective
   |
   +--> RuntimeEffect
   |
   +--> Replay / commit guard
   v
Next eligible candidate or terminate
```

关键顺序是：**先 eligibility，再 balancing**。坏站应暂时离开候选集合，而不是仅仅降低 health score 后仍被反复选中。

## 5. 只保留 5 类路由故障

### 5.1 REQUEST / CLIENT

典型情况：

- 外层客户端 request context 已明确 canceled；
- 明确的请求体/参数错误，且错误属于客户端请求本身；
- 已确认的内容策略/敏感词拦截。

动作：

```text
RetryDirective = NONE
RuntimeEffect  = NONE
```

不处罚 Channel，不污染 Provider health。

内容策略错误默认终止，避免在多个 Provider 上无限绕行。以后若产品层明确允许内容策略跨站 fallback，再单独设计；本轮不做。

### 5.2 CREDENTIAL

典型情况：

- `Invalid API key` / `Invalid token`；
- `API_KEY_DISABLED` / key inactive / key 已失效；
- 明确落在单账号或单凭据上的余额/额度不足；
- 可刷新订阅凭据要求 refresh / reauthorization。

动作：

```text
RetryDirective = NEXT_CREDENTIAL
RuntimeEffect  = COOLDOWN_CREDENTIAL / RECORD_CREDENTIAL_FAILURE
```

同一 Provider 最多尝试有限个备用 Credential；可用 Credential 耗尽后再 `NEXT_PROVIDER`。

401/403/402 **不能仅凭 HTTP status** 判断为 Credential。应优先识别结构化错误 code/message；CF HTML、WAF、模型不支持等不得误伤 Key。

### 5.3 MODEL / CAPACITY

典型情况：

- 429；
- concurrency/RPM/上游负载饱和；
- `model_not_found` / model unavailable；
- `no available channel`；
- `no active accounts available`；
- 某个模型当前没有可服务账号。

动作：

```text
RetryDirective = NEXT_PROVIDER
RuntimeEffect  = COOLDOWN_PROVIDER_MODEL
```

若响应存在可信 `Retry-After`、reset time、`earliest recover at ...`，优先使用；否则使用短默认 cooldown。

不在当前 Provider 上原地 sleep + retry，只要还有其他 eligible Provider 就立即切换。

### 5.4 PROVIDER TRANSIENT

典型情况：

- 502 / 503 / 504 / Cloudflare 52x / 530；
- Cloudflare/WAF HTML；
- DNS lookup failure；
- connection refused / reset / broken pipe；
- proxy connection refused；
- TLS handshake timeout；
- dial / I/O timeout；
- first-token timeout；
- upstream 返回维护 HTML、非预期 HTML；
- 流在**尚未向客户端转发任何 payload**前结束；
- 明显的 provider-side malformed/incomplete response。

动作：

```text
RetryDirective = NEXT_PROVIDER
RuntimeEffect  = SKIP_PROVIDER_FOR_REQUEST
                 + short cooldown
```

全局 cooldown scope：

- 网络、DNS、TLS、CF/WAF、502/503/504：优先 `PROVIDER`；
- first-token timeout、模型侧 overload：优先 `PROVIDER_MODEL`；
- 第一版不做复杂的跨模型故障聚合升级。

推荐递进 cooldown：

```text
1st transient failure   5s
2nd repeated failure   15s
3rd repeated failure   60s
continued failure       5m
```

成功请求降低/清零 failure streak。具体时间应集中为配置常量，不散落在 Handler 中。

原则：**快速绕开，谨慎定罪，允许快速恢复。**

### 5.5 AMBIGUOUS TRANSPORT

主要解决：

```text
failed to send request: Post "...": context canceled
```

第一版不需要精确推断是 transport、proxy、内部 timeout 还是某个 adapter 的 cancel，只需要先判断：

1. 外层客户端 context 是否真的已经 canceled；
2. 上游请求是否确定未发送 / 可能已发送；
3. 是否已经向下游提交 payload。

规则：

```text
outer client ctx canceled
    -> CLIENT_CANCEL
    -> terminate, no provider penalty

outer client ctx alive
+ outbound attempt returns context.Canceled
    -> treat as upstream/transport candidate
    -> apply replay guard
```

这也是当前实现必须优先修正的逻辑：不能仅因为 error chain 中存在 `context.Canceled` 就认定“客户端主动取消”。

## 6. 直接借用 GPT-Load 的成熟设计

参考：[`tbphp/gpt-load`](https://github.com/tbphp/gpt-load)，本计划形成时核对的实现包括：

- `internal/health/decision.go`
- `internal/health/execution_judge.go`
- `internal/scheduler/scheduler.go`
- `internal/gateway/handler.go`

### 6.1 RetryDirective 与 RuntimeEffect 分离

GPT-Load 已经把“接下来重试谁”与“对当前运行态做什么”拆开，这是正确抽象。Octopus 直接采用同一思想，但保留 Provider→Credential 的分层架构。

建议 Octopus 定义：

```text
RetryDirective
- NONE
- SAME_CREDENTIAL          # 仅极少数安全场景
- NEXT_CREDENTIAL
- NEXT_PROVIDER
- REFRESH_CREDENTIAL

RuntimeEffect
- NONE
- COOLDOWN_CREDENTIAL
- COOLDOWN_PROVIDER_MODEL
- COOLDOWN_PROVIDER
- RECORD_CREDENTIAL_FAILURE
- SKIP_PROVIDER_FOR_REQUEST
```

一个判决可同时包含 Retry 与 Effect，例如：

```text
503 / CF 522
Retry  = NEXT_PROVIDER
Effect = SKIP_PROVIDER_FOR_REQUEST + COOLDOWN_PROVIDER

429 model capacity
Retry  = NEXT_PROVIDER
Effect = COOLDOWN_PROVIDER_MODEL

invalid key
Retry  = NEXT_CREDENTIAL
Effect = RECORD_CREDENTIAL_FAILURE
```

### 6.2 请求内 SkipGroup / SkipProvider

GPT-Load iterator 维护 `tried` credential 与 `skippedGroups`。Group 被判为 host/provider failure 后，同一请求不会再从这个 Group 选择其他 credential。

Octopus 应增加等价的 request-local `skippedProviders/channels`：

```text
A/key1 -> CF 522
=> skip A for this request
=> B
```

禁止：

```text
A/key1 -> 522
A/key2 -> 522
A/key3 -> 522
B
```

但 Credential 错误不同：

```text
A/key1 -> invalid key
A/key2 -> success
```

此时不能因为一个坏 Key 跳过整个 A。

### 6.3 Cooldown 在 scheduler 前过滤

GPT-Load 对 credential/model cooldown 是在候选收集阶段直接排除，而不是先选中再看 health。

Octopus 同样采用：

```text
runtime state -> eligibility gate -> balancer
```

而不是：

```text
balancer -> health score -> 尝试后才发现不可用
```

### 6.4 Dispatch / Replay / Commit safety

直接采用 GPT-Load 的核心概念：

```text
DispatchState
- NOT_SENT
- MAYBE_SENT

ReplaySafety
- SAFE / REJECTED_BEFORE_PROCESSING
- UNKNOWN

Downstream state
- NOT_COMMITTED
- COMMITTED
```

不要求第一版完整复制所有 GPT-Load operation policy，但必须有足够信息防止不安全重放。

## 7. 不借用 GPT-Load 的主动 Probe

GPT-Load 的 `OperationProbe` 对 Chat 类接口会真实发送 `ping`，并把输出压到约 1 token。这个机制**不适合当前中转站池**，因为部分中转站会把 `ping` / `hello` / `你好` 等探活请求视为滥用并封禁账号。

Octopus 默认：

```text
probe_mode = passive
```

禁止后台周期性发送合成 LLM 请求。

恢复流程：

```text
AVAILABLE
   |
 failure
   v
COOLDOWN
   |
 cooldown expired
   v
HALF_OPEN / PROBATION
   |
 next real user request (only one concurrent trial)
   +--> success -> AVAILABLE
   +--> failure -> COOLDOWN
```

HALF_OPEN 必须限制并发，例如一个 Provider/Model 同时只放行 1 个真实试探请求，防止 cooldown 到期后流量洪峰一起冲回坏站。

以后可以增加可选模式：

```text
off
passive       # 默认
metadata      # 仅供应商明确允许的 /models、余额等非生成 API
custom        # 管理员明确配置
```

但第一版只需要 `passive`。

## 8. Replay policy：先做 balanced 默认

需要在“可靠拿到结果”和“避免重复请求/双计费”之间取平衡。

### 8.1 永远不 replay

- 外层 client context 已 canceled；
- 已经向客户端提交任何不可撤回的 payload/token；
- Responses continuation / resource mutation 等具有状态语义的操作，除非以后有更强证据；
- 明确的 request/client error。

### 8.2 可安全 failover

- `NOT_SENT`；
- 上游明确 rejected before processing；
- 尚未向客户端提交 payload 且错误明确属于 provider/model capacity。

### 8.3 outcome unknown

对于普通 Chat/Completion 类新生成请求：

- 下游尚未收到 payload；
- 外层 client 仍 alive；
- 上游可能已收到请求，但结果未知；

默认 `balanced` 策略允许**最多一次跨 Provider replay**，接受极低概率的重复上游计费，以优先保证最终得到回答。

以后可暴露：

```text
strict      # outcome unknown 不 replay
balanced    # 默认，普通生成最多跨站 replay 1 次
aggressive  # 以成功率优先，另行设计
```

第一版只实现 `balanced` 所需的最小判断，不提前扩展完整配置 UI。

## 9. 重试预算：防止 attempt 失控

真实日志存在单请求 attempt 29、30、31、32，以及 11、14、18、22 等情况。必须引入 request-level budget，避免一个请求扫遍大量坏候选后才失败。

建议初始默认值：

```text
max_provider_attempts = 4
max_credentials_per_provider = 2
max_unknown_cross_provider_replay = 1
```

再增加一个保险阈值：

```text
max_wire_attempts = 8
```

其中：

- Provider attempt 按唯一 Channel/Provider 计，不因同一 Provider 内协议适配细节无限增加；
- Credential failure 才允许同站换 Key；
- Provider-level failure 立即 skip 当前 Provider，不继续烧 Key；
- first-token timeout / 502 / 503 / CF / DNS / TLS 等，在存在其他 eligible Provider 时默认 **0 次同站 retry**；
- 没有任何其他 Provider 可用时，可以在 replay-safe 前提下允许一次受控的 same-provider retry，避免直接无路可走。

目标不是“重试越多越可靠”，而是**尽快把预算花在独立故障域上**。

## 10. 第一版决策表

| 观察到的失败 | Retry | Effect | Scope | 备注 |
| --- | --- | --- | --- | --- |
| 真 client cancel | NONE | NONE | request | 必须由 outer ctx 证实 |
| 明确 request/parameter 400 | NONE | NONE | request | 不污染健康 |
| 明确 content policy | NONE | NONE | request | 本轮不跨站绕策略 |
| invalid/disabled key | NEXT_CREDENTIAL | RECORD/CRED cooldown | credential | Key 用尽再换站 |
| 明确单账号余额不足 | NEXT_CREDENTIAL | CRED cooldown | credential | 可恢复，不永久拉黑 |
| 429 / concurrency / RPM | NEXT_PROVIDER | cooldown | provider×model | 优先 Retry-After/reset |
| model unavailable / no channel | NEXT_PROVIDER | cooldown | provider×model | 短 cooldown |
| no active accounts | NEXT_PROVIDER | cooldown | provider×model | 有 recover time 则解析 |
| 502/503/504/52x/530 | NEXT_PROVIDER | skip + cooldown | provider | 无同站 retry |
| Cloudflare/WAF HTML | NEXT_PROVIDER | skip + cooldown | provider | 不判 Key 无效 |
| DNS/TLS/connect/proxy error | NEXT_PROVIDER | skip + cooldown | provider | 无同站 retry |
| first-token timeout | NEXT_PROVIDER | skip + cooldown | provider×model | timeout 已消耗大预算 |
| 空流，尚未 forward payload | NEXT_PROVIDER | cooldown | provider×model | replay-safe 才切 |
| 流已向客户端输出后中断 | NONE | health record only | provider×model | 不自动 replay |
| upstream context canceled + client alive | NEXT_PROVIDER* | short cooldown* | provider/model | `*` 受 replay guard 限制 |
| ambiguous 403 | marker-first | conservative | unknown | 不仅凭 403 判 Credential |

## 11. Runtime state：只做最小三层

第一版只维护：

### Provider runtime

```text
cooldown_until
failure_streak
last_failure_reason
half_open_in_flight
```

### Provider × Model runtime

```text
cooldown_until
failure_streak
last_failure_reason
half_open_in_flight
```

### Credential runtime

复用/扩展现有 Key 状态，补足：

```text
cooldown_until
failure_streak
last_failure_reason
```

第一版全部内存态即可；进程重启后重新学习是可接受的。不要为了这个任务引入数据库迁移。

## 12. 与现有 Octopus 的整合位置

预计主要触点：

- `internal/relay/failure_scope.go`
  - 保留粗分类能力，但将“分类”与“动作判决”解耦；
  - 修正 quota/auth/channel scope 过度泛化的问题。
- `internal/relay/retry.go`
  - 从笼统 retryable 转为明确 RetryDirective；
  - same-provider retry 与 next-provider failover 分开。
- `internal/relay/relay_handler.go`
  - 执行 request-level provider budget；
  - 当前 Provider 被 skip 后直接迭代下一 Provider。
- `internal/relay/relay_handler_support.go`
  - Credential 错误时换 Key；Provider 错误不再继续换同站 Key。
- `internal/relay/balancer/iterator.go`
  - 增加 request-local skipped provider；
  - sticky 只能作用于当前 eligible provider。
- `internal/relay/balancer/circuit.go`
  - 现有 key+model circuit 保留；不要强行承担 provider runtime cooldown 的全部职责。
- `internal/relay/first_token_timeout.go`
  - timeout 结果进入统一 Decision Engine。
- 新增建议：`internal/relay/availability/` 或等价小包
  - Provider / Provider×Model runtime cooldown；
  - passive half-open gate；
  - 不包含网络 IO，不做主动探测。

具体文件名可以在实现时根据现有 package 边界调整；原则是不要把所有逻辑继续堆入 `relay_handler.go`。

## 13. Sticky / Session affinity 规则

Sticky 只能是偏好，不能突破 eligibility。

```text
sticky provider healthy/eligible
    -> 可以优先

sticky provider cooling / skipped / model cooling
    -> 忽略 sticky
    -> 选择下一 eligible provider
```

Provider-level failure 后应使本请求不再恢复到该 sticky Provider。是否删除长期 sticky 可按现有 TTL 行为处理，第一版不需要复杂迁移。

## 14. 最小观测字段

为了能继续调规则，而不是依赖字符串猜测，每个 attempt 至少记录：

```text
failure_class
failure_scope
retry_directive
runtime_effect
rule_id
client_context_canceled
 dispatch_state
response_started
downstream_committed
replay_decision
provider_cooldown_until
model_cooldown_until
credential_cooldown_until
provider_attempt_index
wire_attempt_index
```

其中 `rule_id` 应稳定、短小，例如：

```text
client.cancel
transport.context_canceled_client_alive
transport.dns
transport.tls
provider.http_5xx
provider.cloudflare
capacity.rate_limit
capacity.model_unavailable
credential.invalid
credential.quota
stream.empty_before_commit
stream.failed_after_commit
```

不要求第一版把所有供应商文本转成独立 rule。

## 15. 实现阶段

### Phase 1 — Decision contract + P0 cancel 修复

1. 定义 RetryDirective / RuntimeEffect / coarse failure class；
2. 引入 dispatch / downstream commit 最小证据；
3. 修正 `context.Canceled`：只有 outer client ctx 真的 canceled 才归 CLIENT_CANCEL；
4. 补充判决单测，不改变生产配置。

### Phase 2 — request-local skip + retry budget

1. Iterator 增加 skipped Provider；
2. Provider-level failure 后不再尝试同站其他 Key；
3. 增加 provider / credential / wire attempt budget；
4. sticky 不得越过 eligibility。

### Phase 3 — runtime cooldown + passive recovery

1. Provider / Provider×Model runtime state；
2. 5s → 15s → 60s → 5m 的有限递进 cooldown；
3. cooldown 到期进入 passive HALF_OPEN；
4. HALF_OPEN 单并发真实业务试用；
5. success 恢复 AVAILABLE。

### Phase 4 — 高频错误映射

只覆盖故障库高频模式：

- first-token timeout；
- `context canceled`；
- 429 / concurrency / RPM；
- 502/503/504/52x/530；
- CF/WAF HTML；
- DNS/TLS/connect/proxy；
- invalid key / disabled key；
- explicit balance/quota；
- model unavailable / no available channel / no active accounts；
- empty stream before commit。

达到覆盖目标后停止扩枚举，不为了低频长尾继续堆规则。

## 16. 必须覆盖的测试场景

### 路由与恢复

1. 高优先级 A 返回 503，健康低优先级 B：当前请求立即切 B；cooldown 内后续请求不再撞 A。
2. A 发生 CF 522/530/HTML：不换 A 的其他 Key，直接 B。
3. A/key1 invalid，A/key2 正常：只淘汰 key1，允许 key2 成功。
4. A/key1、key2 都无余额：A credentials 耗尽后切 B。
5. A/modelX 429：modelX cooldown，但 A/modelY 不被无条件全站污染。
6. cooldown 到期：只放一个真实请求 HALF_OPEN；成功恢复，失败重新 cooldown。
7. sticky 指向 cooling Provider：必须忽略 sticky。

### context canceled / replay

8. outer client ctx 已 canceled：不 retry、不 cooldown Provider。
9. outer ctx alive，但 outbound 返回 `context canceled`，且 NOT_SENT：切 B。
10. outer ctx alive、MAYBE_SENT、未 downstream commit：普通 Chat 在 balanced policy 下最多跨站 replay 一次。
11. downstream 已输出 token 后中断：不跨站 replay。

### retry budget

12. 多个坏 Provider：最多尝试 4 个 Provider 后结束。
13. 单 Provider 有大量 Key：Provider-level 503 不得逐 Key 扫描。
14. Credential error：每 Provider 最多尝试配置数量的 Key。
15. 无其他 Provider：replay-safe 场景允许受控 same-provider retry，不形成无限循环。

### 错误归因

16. 403 Cloudflare HTML 不得标记 invalid key。
17. 401 文本明确 `model ... is not supported` 不得淘汰 Credential。
18. 402/403 明确 `insufficient balance/quota` 按 credential/capacity marker 处理，而非单看 status。
19. `sensitive_words_detected` 即使包装成 HTTP 500，也不得作为 provider 5xx health failure。
20. 维护 HTML / 非 JSON 响应视为 provider transient，而不是 transformer 永久错误。

## 17. 验收标准

第一阶段不要求把所有历史失败消灭，满足以下条件即可认为设计目标达成：

1. P0：`context canceled` 不再被无条件当作 client cancel；
2. P0：first-token timeout、5xx、CF、DNS/TLS/connect 在有替代 Provider 时立即 failover；
3. Provider-level failure 不再逐 Key 重试；Credential-level failure不会误伤整个 Provider；
4. cooldown Provider 在 scheduler 前被排除；
5. cooldown 自动通过 passive half-open 恢复，不发送合成 probe；
6. sticky 不绕过 cooldown；
7. 单请求存在明确 provider/wire attempt budget；
8. downstream 已提交后不会被自动 replay；
9. 故障库中的高频模式都有测试样例覆盖；
10. 不引入数据库迁移，不修改生产配置，不扩大为一次全面重构。

## 18. 验证与交付边界

按仓库 `AGENTS.md`：

- 本地开发工作站不运行 Go/前端构建测试；
- 实现阶段 Relay 相关测试和 `go test -buildvcs=false ./...` 只在 GitHub CI 或 fwq57ys 固定版本容器执行；
- 本计划本身不授权生产部署、容器生命周期操作或 SQLite 写入；
- 代码完成、合并 main、Release、生产部署继续视为四个独立阶段。

## 19. 当前结论

本轮的核心判断是：**Octopus 不缺更多 balancer 模式，缺的是失败反馈到下一次选路之间的闭环。**

最小有效方案不是一个复杂的“AI 错误分类器”，而是：

```text
5 类粗错误
+ Retry 与 Effect 分离
+ 三层 cooldown
+ request-local skip
+ replay/commit guard
+ 有限 retry budget
+ passive half-open
```

先用这套机制处理真实日志中占主导的 `context canceled`、first-token timeout、5xx/CF、网络、429/容量与 Credential 故障；长尾错误只有在真实数据证明有必要时再增加规则。