# Octopus 自适应路由最终评审：对照 GPT-Load 与真实故障库

状态：**最终设计补充 / 覆盖原计划中冲突项**  
基线：`main@f0617d51dccab76dc0dec068956dfb852b5996b4`  
原计划：`docs/plans/adaptive-relay-routing-80-percent-plan.md`  
故障证据：`mumu-140/vps-infra@42dcb851073560c318351b3b4e0a38d7d755350e/reports/octopus-failures/2026-09-13`  
GPT-Load 对照：`tbphp/gpt-load@main`，评审时对应代码搜索结果约 `426bde9a385a1586c3a988bb2881a5a761936aaf`

> 本文只收敛最终路由策略。若与原 80% plan 冲突，以本文为准。

## 1. 最终结论

Octopus 不应复制 GPT-Load 的整体调度模型，而应保留自己的 **Provider -> Credential 两级架构**，只吸收 GPT-Load 已经验证成熟的失败闭环：

```text
Request
  -> classify operation / replay safety
  -> runtime eligibility
       -> Provider state
       -> Provider x Model state
       -> Credential state
       -> request-local skip set
  -> Provider scheduler
  -> sticky preference among eligible AVAILABLE candidates only
  -> Credential scheduler
  -> wire attempt
  -> failure decision
       -> RetryDirective
       -> RuntimeEffect
       -> replay/commit guard
  -> next candidate / terminate
```

核心原则：

1. **eligibility before balancing**；
2. **failure domain before HTTP status**；
3. **request-local fast failover before long-horizon health scoring**；
4. **Provider failure 不逐 Key 扫描，Credential failure 不误伤整个 Provider**；
5. **recoverable cooldown，不做永久定罪**；
6. **不发送合成 LLM probe**；
7. **sticky 只能影响健康候选的顺序，不能绕过 eligibility**；
8. **already committed / unsafe replay 永不自动跨站重放**。

## 2. Octopus 与 GPT-Load 的路由差异

| 维度 | Octopus 当前 | GPT-Load 当前 | 最终采用 |
| --- | --- | --- | --- |
| 调度单元 | Provider/GroupItem 后再选 Key | 跨 Group 的 Credential 候选池 | 保留 Octopus 两级结构 |
| Provider 权重 | GroupItem 权重直接决定 Provider 份额 | group weight x credential weight，多个 credential 会增加组总候选权重 | 保留 Octopus Provider 权重，避免 key 数量改变 Provider 份额 |
| 策略 | RR/Random/Failover/Weighted/HealthFirst/LeastUsed/P2C 等 | weighted fair + route tier | 保留 Octopus 多策略；在策略前统一 eligibility gate |
| 请求内失败隔离 | 主要靠外层遍历和 key circuit | `tried` credential + `SkipGroup` | 借用 `SkipGroup` 思想，增加 `skippedProviders` |
| 长期运行态 | key+model circuit、outlier score | credential cooldown、model cooldown、blacklist、quota/reset | 建 Provider / Provider×Model / Credential 三层 runtime state |
| 失败判决 | status/字符串 -> health scope，retry 与处罚耦合较多 | `RetryDirective` 与 `Effect` 独立 | 直接借用该抽象 |
| replay safety | 已有 Responses replay/fallback 逻辑，但普通失败路由缺统一 dispatch/commit 判决 | 明确 DispatchState、ReplaySafety、DownstreamCommitted | 借用统一 replay/commit guard |
| sticky | 可在 balancer 排序后把 Channel 移到最前 | preferred credential 仍受候选准入过滤 | sticky 移到 eligibility 后，只能重排 AVAILABLE 候选 |
| 探活 | 当前无统一恢复探活闭环 | 存在真实 `ping`/1-token probe | 不采用；默认 passive half-open |

### 为什么不复制 GPT-Load 的扁平 Credential 调度

