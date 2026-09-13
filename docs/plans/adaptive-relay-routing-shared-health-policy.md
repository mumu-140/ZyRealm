# 自适应路由补充决策：共享健康事实层与现有策略兼容

状态：**最终设计补充 / 属于 adaptive relay routing plan**  
关联文档：`adaptive-relay-routing-final-review.md`  
主题分支：`codex/adaptive-relay-routing-plan`

> 本文固定两个实现决策：健康/运行态信息属于所有路由策略共享的事实层；本轮不新增 `Adaptive` GroupMode，而是在所有现有模式之前增加统一的 runtime eligibility 层。

## 1. 健康信息不是 HealthFirst 私有状态

`Provider`、`Provider × Model`、`Credential` 的运行态和健康证据必须由所有真实请求 attempt 共同写入，与当前 GroupMode 无关。

无论请求来自 RoundRobin、Random、StrictRandom、Failover、Weighted、HealthFirst、LeastUsed 或 P2C，只要发生真实 attempt，都走同一套 failure decision / runtime observation 路径，并按 failure scope 更新共享状态。

共享事实至少包括：

```text
Provider runtime
- state
- reason
- cooldown_until
- failure_streak
- success_streak
- last_failure_at
- last_success_at
- half_open_inflight

Provider × Model runtime
- state
- reason
- cooldown_until / reset_at
- first-token timeout evidence
- rate/capacity evidence
- ambiguous-cancel evidence
- recent success/failure observations

Credential runtime
- auth/quota state
- cooldown_until / reset_at
- failure count
- last success/failure

Capability negative cache (deferred)
- channelID
- upstreamModel
- capabilitySignature
- expires_at
```

`HealthFirst` 只是这些共享事实的一个更强消费者，不是数据拥有者。

## 2. 写入规则：所有策略一致

路由模式不能决定是否记录健康信息。写入由 attempt 结果和 failure scope 决定。

```text
Weighted -> first-token timeout
    => Provider×Model cooldown evidence

RoundRobin -> CF 522
    => Provider transient evidence

P2C -> 429/model capacity
    => Provider×Model cooldown evidence

Failover -> invalid key
    => Credential evidence

HealthFirst -> success
    => 相同的 success evidence
```

因此应该是：

```text
attempt result
  -> common classifier
  -> common runtime updater
  -> shared state
```

其中 model/capacity evidence 的写入不依赖“当前是否存在另一个 Provider”。即使组内只有一个 Provider，真实 429/capacity 失败也必须让该 `Provider × Model` 进入 cooldown；是否能立即跨 Provider failover 是另一个独立决策。

## 3. 读取规则：硬准入共享，排序仍由原 GroupMode 决定

### 3.1 所有模式必须使用的硬准入

所有模式都必须在自己的算法运行前应用：

```text
static compatibility
-> request-local skipped provider
-> capability negative cache (when implemented)
-> Provider cooldown
-> Provider×Model cooldown
-> Credential availability
-> HALF_OPEN lease eligibility
```

硬规则：

```text
COOLDOWN
    -> 不进入任何 balancer

request-local skipped
    -> 当前请求不再进入任何 balancer

HALF_OPEN
    -> 只有成功取得 single-flight lease 的真实请求可以进入候选集
```

现有 RR / Weighted / Failover / Random 等模式都不能绕过这一层。

### 3.2 AVAILABLE、SUSPECT 与 HALF_OPEN 的实际语义

实现采用两级候选集合，而不是把 HALF_OPEN 永久放在 AVAILABLE 之后：

```text
Primary candidates:
- AVAILABLE
- HALF_OPEN with acquired lease

Fallback candidates:
- SUSPECT

Excluded:
- COOLDOWN
- HALF_OPEN without lease
```

关键原因是被动恢复不能依赖合成健康探针。如果规定“只要还有 AVAILABLE，就永远不允许 HALF_OPEN 进入 balancer”，一个已经冷却结束的 Provider 在长期存在健康 Provider 时将永远拿不到真实恢复流量，形成 recovery starvation。

因此当前固定语义是：

```text
expired cooldown
  -> atomically acquire one HALF_OPEN lease
  -> leased candidate may re-enter the configured GroupMode
  -> only that one real request performs the recovery trial
  -> success => AVAILABLE / reset streak
  -> health failure => COOLDOWN again
  -> neutral outcome => release lease, remain HALF_OPEN
```

这不表示 HALF_OPEN 获得额外优先级。它只是重新进入原有 GroupMode，由 RR / Weighted / Failover / P2C 等既有算法决定是否被真正选中。single-flight lease 防止恢复惊群。

`SUSPECT` 仍是降级候选：只在 primary candidates 为空时交给原 GroupMode。

## 4. 各现有模式最终语义

### RoundRobin / Random / StrictRandom

```text
runtime eligibility
-> primary candidates (AVAILABLE + leased HALF_OPEN)
-> configured algorithm
-> if primary empty, SUSPECT fallback
```

StrictRandom 的“Strict”只表示不额外按 Priority、Weight、连续 health score 排序，不表示可以绕过 cooldown/runtime safety。

### Weighted

Provider configured weight 保持不变。共享健康层只决定候选能否进入权重池，不动态修改用户配置权重。

第一版不做：

```text
effectiveWeight = configuredWeight * healthScore
```

