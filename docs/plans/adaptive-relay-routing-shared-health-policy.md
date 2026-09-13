# 自适应路由补充决策：共享健康事实层与现有策略兼容

状态：**最终设计补充 / 属于 adaptive relay routing plan**  
关联文档：`adaptive-relay-routing-final-review.md`  
主题分支：`codex/adaptive-relay-routing-plan`

> 本文固定两个实现决策：健康/运行态信息属于所有路由策略共享的事实层；本轮不新增 `Adaptive` GroupMode，而是在所有现有模式之前增加统一的 runtime eligibility / health tier 层。

## 1. 健康信息不是 HealthFirst 私有状态

`Provider`、`Channel × Model`、`Credential` 的运行态和健康证据必须由**所有真实请求 attempt 共同写入**，与当前 GroupMode 无关。

无论请求来自：

- RoundRobin
- Random
- StrictRandom
- Failover
- Weighted
- HealthFirst
- LeastUsed
- P2C

只要发生真实 attempt，都走同一个 failure decision / runtime observation 路径，并根据已经确定的 failure scope 更新共享状态。

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
- outlier rolling stats / health score

Credential runtime
- auth state
- quota/balance state
- cooldown_until / reset_at
- failure count
- last success/failure

Capability negative cache
- channelID
- upstreamModel
- capabilitySignature
- expires_at
```

`HealthFirst` 只是这些共享事实的一个**更强消费者**，不是数据拥有者。

## 2. 写入规则：所有策略一致

路由模式不能决定是否记录健康信息。写入由 attempt 结果和 failure scope 决定。

例如：

```text
Weighted 请求 -> first-token timeout
    => 写 Provider×Model timeout/runtime evidence

RoundRobin 请求 -> CF 522
    => 写 Provider transient/runtime evidence

P2C 请求 -> 429 model capacity
    => 写 Provider×Model cooldown evidence

Failover 请求 -> invalid key
    => 写 Credential evidence

HealthFirst 请求 -> success
    => 写相同的 success evidence
```

因此不能存在：

```text
if mode == HealthFirst {
    report health
}
```

应该是：

```text
attempt result
  -> common classifier
  -> common runtime updater
  -> shared state
```

然后各策略下一次选路都从同一份共享状态读取。

## 3. 读取分两层：硬准入共享，软排序按模式

为了既提高可靠性，又不破坏现有模式语义，把共享健康信息分成两种使用方式。

### 3.1 所有模式必须使用的硬准入

所有模式都必须在自己的算法运行前应用：

```text
static compatibility
-> request-local skipped provider
-> capability negative cache
-> Provider cooldown
-> Provider×Model cooldown
-> Credential availability
-> HALF_OPEN lease eligibility
```

这部分不是“负载均衡策略”，而是**候选是否合法**。

因此：

```text
COOLDOWN = 不进入任何 balancer
capability mismatch = 当前请求能力签名下不进入任何 balancer
request-local skipped = 当前请求不再进入任何 balancer
HALF_OPEN = 没拿到 lease 就不进入任何 balancer
```

现有 RR / Weighted / Failover / Random 等模式都不能绕过这层。

### 3.2 所有模式共享 runtime tier

在硬过滤之后，候选按共享运行态分层：

```text
Tier 0: AVAILABLE
Tier 1: SUSPECT
Tier 2: HALF_OPEN with acquired lease
Tier X: COOLDOWN / unavailable -> excluded
```

默认只把**当前最优非空 tier**交给现有 balancer：

```text
AVAILABLE 非空
    -> 只在 AVAILABLE 中执行 GroupMode

AVAILABLE 为空、SUSPECT 非空
    -> 在 SUSPECT 中执行 GroupMode

AVAILABLE/SUSPECT 都为空
    -> 允许一个 HALF_OPEN lease 后执行