Octopus 的 Provider 权重是用户明确配置的故障域权重。若 A、B 两个 Provider 都权重 1，而 A 有 10 个 Key、B 只有 1 个 Key，扁平化 credential 调度可能让 A 获得远高于 B 的聚合流量。

因此最终模型是：

```text
Provider scheduler decides provider share
    -> credential scheduler only balances keys inside that provider
```

如果以后要借用 GPT-Load 的 weighted-fair 算法，应只放到 **单 Provider 内部的 Key 池**，不能跨 Provider 扁平化。

## 3. 真实故障库对策略的约束

现存 14,288 请求中最终失败 2,430；其中：

- `context canceled`：1,105，45.47%；
- first-token timeout：922，37.94%；
- 两者合计约 83.4%。

尝试级高频错误还包括：

- 上游不可用 2,164；
- 限流/配额 1,290；
- first-token timeout 988；
- context canceled 981；
- 协议/请求参数 757；
- Cloudflare/WAF 626；
- 网络/连接 551；
- 鉴权 386；
- 流中断/空流 214。

同时，1,176 个最终成功请求曾经历 failed 或 local skip attempt，因此任何“单次失败 = 永久坏站”的方案都会过度处罚。

真实日志还出现单请求 attempt 29、30、31、32，以及 11、14、18、22，说明当前的主要问题不只是“能否 failover”，而是 **failover 太晚、重复尝试同一故障域、缺少请求级预算**。

## 4. 最终错误判决顺序：marker-first，而不是 status-first

真实样本中存在：

```text
HTTP 401
message: Model claude-opus-5-low is not supported
error type: authentication_error
code: invalid_api_key
```

如果按 HTTP 401 或 `invalid_api_key` 先判，会错误淘汰 Key；实际根因是当前 Provider/Model 组合不支持该模型。

因此第一版必须固定判决优先级：

```text
1. outer client cancellation / downstream cancellation
2. downstream committed / replay safety
3. explicit content-policy / request-invalid markers
4. explicit model/capability markers
5. explicit credential/auth/quota markers
6. explicit provider-capacity / recover-at / Retry-After markers
7. CF/WAF/HTML/DNS/TLS/connect/transport markers
8. HTTP status fallback
9. ambiguous fallback
```

不能用 `401/402/403 => credential`、`500 => provider` 作为第一判断。

## 5. 最终失败类别与动作

### 5.1 CLIENT / REQUEST

包括：

- 外层客户端 context 确实 canceled/deadline exceeded；
- 明确 invalid request 且与 Provider 能力无关；
- 明确内容策略错误。

动作：

```text
Retry = NONE
Effect = NONE
Health penalty = NONE
```

内容策略不自动跨站绕行；它是产品/合规语义，不属于可靠性 failover。

### 5.2 CREDENTIAL

包括：

- 明确 invalid token / invalid key；
- 单 Key 的 401/403 鉴权失败；
- 明确单账号余额不足；
- 402 `Insufficient account balance`；
- token refresh / reauthorization required。

动作：

```text
Retry = NEXT_CREDENTIAL
Effect = credential cooldown / auth failure
```

同 Provider 内只在 **Credential failure** 时换 Key；默认最多 2 个 Key。

如果所有 Key 都不可用，该 Provider 才对当前请求变为 unavailable。

Quota/balance 必须可恢复：

- 有 reset/recover time -> 冷却到该时间；
- 无 reset time -> 递进式 credential cooldown，不永久 disable；
- 可刷新订阅凭据走 refresh 流程，不把 Provider 判坏。

### 5.3 MODEL / CAPACITY / CAPABILITY

包括：

- 429 group RPM / concurrency / upstream saturated；
- `model_not_found`；
- `No available channel for model ...`；
- `no available channels after filtering ... model=...`；
- `no active accounts available ... earliest recover at ...`；
- model not supported；
- `effort_not_supported`；
- provider-specific `unsupported parameter`；
- routing group / protocol capability mismatch。

动作分两种：

#### 临时容量类