否则会把 Weighted 偷偷变成 HealthWeighted，并增加流量震荡风险。

### Failover

Priority 仍是主备语义。运行态过滤先排除 COOLDOWN；leased HALF_OPEN 可以参加真实恢复试验；SUSPECT 只作为 primary 为空时的降级候选。

### HealthFirst

HealthFirst 仍可在可参与路由的候选中使用连续 health score 做主要排序，但其 runtime state、rolling observations、cooldown 和 success/failure evidence 与其他模式完全共享。

### LeastUsed / P2C

并发比较只发生在 runtime eligibility 之后。处于 COOLDOWN 的 Provider 不会因为当前并发为 0 而被选中；leased HALF_OPEN 作为单个真实恢复候选可按原算法参与一次试验。

## 5. 不新增 Adaptive 模式

本轮明确：**不新增 `GroupModeAdaptive`。**

原因：

1. runtime eligibility 是可靠性安全层，不是用户偏好的流量分配算法；
2. 如果只在 Adaptive 生效，其他模式仍会反复选择已知坏站；
3. 新模式会迫使用户修改已有 Group 配置；
4. 两套健康行为会增加长期维护成本；
5. GPT-Load 值得借用的是 scheduler 前统一 eligibility，而不是再增加一个特殊 balancer；
6. 当前真实故障证据说明主要问题发生在公共路由闭环，而不是某一种 balancing 算法本身。

最终结构：

```text
                 Shared Runtime Facts
             Provider / Model / Credential
                         |
                         v
Request -> Static Compatibility
        -> Runtime Eligibility
        -> Primary / SUSPECT fallback candidate set
        -> Existing GroupMode
             RR / Random / StrictRandom
             Failover / Weighted / HealthFirst
             LeastUsed / P2C
        -> Sticky within runtime-safe candidates
        -> Credential selection
        -> Attempt
        -> Common Decision Engine
        -> Shared Runtime Facts
```

## 6. Iterator 与请求内动态排除

Iterator 维护请求级动态状态：

```text
skippedProviders
triedCredentials
providerAttemptBudget
wireAttemptBudget
unknownReplayBudget
```

外层每次取下一个候选时，必须重新尊重共享 runtime state 和 request-local skip。

Provider-transient failure 后，当前请求直接跳过该 Provider；明确 Credential failure 可以在同一 Provider 内轮换 key，但受 credential-attempt budget 限制。

## 7. outlier / health score 仍然是共享观测

所有真实 attempt 都应写共享 observations / outlier。HealthFirst 可以额外消费连续 score；Failover 可在不破坏 Priority 主语义的前提下用于稳定 tie-break。

outlier 的分钟级窗口适合慢速观察/POR，不承担秒级 cooldown 准入。

## 8. Sticky 的边界

最终顺序：

```text
static compatibility
-> runtime eligibility
-> configured GroupMode
-> sticky preference only when it does not violate runtime safety
```

Sticky 不能把 `SUSPECT` 或 `COOLDOWN` Provider 越级顶回健康候选之前，也不能绕过 HALF_OPEN single-flight lease。

如果 sticky Key 失效但 Provider 仍可用，可以清 Key affinity、保留 Provider affinity，再由 Credential scheduler 选择新 Key。

## 9. 第一阶段兼容性边界

这一阶段不需要：

- 新 GroupMode 枚举；
- 数据库迁移；
- 前端 Adaptive 配置；
- 用户修改现有 Group 配置；
- 合成 `ping` / `hello` / 1-token 健康探针。

正常健康状态下，所有候选均为 AVAILABLE，公共层近似 no-op，各模式保持原有流量语义。

## 10. 验收重点

至少覆盖：

1. 任一模式产生 Provider failure 后，状态能被其他模式立即共享；
2. first-token timeout 写 `Provider × Model` cooldown；
3. 429/model capacity 无论是否存在替代 Provider，都写 `Provider × Model` cooldown；
4. 多 Provider 429 可以立即切换，不重复累计同一次 capacity evidence；
5. 单 Provider 429 后，紧接着的下一请求被 runtime eligibility 挡住，不再次击打上游；
6. COOLDOWN candidate 在所有 GroupMode 中均不进入 balancer；
7. AVAILABLE + SUSPECT 并存时优先 primary；只有 SUSPECT 时仍可降级工作；
8. HALF_OPEN 同一 Provider/Model 最多一个真实请求获得 lease；
9. leased HALF_OPEN 即使同时存在 AVAILABLE，也可以重新进入原 GroupMode，避免 recovery starvation；
10. Weighted / RR / StrictRandom / P2C / LeastUsed 在过滤后仍保持各自原始算法语义；
11. sticky 不能绕过 SUSPECT/COOLDOWN/HALF_OPEN lease；
12. all healthy/AVAILABLE 时行为与改造前一致。

## 11. 最终决策

**不新开路由模式。**

共享 runtime 层回答的是：“这个候选现在是否应该参与路由？”

原 GroupMode 回答的是：“在允许参与的候选中，按什么策略选？”

HALF_OPEN 是共享 runtime 状态，不是独立优先级。冷却到期后，它通过 single-flight lease 获得一次重新进入原 GroupMode 的真实流量机会，以完成被动恢复；这正是当前实现的固定语义。