```

这样健康信息对所有模式都有效，但不会把 health score 强行变成所有模式的主排序规则。

## 4. 各现有模式最终语义

### RoundRobin

```text
best runtime tier
-> RoundRobin
```

仍然严格轮询可用候选；坏站/cooldown 不参加轮询。

### Random

```text
best runtime tier
-> Random
```

随机性只发生在当前最佳可用健康层中。

### StrictRandom

```text
best runtime tier
-> StrictRandom
```

`Strict` 表示不使用 Priority、Weight、连续 health score 等做额外排序，不表示可以绕过 cooldown / runtime safety。

### Weighted

```text
best runtime tier
-> 按原 GroupItem.Weight 加权抽样
```

Provider configured weight 保持不变。共享健康层只决定候选能否进入权重池，不动态修改用户配置权重。

第一版**不要**做类似：

```text
effectiveWeight = configuredWeight * healthScore
```

否则会把 Weighted 偷偷变成 HealthWeighted，难以解释，也容易造成流量震荡。

### Failover

```text
best runtime tier
-> Priority ASC
-> 同 Priority 内 health score DESC
```

明确 hard failure 后 Provider 会进入 cooldown，所以低优先级健康站能够接管；无需让 health score 全局覆盖用户 Priority。

### HealthFirst

```text
best runtime tier
-> health score DESC
-> 再用稳定轮换/priority 等 tie-break
```

这是唯一把连续 health score 作为主要排序信号的现有模式。

但它使用的 score、rolling observations、cooldown 和 runtime state，与其他模式完全共享。

### LeastUsed

```text
best runtime tier
-> in-flight concurrency ASC
-> Priority tie-break
```

不会因为某站并发最低就选择正在 SUSPECT/COOLDOWN 的候选。

### P2C

```text
best runtime tier
-> Power of Two Choices
```

两个候选均来自最佳 runtime tier；健康状态先于并发比较。

## 5. 不新增 Adaptive 模式

本轮明确：**不新增 `GroupModeAdaptive`。**

原因：

1. runtime eligibility 是可靠性安全层，不是用户偏好的流量分配算法；
2. 如果只在 Adaptive 生效，RR/Weighted/Failover/P2C 仍会反复选坏站，无法解决真实故障库中的主问题；
3. 新模式会迫使用户为了获得正确 failover 行为修改现有 Group 配置，增加迁移和理解成本；
4. 会出现两套健康行为：Adaptive 有 cooldown/half-open，其他模式没有，长期维护更复杂；
5. GPT-Load 值得借用的正是 scheduler 前统一 eligibility，而不是增加一个特殊 balancer；
6. 当前 14,288 请求故障证据说明问题发生在公共路由闭环，不是某一种 balancing 算法本身。

因此实现结构应是：

```text
                 Shared Runtime Facts
             Provider / Model / Credential
                         |
                         v
Request -> Static Compatibility
        -> Runtime Eligibility
        -> Best Runtime Tier
        -> Existing GroupMode
             RR / Random / StrictRandom
             Failover / Weighted / HealthFirst
             LeastUsed / P2C
        -> Sticky within eligible AVAILABLE candidates
        -> Credential selection
        -> Attempt
        -> Common Decision Engine
        -> Shared Runtime Facts
```

这是一个闭环，而不是第九种负载均衡器。

## 6. 对已有模式“更新”到什么程度

不是重写各模式，而是做三类公共改动。

### A. Balancer 之前新增公共候选过滤

推荐新增独立组件，例如：

```text
runtimeeligibility.Filter(group.Items, requestContext)
```

或等价的 candidate planner。

不要把 cooldown 判断分别复制到 8 个 `Candidates()` 实现中。

### B. Iterator 维护请求内动态排除

参考 GPT-Load 的 `SkipGroup`，Octopus 增加：

```text
skippedProviders
triedCredentials
providerAttemptBudget
wireAttemptBudget
```

外层每次取下一个候选时，重新尊重共享 runtime state 和 request-local skip。

### C. 各模式只保留自己的核心排序算法

例如 `Weighted.Candidates()` 继续只负责权重；`P2C` 继续只负责并发二选一；`RoundRobin` 继续只负责轮询。

健康状态、冷却、capability、half-open 不应该散落到这些算法内部。

唯一可保留的模式特异 health 使用：

- `HealthFirst`：连续 health score 是主排序；
- `Failover`：同 Priority 内可用 health score 做 tie-break；
- 其他模式：第一版只消费 shared runtime tier，不消费连续 score。

这样可以避免重复逻辑和策略语义漂移。

## 7. 现有 outlier / health score 也必须共享

当前 `outlierwindow` 本身已经按 `channelID + modelName` 维护滚动证据，这个粒度正确，应继续作为共享观测层，而不是绑定 `HealthFirst`。

建议职责固定为：

```text
所有 attempt
  -> 写 shared observations / outlier