```text
Retry = NEXT_PROVIDER
Effect = COOLDOWN_PROVIDER_MODEL
```

`Retry-After`、reset time、`earliest recover at` 优先于默认 cooldown。

#### 能力不匹配类

```text
Retry = NEXT_PROVIDER
Effect = NEGATIVE_CACHE_PROVIDER_MODEL_CAPABILITY
Health penalty = NONE
```

能力错误不是健康错误。建议第一版负缓存 30 分钟，成功或配置变更可提前清除；不能永久记死，因为中转站模型映射会变化。

这是原 plan 的一个重要补充：真实 401 + model-not-supported 和多种 400 capability mismatch 必须从 credential/client-error 中剥离，否则会重复扫 Key 或直接终止。

### 5.4 PROVIDER TRANSIENT

包括：

- 502/503/504/52x/530；
- CF/WAF/HTML；
- DNS、TLS、connection reset/refused、broken pipe；
- proxy/connect timeout；
- `do_request_failed`；
- 返回 HTML 导致 JSON `<` 解析错误；
- 明确 provider-side malformed/incomplete response。

动作：

```text
Retry = NEXT_PROVIDER
Request effect = SKIP_PROVIDER_FOR_REQUEST
Runtime effect = short PROVIDER cooldown
Same-provider retry = 0 when another eligible provider exists
```

递进 cooldown 建议：

```text
first hard provider failure      5s
second within short window      15s
third                           60s
continued                       5m
```

成功后快速衰减 failure streak，避免站点恢复后长时间回不来。

### 5.5 FIRST-TOKEN TIMEOUT

first-token timeout 在实际最终失败中占 37.94%，必须独立对待：

```text
Retry = NEXT_PROVIDER
Request effect = skip current provider for this request
Runtime scope = Provider x Model
Same-provider retry = 0 when alternative exists
```

它不应直接惩罚 Provider 的所有模型。

建议 cooldown：

```text
1st timeout       15s
2nd/short window  60s
repeated          5m
```

如果当前请求已经消耗一次完整 30s first-token budget，不再在同站 sleep/backoff 后重试；重试预算应花在独立 Provider 上。

### 5.6 AMBIGUOUS `context canceled`

真实样本大量出现：

```text
failed to send request: Post "https://...": context canceled
```

Octopus 当前 `isClientCancellation(ctx, err)` 只要 `errors.Is(err, context.Canceled)` 就会判为 client cancellation，即使外层 client request context 仍然活着。这会直接抑制后续 failover。

最终规则：

```text
outer client ctx canceled/deadline
    -> CLIENT_CANCEL
    -> terminate
    -> no provider penalty

outer client ctx alive
+ outbound error wraps context.Canceled/context.DeadlineExceeded
    -> AMBIGUOUS_TRANSPORT
    -> replay guard
    -> request-local NEXT_PROVIDER
```

但与原 plan 相比，**单个 ambiguous cancel 不立即做全局 Provider cooldown**。原因是它可能来自内部 child context、local budget、proxy cancellation 或 adapter，而不一定是 Provider 本身。

第一版动作：

```text
Retry = NEXT_PROVIDER if replay-safe
Request effect = SKIP_PROVIDER_FOR_REQUEST
Runtime effect = soft SUSPECT evidence only
```

只有满足下列之一才升级全局 cooldown：

- 同 Provider/Model 在短窗口内从不同请求出现 >=2 次 ambiguous cancel；
- 同期还有 502/503/CF/DNS/TLS/first-token-timeout 等独立 provider-side 证据。

这可以同时修复 45.47% 的主故障类别，又避免因为本地 cancellation bug 把健康中转站大面积冷却。

## 6. Runtime 状态：增加 SUSPECT，不只 HEALTHY/COOLDOWN

建议：

```go
type RuntimeState uint8

const (
    RuntimeAvailable RuntimeState = iota
    RuntimeSuspect
    RuntimeCooldown
    RuntimeHalfOpen
)
```