所有模式
  -> 读取 runtime eligibility / tier

HealthFirst
  -> 额外读取连续 health score 排序

Failover
  -> 可在同 Priority 内读取 score tie-break

控制面 POR
  -> 读取长期 outlier 聚合
```

不能把 `outlierwindow.Report*` 的调用放在模式分支里。

同时仍维持之前的边界：outlier 的 10 分钟窗口、85% failure gate 等适合慢速观察/POR，不承担秒级 cooldown 准入。

## 8. Sticky 也属于公共层之后

最终顺序固定：

```text
static compatibility
-> runtime eligibility
-> best runtime tier
-> configured GroupMode
-> sticky preference only among eligible AVAILABLE candidates
```

如果 sticky Provider 已进入 SUSPECT，而存在 AVAILABLE Provider，则 sticky 不得把 SUSPECT 顶回首位。

如果 sticky Key 失效但 Provider 可用，可以清 Key affinity、保留 Provider affinity，再由 Credential scheduler 选择新 Key。

## 9. 实现兼容性

这一方案第一阶段不需要：

- 新 GroupMode 枚举；
- 数据库迁移；
- 配置 UI 增加“Adaptive”选项；
- 用户修改现有 Group 配置；
- 改变正常情况下 RR/Weighted/P2C 等的流量语义。

正常健康状态下：

```text
all candidates AVAILABLE
```

则公共层近似 no-op，各模式行为应与当前保持一致。

只有出现真实健康/能力/冷却证据时，公共层才改变候选集合。

## 10. 新增验收测试

在 `adaptive-relay-routing-final-review.md` 已有测试之外，增加：

1. RoundRobin 请求产生 503 后，写入的 Provider runtime 状态能影响后续 Weighted 请求；
2. Weighted 请求产生 first-token timeout 后，HealthFirst 能立即看到相同 Provider×Model cooldown；
3. HealthFirst 成功恢复后的 success evidence 能被 Failover/RR 共享；
4. `COOLDOWN` candidate 在所有 8 种 GroupMode 中均不会进入 balancer；
5. AVAILABLE + SUSPECT 并存时，所有模式先使用 AVAILABLE；
6. 只有 SUSPECT 时，各模式仍可降级工作，不直接无路可用；
7. HALF_OPEN 同一 Provider/Model 最多一个真实请求获得 lease，与 GroupMode 无关；
8. Weighted 在过滤后仍保持剩余 AVAILABLE Provider 的配置权重比例；
9. RR 在过滤后仍对剩余 AVAILABLE Provider 正常轮询；
10. StrictRandom 不能绕过 cooldown，但在最佳 runtime tier 内保持严格随机；
11. P2C/LeastUsed 不会因为 cooldown Provider 并发为 0 而选中它；
12. Failover 保持 Priority 主备语义，只在同 Priority 内用 score tie-break；
13. HealthFirst 使用与其他策略相同的 shared observations，而不是独立窗口；
14. sticky SUSPECT + 另有 AVAILABLE 时，sticky 不能越级；
15. all candidates healthy/AVAILABLE 时，各现有模式回归行为与改造前一致。

## 11. 最终决策

**不新开路由模式。**

本轮是在已有模式下方建立统一的运行态事实层、在已有模式上方建立统一的候选准入层：

```text
shared observations
      ^
      |
 common decision engine
      ^
      |
    attempt
      ^
      |
existing GroupMode
      ^
      |
shared runtime eligibility
```

`HealthFirst` 仍然有存在价值，因为它回答的是“在当前可用候选中，是否主动优先更健康的那个”；共享 runtime 层回答的是更基础的问题——“这个候选现在是否应该参与路由”。两者不是同一层。