三层状态键：

```text
Provider:          channelID
Provider x Model:  channelID + upstreamModel
Credential:        channelID + keyID
```

可选能力负缓存：

```text
Capability: channelID + upstreamModel + capabilitySignature
```

状态字段：

```text
state
reason
since
cooldown_until
reset_at
failure_streak
success_streak
last_failure_at
last_success_at
half_open_inflight
```

规则：

- `AVAILABLE`：正常参与调度；
- `SUSPECT`：仍可用，但排在 AVAILABLE 后；
- `COOLDOWN`：scheduler 前直接过滤；
- `HALF_OPEN`：只允许 1 个真实用户请求试探；成功恢复，失败重新 cooldown。

## 7. Provider 排序的最终语义

不要简单把现有 Failover 改成“health 永远覆盖 Priority”，否则会破坏用户配置的主备语义。

最终顺序：

```text
1. static compatibility
2. runtime eligibility
3. runtime tier: AVAILABLE > SUSPECT > HALF_OPEN lease > COOLDOWN excluded
4. configured balancer semantics
5. sticky preference inside the same AVAILABLE tier only
```

对于 Failover：

```text
eligible AVAILABLE candidates:
    Priority ASC
    -> health score DESC inside same priority
```

Provider 一旦发生明确 hard transient，会立即进入短 cooldown，因此低优先级健康 Provider 可以自然接管，无需让 health 永久覆盖 Priority。

对于 Weighted / RR / P2C / LeastUsed：先剔除 cooldown，再运行原算法。

## 8. Sticky 的最终约束

当前 sticky 在 balancer 之后把指定 Channel 移到首位。最终必须改为：

```text
eligibility -> runtime tier -> sticky -> balancer tie/preference
```

或者等价地：sticky 只在已经确认 `AVAILABLE` 的候选中生效。

规则：

- Provider cooldown -> sticky 无效；
- Provider×Model cooldown -> 当前模型 sticky 无效；
- capability negative cache -> 当前模型 sticky 无效；
- Credential failure -> 可以保留 Provider affinity，但清掉坏 Key affinity；
- Provider hard failure -> 清当前 Provider sticky；
- Responses continuation/resource operation 按已有 replay/session 约束处理，不强行跨站。

## 9. Retry / failover 最终预算

保留原 plan 的上限：

```text
max_provider_attempts = 4
max_credentials_per_provider = 2
max_unknown_cross_provider_replay = 1
max_wire_attempts = 8
```

但增加两个语义约束：

1. `max_credentials_per_provider` 只在 credential-class failure 消耗；provider/model failure 不逐 Key 重试；
2. protocol fallback 与 same-provider retry 也必须计入 `max_wire_attempts`。

默认：

```text
provider/model failure + other provider available
    => same-provider retry = 0

credential failure
    => next credential, max 2

all other providers unavailable
    => only replay-safe requests may do one controlled same-provider retry
```

暂不新增统一的绝对 wall-clock 路由 deadline。先修复 context-cancel causality 并增加 phase timing，再依据数据设置总时间预算；否则可能把新的本地 deadline 继续伪装成 `context canceled`。

## 10. 429 / 402 / quota 的最终区分

不能把“限流/配额”全部放在一个 scope：

| 真实模式 | Scope | Action |
| --- | --- | --- |
| `429 group requests-per-minute limit exceeded` | Provider×Model | cooldown model + next provider |
| `429 concurrency limit exceeded for account` | 优先 Credential；证据不足时 Provider×Model | rotate key / next provider |
| `429 upstream load saturated` | Provider×Model | short cooldown + next provider |
| `402 Insufficient account balance` | Credential first | cooldown key/account；再换 key |
| `no active accounts ... earliest recover at ...` | Provider×Model | cooldown until parsed recover time |
| `No available accounts` | Provider×Model | short/adaptive cooldown |

只有当所有 Key 都出现 credential quota/auth 不可用时，才在当前请求内把整个 Provider 判 unavailable。

## 11. 400 / 401 / 403 不能按状态码直接处理

实际错误库已证明状态码语义不可信：

- 401 可以实际表示 `Model ... is not supported`；
- 500 可以包装内容策略；
- 400 可以是 Provider-specific unsupported parameter；
- 403 可能是 WAF、账号、内容策略或未知 blocked。

因此：

```text
structured code/message marker > transport evidence > HTTP status fallback
```

HTTP status 仅在没有更强证据时兜底。

## 12. Outlier window 与 Circuit Breaker 的角色调整

现有 outlier 默认：

```text
MinSamples = 8
FailRate = 0.85
ConsecutiveFails = 10
TimeWindow = 10m
```

这适合慢速 POR/退役判断，不适合实时路由准入。一个 30%-50% 抖动的中转站可以长期达不到 85% 失败率，却持续制造大量用户失败和延迟。

最终角色：

```text
Runtime cooldown = 快速数据面准入
Circuit breaker = key/model 局部保护
Outlier window = 慢速观测、排序、POR/控制面
```

不要通过单纯调低 outlier 阈值来替代 runtime state machine。

## 13. Passive half-open，明确不复制 GPT-Load probe

GPT-Load 当前 Chat probe 会真实生成 `ping` 并限制输出 token。该机制对本项目的中转站有封号风险，因此不采用。

默认恢复：

```text
COOLDOWN
  -> deadline reached
  -> HALF_OPEN
  -> acquire single-flight probe lease
  -> next real user request
       success -> AVAILABLE
       failure -> cooldown escalation
```

如果所有 Provider 都在 cooldown：

- 不忽略 cooldown 盲目重新扫一遍；
- 有已到期候选时，只给一个 HALF_OPEN lease；
- 都未到期时快速失败，返回可计算的最短 `Retry-After`；
- 不发送后台 synthetic completion。

## 14. Replay safety：比 GPT-Load 略激进，但必须有边界

GPT-Load 对 `DispatchMaybeSent + transport outcome unknown` 非常保守，默认可拒绝 replay。Octopus 面对大量免费/公益/中转站，产品目标更偏向拿到结果，因此 ordinary Chat 可以采用 balanced policy：

```text
outer client ctx alive
AND downstream not committed
AND operation is ordinary read-like chat generation
AND request-level unknown replay count < 1
    => allow one cross-provider replay
```

但以下情况永不自动 replay：

- downstream 已写出 token/payload；
- client 已取消；
- Responses retrieve/delete/cancel 等资源操作；
- continuation/resource ownership 无法安全迁移；
- 明确可能造成不可逆副作用的工具/资源操作。

这部分借 GPT-Load 的安全模型，不照搬其全部保守度。

## 15. 第一版实现优先级

### P0 — 修直接阻断 failover 的问题

1. 修 `isClientCancellation`：只有 outer client request context 确实取消才判 client cancellation；
2. first-token timeout -> immediate next Provider，不做同站 retry；
3. Provider-level 5xx/CF/DNS/TLS/connect -> request-local skip Provider；
4. sticky 不得绕过 runtime eligibility；
5. 加 provider/wire request budget。

### P1 — 建 runtime eligibility

1. Provider runtime state；
2. Provider×Model runtime state；
3. Credential cooldown state；
4. passive half-open single-flight；
5. marker-first failure decision + stable rule_id。

### P2 — 真实错误特化，但仍保持粗粒度

1. `earliest recover at` / Retry-After 解析；
2. 401 model-not-supported 优先于 invalid_api_key；
3. capability negative cache；
4. 402/credential quota 与 provider capacity 分离；
5. HTML/transform `<` 错误归 provider transient；
6. ambiguous cancel 的 SUSPECT 升级规则。

## 16. 必须覆盖的回归测试

1. wrapped `context.Canceled` + outer ctx alive -> 不是 client cancel，可进入 replay/failover；
2. outer ctx canceled -> 立即终止，不处罚 Provider；
3. first-token timeout + B healthy -> 不在 A sleep/retry，直接 B；
4. A 503 -> 同请求不再尝试 A 的其他 Key；
5. A/key1 invalid key -> A/key2 可继续；
6. 402 insufficient balance -> key cooldown，可换 key；
7. 503 `no active accounts ... recover at` -> Provider×Model cooldown 到解析时间；
8. 401 + `Model ... is not supported` + `invalid_api_key` envelope -> capability/model，不淘汰 Key；
9. 400 `effort_not_supported` -> capability negative cache，不污染 health；
10. 400 provider-specific unsupported parameter -> 换 Provider，不判整个客户端请求永久非法；
11. 429 group RPM -> Provider×Model cooldown；
12. CF/HTML/522 -> Provider cooldown；
13. JSON parse `<` from upstream HTML -> Provider transient；
14. downstream 已写 token 后中断 -> 不 replay；
15. explicit content policy -> 不污染 health，不自动跨站绕行；
16. cooling sticky Provider -> sticky 不能把它提到首位；
17. SUSPECT high-priority A + AVAILABLE lower-priority B -> B 优先；
18. cooldown 到期 -> 单个 HALF_OPEN real request，不惊群；
19. Provider-level failure 不逐 Key 扫描；
20. max provider=4 / wire=8 生效；
21. all Provider cooling -> fail fast + shortest Retry-After；
22. healthy candidates 下原 RR/Weighted/P2C/Failover 语义保持不变；
23. provider key 数量变化不改变 Provider configured weight share；
24. capability/config 变更后负缓存可清除。

## 17. 可观测性必须同步补齐

每个 attempt 至少记录：

```text
failure_category
failure_scope
rule_id
retry_directive
runtime_effect
outer_context_state
outbound_context_cause
dispatch_state
downstream_committed
replay_safety
provider_runtime_state
model_runtime_state
credential_runtime_state
cooldown_until
provider_attempt_index
wire_attempt_index
phase_queue_ms
phase_connect_ms
phase_first_byte_ms
```

特别是 `context canceled`，必须区分：

```text
client_cancel
local_budget_cancel
first_token_cancel
outbound_child_cancel
unknown_transport_cancel
```

否则以后仍然只能从字符串猜根因。

## 18. 最终验收标准

实现完成后必须满足：

1. `context canceled` 不再因为 error chain 自身就被误判为客户端取消；
2. first-token timeout、5xx、CF、DNS/TLS/connect 在有替代 Provider 时立即切站；
3. 单个 ambiguous cancel 不误伤全局 Provider，但短期重复会升级；
4. Provider failure 不逐 Key 扫描；
5. Credential failure 可在同站换 Key；
6. 401 model-not-supported 等伪鉴权不会淘汰 Key；
7. cooldown 在 scheduler 前过滤；
8. sticky 不能越过 eligibility；
9. capability mismatch 不污染 health；
10. Provider/Model/Credential 都能自动恢复；
11. 不发送合成 LLM probe；
12. 单请求 provider/wire attempt 有硬上限；
13. downstream commit 后不自动 replay；
14. healthy-only 情况下现有用户配置的 balancer 语义不变；
15. Outlier/POR 保持慢速控制面角色，不承担实时准入；
16. 真实错误库中的高频模式都有固定回归样例。

## 19. 最终一句话架构

Octopus 的最终方向不是“做一个更复杂的负载均衡器”，而是：

> **保留 Provider -> Credential 的可解释两级调度，用 GPT-Load 的 eligibility / retry-effect / replay-safety 思想建立快速闭环，再用真实中转站故障数据增加 Provider、Model、Credential 三层可恢复 runtime state；明确故障立即切站，模糊故障先请求内绕开再累计证据，恢复只用真实流量 half-open。**
