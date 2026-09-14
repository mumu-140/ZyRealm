# 2026-09-14 Relay failover 只读诊断

> 范围：生产日志、只读数据库查询、部署身份与既有源码核验。仅新增本报告和配套 JSONL；没有修改 Go、TS、SQL、配置、部署文件或生产数据，没有重放真实请求、构建镜像或操作容器生命周期。
>
> 最后采集：2026-09-14T19:13:32.396269+08:00。时间显示统一采用 Asia/Shanghai（UTC+08:00）；原始容器时间在 JSONL 中保留 UTC。

## 1. 结论

1. **3 条 first-token timeout 的直接阻断点是非模型输出的 heartbeat/header 写入被当成 delivery started。** 记录的 RuleID 确实为 `first_token_timeout`，但实际 RetryDirective 是 `terminal`，ReplaySafety 是 `downstream_committed`；不是缺少 timeout classifier。3 条均无首个模型协议输出，超时后没有下一次 provider attempt。
2. **3 条最终 context canceled 是外层请求取消。** 最终 trace 为 outer=`canceled`、cause=`context_canceled`、rule=`client_cancel`，应终止；不能据错误字符串归咎 provider，也无法仅据现有证据区分最终用户、客户端代理或其他下游代理。
3. **2 条 INTERNAL_ERROR 是真实模型协议输出之后的流失败。** FTUT 为 19,165 / 9,874 ms，commit=true，正确 terminal；不建议跨 provider replay。
4. **真实 failover 并非全面失效。** 外层 active 的拨号/transport deadline 后实际切到了下一渠道；两次 524 后也各有下一渠道。另有一次 HTTP 400 后切换并最终成功。
5. **额外失败全部列入本报告。** 共 12 个请求、18 个实际 attempt，最终 8 失败 / 4 成功；14 个 failed attempt、4 个 success attempt。范围包括主窗、跨窗请求尾部及明确标注的成功对照，不把它们混成“主窗新发请求数”。

A 类在本文表示“本应保留预输出 failover 资格，却被通用 writer commitment gate 提前终止”，**不表示已证明当时必有健康候选或切换必能成功**。历史候选可用性缺口见第 7 节。

## 2. 实际部署身份，不以 main 代替生产

| 证据 | 实际值 |
| --- | --- |
| 运行源码 SHA（OCI revision） | `cbe4638b01aa5beb1a46f73dfb41cabaecaf890c` |
| 运行版本 / release tag | `v0.11.0-mumu.4` |
| 镜像 ID / digest | `sha256:0105a202978785c7fc6a5d72453b78970a4897ea53c78f42d88c1008f2560787` |
| 容器 ID | `85c13f4f442a67041ccd9b335f96c0f65c1c95b0e3449b1b1c1b5e2814c2ed2d` |
| 容器启动时间 UTC / restart count | `2026-09-14T05:38:16.444286838Z` / 0 |
| 容器状态 / 网络 / 数据挂载核对 | running / host / 精确匹配生产数据目录 |
| 状态清单 sourceCommit / release.sourceCommit / tag 解引用 | 三者均等于运行 SHA |
| 状态清单 sourceTree / Git tree | `b52b3d006e4411777107f3ccaf096e4e4b2e8df0`，一致 |
| fetch 后 current main | `c3e1e5a018b136ee61c7e2696275ea314f916dcd` |
| 诊断分支基线 | `origin/codex/relay-failover-error-audit` @ `54922956a2a8e72f802f5b6a9dd04240a17b6dab` |

运行版本确实不同于 current main，**但不能据此解释本次 failover 终止**。本次核对 24 个相关源码文件：用户列出的 12 个 Go 文件均与审计分支一致；全部 24 个文件的本地只读快照 SHA-256 均匹配部署 Git 对象，其中 **23 个与审计分支逐字一致**。唯一例外是 `relay_request.go`：`copyHeaders` 增加客户端请求头模板渲染；本报告引用的 `sendRequest` 实现未变。部署版本已经含有 first-token timeout 和 outer-active ambiguous cancellation 的 next-provider 逻辑。

部署到 main 的 relay/model 差异涉及 client-header template、protocol attempt、request clone、type、WS client 等；不能把“所有 relay 文件都一致”写成结论。本报告引用的代码链接均固定在**部署 SHA**，不是浮动分支。启动时间早于事件窗口，取证前后容器 ID、image ID、StartedAt、restart count 均未改变。

## 3. 数据来源、窗口与字段语义

- 优先窗口：2026-09-14 **17:55:00–18:08:30**，Unix 秒 `1789379700–1789380510`。
- 主窗内开始的请求为 **10 个（7 失败 / 3 成功）**。
- 额外纳入 17:54:30 开始、17:55:00 完成的 carry-in：`1789379700535`。
- 18:08:02 开始的 `1789380759127` 在 18:12:39 才终止，所以容器日志跟踪到 **18:13:00**，不截断它的 attempt 链。
- 额外纳入尾部日志中的 `1789380765954`（18:12:39 开始、400 后切换成功），标记为 `supplemental_after_window`，不计入主窗新发请求。
- 数据库扫描采用主窗前 1 小时的 bounded carry-in 回看、完成时间交集条件和上述补充 ID；不声称已扫描更早的任意长请求。容器窗口中全部 12 条 `relay.complete` 都能唯一匹配到所导出的请求，没有观察到未匹配的完成记录。
- 容器日志共 **40 行：38 条非空记录 + 2 条空行**；全部非空行已归类。31 WARN、7 INFO，没有 ERROR/FATAL/PANIC 级记录；这不等于“没有失败”。

### 实际 schema / API

先核验了 `relay_logs`、`groups`、`group_items`、`channels`、`channel_keys`、`settings` 的 `PRAGMA table_info`，再查询；SQLite 使用 `mode=ro`、`PRAGMA query_only=ON` 和固定只读事务，结束以 `ROLLBACK` 释放快照。

- `relay_logs` 的 `attempts` 是序列化 JSON；ChannelAttempt 嵌入 AttemptRoutingTrace，不另猜一个 trace 表。
- streaming 只在服务器进程内从 `request_content` 提取布尔 `stream`；不导出该字段的其他内容。
- `time` 是请求开始秒数，`use_time` 是请求总毫秒数；不是错误发生时刻。见 [metrics 存储语义](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/metrics.go#L242-L267)。
- attempt 没有独立持久化开始/结束时间或 HTTP status 字段。HTTP 400/524 来自同窗容器记录与脱敏错误类别；不伪造 attempt 精确时间。
- 完成时间以容器 `relay.complete` 为准，按 model + final channel ID + success + duration_ms + attempts 唯一匹配（12/12）；这是**派生关联**，不是一个原生 request correlation ID，也不把 log ID 解码成时间。
- 日志没有持久化 group ID/name/mode 或完整候选列表。group 信息是按 request model 精确查得的**采集时快照**；不能冒充事件时 snapshot。源码按 name 取 group 的契约见 [GroupGetEnabledMap](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/op/group.go#L41-L60)。
- 已核验既有 [日志 API](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/server/handlers/log.go#L19-L62) 与 [模型字段](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/model/log.go#L15-L32) / [trace 字段](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/model/attempt_routing_trace.go#L3-L23)。`include_content=false` 也不能保证任意 error/msg 不含敏感内容，所以没有直接提交 API 原始输出。

### 配置快照（不是历史事件配置证明）

采集时间：2026-09-14T19:13:32.396269+08:00。先前 18:34:59 的快照与本次相关 group/settings 值相同，但仍不据此假定事件期间从未变化。

| Group ID | Group name | Mode | FirstTokenTimeOut 秒 | RetryEnabled / MaxRetries | 配置 items / distinct channels | enabled items / distinct channels | MaxConcurrency 值 / MaxRPM |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| 65 | `deepseek-v4-flash-0731` | 5 / HealthFirst | 20 | true / 1 | 24 / 23 | 21 / 20 | 3 / 0 |
| 67 | `glm-5.3-flash` | 5 / HealthFirst | 30 | true / 1 | 38 / 28 | 35 / 25 | 3, 5 / 0 |

全局快照：`sse_pre_stream_heartbeat_delay=20s`、`sse_heartbeat_interval=20s`、provider attempt limit=30、wire attempt limit=50。源码固定 unknown-outcome cross-provider replay allowance=1。

`MaxRetries=1` 限制**同一候选/credential/protocol 的执行循环**，不是整个请求只能尝试一个 provider；[sameChannelRetryLimit](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/relay_handler_support.go#L66-L74) 与 [runProtocolRetries](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/relay_handler_support.go#L198-L240) 明确如此。本文的 provider 计数按 **channel ID**，不等价于已验证物理上游彼此独立。

## 4. 全部请求与完整渠道顺序

GLM=`glm-5.3-flash`；DS=`deepseek-v4-flash-0731`。顺序列中的每项为 `channel ID / numeric key ID`；这些全部为真实 wire attempt，没有插入推测的 skip。分类只对最终失败请求分配 A–F。

| Relay log ID | 开始 +08 | 完成 +08（毫秒） | 模型 | streaming | 完整 channel/key 顺序 | FTUT / 总耗时 ms | 最终结果/分类 |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `1789379700535` | 17:54:30 | 17:55:00.535 | GLM | true | `246/360` | 0 / 30157 | A |
| `1789379754718` | 17:55:00 | 17:55:54.718 | GLM | false | `195/300` | 0 / 54019 | success 对照 |
| `1789379836071` | 17:55:56 | 17:57:16.071 | GLM | true | `195/300` | 19165 / 79176 | D |
| `1789379874326` | 17:57:16 | 17:57:54.326 | GLM | false | `195/300` | 0 / 38093 | success 对照 |
| `1789379961436` | 17:58:09 | 17:59:21.436 | GLM | true | `195/300` | 9874 / 71629 | D |
| `1789379969479` | 17:59:21 | 17:59:29.479 | DS | true | `158/243` | 6050 / 7914 | success 对照 |
| `1789379991463` | 17:59:29 | 17:59:51.463 | DS | false | `158/243` | 0 / 21905 | C |
| `1789380201934` | 18:02:51 | 18:03:21.934 | GLM | true | `233/345` | 0 / 30166 | A |
| `1789380368856` | 18:03:22 | 18:06:08.856 | GLM | false | `244/358 → 251/365 → 190/290` | 0 / 166761 | C |
| `1789380482030` | 18:07:39 | 18:08:02.030 | DS | true | `158/243 → 219/328` | 0 / 22777 | A |
| `1789380759127` | 18:08:02 | 18:12:39.127 | DS | false | `70/149 → 66/145 → 190/290` | 0 / 276976 | C |
| `1789380765954` | 18:12:39 | 18:12:45.954 | DS | false | `123/202 → 218/327` | 0 / 6063 | success 对照 |

配套 [JSONL](2026-09-14-relay-failover-diagnostic.jsonl) 每行一个请求事件（12 行），内含完整 attempts（18 个）、原有路由字段、group 配置快照、后续 attempt 是否真实出现及清楚标注的派生诊断字段。

共同语义：
- 本组全部 attempt 的 CredentialRevision=1，ingress=`anthropic`，selected=`openai_chat`，protocol mode=`follow`，attempt kind=`candidate_primary`，DispatchState=`maybe_sent`。
- 原始 JSON 的 bool 使用 `omitempty`；JSONL 对缺失的 DownstreamCommitted 按 Go bool 零值归一为 false，并保留 `downstream_committed_serialized`。其他缺失 trace 字段保留 null。
- **成功 attempt 的 DownstreamCommitted=false 不能解读为“成功响应没有写出”**：[finishSuccessfulAttempt](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/relay_attempt.go#L67-L78) 构造成功结果时没有填 Written。本报告不依赖成功行的该值推断 payload。
- OutboundContextCause 是 [outboundContextCause](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/routing_decision.go#L382-L410) 对错误链的枚举，不是导出的完整 context cause。timeout 行该字段缺失，不应臆造为 `context_canceled`。

## 5. 三类点名错误的直接 gate

### 5.1 First token timeout：非 payload 写入触发 Written terminal（A，3 请求）

| Relay log ID | 失败渠道 / key | 错误 | RuleID / Directive | ReplaySafety / committed / outer | 超时后下一 provider |
| --- | --- | --- | --- | --- | --- |
| `1789379700535` | 246 / 360，773 公益站，oct-限免 | first token timeout (30s) | first_token_timeout / terminal | downstream_committed / true / active | 无 |
| `1789380201934` | 233 / 345，Fengwind，oct-稳定dp | first token timeout (30s) | first_token_timeout / terminal | downstream_committed / true / active | 无 |
| `1789380482030` | 219 / 328，霸气公益平台，英伟达 group | first token timeout (20s) | first_token_timeout / terminal | downstream_committed / true / active | 无第 3 次 |

最后一行真实渠道名称为“**霸气公益平台**”，与提供线索中的“霸王公益平台”略有不同；模型、时间、20s timeout 和原始渠道 ID 对应。它先在 158 收到 HTTP 400，已经切换至 219，不能描述为“本请求一次都没有切换”。

这 3 条共同具有：
- FTUT=0，sanitized error 为 `failed to send request: first token timeout (...s)`；
- timeout 分支已正确识别，FailureScope=`provider_model`、RuntimeEffect=`model_cooldown`、CircuitEffect=`none`、OutlierEffect=`model_failure`；
- `maybe_sent` 没有走到 `unknown_upstream_outcome`，因为更早的 Written 条件已把 replay safety 改为 `downstream_committed`；
- 实际 provider/wire index 分别 1/1、1/1、2/2；没有预算拒绝或后续 skip 记录。

**证据链：**

1. [handler 先启动早期 heartbeat](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/relay_handler.go#L99-L118)；[heartbeat 写出 HTTP 200 header、Flush 和 SSE comment](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/heartbeat.go#L52-L76)，具体写点在 [105–123 行](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/heartbeat.go#L105-L123)。
2. [sendRequest](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/relay_request.go#L98-L120) 在 `httpClient.Do` 出错时规范化 timeout 并返回；[标准 HTTP 路径](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/relay_http.go#L217-L244) 此时还没有进入 response/stream processor。因此这些 `failed to send request` 失败不是模型 token 已返回后的 timeout。
3. [deliveryStarted](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/relay_attempt.go#L124-L131) 虽先检查 `streamPayloadWritten`，却会回退到通用 `Writer.Written()`。header/heartbeat 写入足以使这个值为 true，即使没有模型 payload。
4. [first-token routing 分支](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/routing_decision.go#L162-L184) 因 Written=true 返回 terminal；[handler 的 attemptActionWritten](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/relay_handler.go#L307-L336) 保存 failed 后立即返回，不再走下一个候选。

**能证明与不能证明的边界：**持久化 trace 和源码能证明这里是“未收到上游响应，但通用 writer 已提交”这一 gate；heartbeat/header 是该等待路径的写出来源。现有日志没有逐次 heartbeat 的发送时间或已写字节数，不能伪造每条心跳恰在第 20,000 ms 发出。配置快照与 20s/30s 时序一致，但不是唯一依据。也没有当时剩余候选的可用性快照，所以只确认不应把这 3 条当成“已输出模型内容后正确终止”的 D 类，不保证绕过该 gate 后请求必成功。

### 5.2 Context canceled：最终外层确已取消（C，3 请求）

| Relay log ID | 最终 channel/key | streaming | 完整失败链 | 最终 outer / cause / rule | 最后一次后是否继续 |
| --- | --- | --- | --- | --- | --- |
| `1789379991463` | 158/243，liWAN LAB | false | 158 context canceled | canceled / context_canceled / client_cancel | 否，正确 terminal |
| `1789380368856` | 190/290，omniroute | false | 244 transport deadline → 251 HTTP 400 capability → 190 context canceled | canceled / context_canceled / client_cancel | 否；此前确有两次切换 |
| `1789380759127` | 190/290，omniroute | false | 70 HTTP 524 → 66 HTTP 524 → 190 context canceled | canceled / context_canceled / client_cancel | 否；此前确有两次切换 |

3 个最终 attempt 的 DownstreamCommitted=false、ReplaySafety=`client_canceled`、FailureScope=`none`、RuntimeEffect/CircuitEffect/OutlierEffect=`none`。源码 [isClientCancellation](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/cancel.go#L43-L70) 以 outer request context 为权威；[client_cancel decision](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/routing_decision.go#L116-L125) 正确终止。

反例同样存在：`1789380368856` 的 attempt #1 为 **outer=active / cause=deadline_exceeded / RuleID=ambiguous_transport_cancel / next_provider / unknown_upstream_outcome**，随后真实出现 251 的 attempt #2。这证明不能把所有含 cancellation/deadline 的 transport 错误都称为 client cancel，也证明 active-outer 的 next-provider 路径在部署版本实际运行过。

无法仅从这些记录判定取消来自终端用户、客户端超时还是中间代理；未读取或发布客户端 IP、私有网络地址、请求内容来猜测来源。

### 5.3 INTERNAL_ERROR：真实协议 payload 已写出（D，2 请求）

| Relay log ID | channel/key | HTTP/2 stream ID | FTUT ms | DownstreamCommitted | RuleID / RetryDirective | 后续 provider |
| --- | --- | --- | --- | --- | --- | --- |
| `1789379836071` | 195/300，黑与白-glm | 7 | 19165 | true | downstream_committed / terminal | 无 |
| `1789379961436` | 195/300，黑与白-glm | 11 | 9874 | true | downstream_committed / terminal | 无 |

两条 outer=active、ReplaySafety=`downstream_committed`、CircuitEffect=`none`、OutlierEffect=`model_failure`；并非 client cancellation。

这里不只依赖 committed bool：[StreamProcessor.processEvent](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/stream/processor.go#L204-L236) 仅在非空转换输出写成功后设置 `payloadWritten`；[OnFirstToken 调用点](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/stream/processor.go#L178-L198) 再触发 [FTUT 记录](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/relay_stream_response.go#L25-L44)。heartbeat 不设置 payloadWritten，也不触发该回调。因此正 FTUT 是已写过至少一个**非 heartbeat 的模型协议 payload** 的证据；不额外宣称该 payload 一定是用户可见自然语言 token。

按 [post-commit gate](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/routing_decision.go#L187-L201) 正确 terminal。不能通过跨 provider replay 拼接或重复响应。本窗口没有 DownstreamCommitted=false 的 INTERNAL_ERROR 样本，无法据这两条证明“预输出 INTERNAL_ERROR classifier 缺陷”；这一问题仍需独立预输出证据或隔离测试。

## 6. 额外报错、失败 attempt 与其他告警

### 6.1 不限于用户点名三类的失败

以下时间是容器对应告警时间，均为 +08:00。

| 时间 | Relay log ID / attempt | channel/key | 脱敏错误类别 | 路由决策与实际后续 |
| --- | --- | --- | --- | --- |
| 18:03:52.256 | `1789380368856 / 1` | 244/358，abrdns | `failed to send request: Post "<upstream>": dial tcp <upstream>: [transport timeout]` | outer active；ambiguous_transport_cancel → next_provider；30,160 ms 后结束此 attempt，下一渠道 251 |
| 18:05:12.825 | `1789380368856 / 2` | 251/365，午夜-默认 | HTTP 400；unsupported parameter(s)；bad_response_status_code；body omitted | model_capability → protocol_or_provider；随后是不同渠道 190，不是同渠道协议回退 |
| 18:07:41.949 | `1789380482030 / 1` | 158/243，liWAN LAB | HTTP 400；invalid_request_error / request_rejected；body omitted | status_4xx_request → next_candidate；随后 219；请求最终因 heartbeat/timeout gate 失败 |
| 18:10:08.230 | `1789380759127 / 1` | 70/149，烁，mac1 | HTTP 524；Cloudflare timeout/intercept page；body omitted | provider_intercept → next_provider；126,078 ms；随后 66 |
| 18:12:13.997 | `1789380759127 / 2` | 66/145，烁，opensource-slow | HTTP 524；Cloudflare timeout/intercept page；body omitted | provider_intercept → next_provider；125,764 ms；随后 190 |
| 18:12:41.667（补充） | `1789380765954 / 1` | 123/202，123nhh，9rt | HTTP 400；upstream_error / bad_response_status_code；body omitted | status_4xx_request → next_candidate；随后 218/327 成功，整个请求 6,063 ms |

- 这些共有 **6 个其他 failed attempt**（1 transport deadline、3 HTTP 400、2 HTTP 524），加上 3 timeout、3 最终 client cancel、2 INTERNAL_ERROR，共 14 个 failed attempt。
- HTTP 400 的 domain/rule 是已记录的路由分类，不是对用户输入是否真实有错的独立证明。未导出 body，不能臆造具体参数值或上游拒绝细节。
- 两条 524 有 provider cooldown / circuit record_failure / provider_failure，且都实际继续了；不能把“本次失败后记录 cooldown”反过来说成“本次因 cooldown 没有切换”。
- 524 所属请求 streaming=false。[首 token budget 仅用于 Stream=true](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/first_token_timeout.go#L51-L76)，因此它们约 126s 的等待不受 group 20s 首 token timeout 限制；这不是证据缺失的“20s timeout 没执行”。
- 两个“烁”渠道属于不同 channel ID；没有导出 upstream 地址验证底层服务独立性，不据名称推断物理 provider 故障域。

### 6.2 其余日志全部说明，不把 WARN 等同于独立失败

| 日志类别 | 主窗 | 跟踪尾部 | 含义 |
| --- | ---: | ---: | --- |
| first_token_timeout_warning | 3 | 0 | 3 个 timeout attempt；文案包含 switching channel，但不代表真的完成切换 |
| request_canceled_before_upstream_response | 2 | 1 | INFO 级的最终取消提示，与 3 个 client_cancel 请求对应 |
| failed_to_send_request | 1 | 0 | 上述 active-outer transport deadline |
| upstream_error，HTTP 400 | 2 | 1 | 上表 3 个 400 |
| upstream_error，HTTP 524 | 0 | 2 | 上表 2 个 524 |
| relay.complete，failed | 7 | 1 | 共 8 个失败请求 |
| relay.complete，success | 3 | 1 | 共 4 个成功请求 |
| http.slow，POST relay_messages，HTTP 200 | 10 | 2 | 12 个请求均触发慢日志；HTTP 200 不代表 relay success |
| http.slow，GET log_stream_sse，HTTP 200 | 2 | 0 | 管理端日志订阅长连接，不是两次模型调用失败 |
| 空行 | 0 | 2 | 位于两条 524 后；已确认 timestamp 后内容为空，不是遗漏的未知异常 |
| 其他未分类非空行 | 0 | 0 | 采集窗口内没有 |

两个 GET SSE 订阅结束时间为 17:57:36.384 / 18:08:21.190，耗时 1,082,659 / 350,137 ms，status=200。它们属于日志订阅的连接时长告警，不能自动判成 API 故障。

两项可观测性问题：
1. [timeout warning](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/first_token_timeout.go#L99-L110) 在最终 routing gate 之前写“switching channel”。本次 3 条都是该文案之后直接 terminal，故该文案不能作为成功 failover 证据。
2. [HTTP logger](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/server/middleware/logger.go#L19-L57) 读取的是 writer status 和整条请求延时。早期 heartbeat 会提交 200；取消路径也可能保留默认 200。**12 条 POST slow 均显示 200，其中 8 条 relay.complete 为 false**；既不能据 200 说模型请求成功，也不能据所有 WARN 统计 31 个失败请求。最终 outcome 以 [RelayLog.Success](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/metrics.go#L306-L313) 为准。

上述“没有某类日志”仅限被采集的生产容器窗口，不等于没有未启用的 DEBUG 事件、客户端错误或其他服务的异常。

## 7. Alternative candidate、skip 和 budget 证据矩阵

| Gate / skip 原因 | 本次观察 | 能下的结论 / 信息缺口 |
| --- | --- | --- |
| runtime cooldown / half-open busy | 0 条对应 skipped attempt | 初始化时 [runtimeOrderedCandidates](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/balancer/runtime_candidates.go#L15-L38) 直接过滤 cooldown，不产生 attempt；未保存事件时列表，无法排除前置过滤 |
| circuit breaker | 0 条 circuit_break attempt | 不是这些最终失败的已观察终止理由；不能从零记录还原全体未选 credential 的状态 |
| disabled | 0 条 channel-disabled attempt | group 读取时 [直接过滤 disabled](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/op/group.go#L51-L60)；当前快照每组各有 3 个 disabled channel，不等于事件时有这 3 条 skip |
| capability negative cache | 0 条对应 skipped attempt | 观察到一次 model_capability 失败，但未观察到本样本后续候选因该缓存被跳过 |
| max concurrency | 0 条 capacity_skipped | 当前配置有上限，不含历史在途数量；不能从配置推出当时是否满载 |
| RPM | 0 条 rate_skipped | 当前相关渠道 MaxRPM=0；不冒充事件时配置证据 |
| provider attempt budget | 0 条 attempt_budget / provider budget skip | 本例最终 gate 是 Written 或 client cancel；当前 limit=30，实际最大 provider index=3 |
| wire attempt budget | 0 条 wire budget skip / budget terminal 证据 | 当前 limit=50，实际最大 wire index=3；所有记录的 next-provider decision 后均有真实下一渠道 |
| unknown-outcome replay budget | 1 个 outer-active ambiguous outcome 实际继续下一 provider；未见耗尽终止 | [源码 allowance=1](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/attempt_budget.go#L11-L15)，在 [handler 条件块](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/relay_handler.go#L263-L279) 消耗；3 个 Written timeout 不进入该 replay 条件块 |
| no alternative candidate | 最终停止时没有持久化 alternative flag/list | **unknown**，不填 false；配置中有许多候选不证明该时刻仍 eligible。已继续的失败则有真实下一渠道作为正证据 |

全部 18 条状态仅为 success/failed。**无 skip 记录不等于没有筛选或跳过**，尤其 disabled/runtime cooldown 可能发生在 trace 之前。[HasAlternativeProvider](https://github.com/mumu-140/ZyRealm/blob/cbe4638b01aa5beb1a46f73dfb41cabaecaf890c/internal/relay/balancer/iterator_alternative.go#L7-L25) 也只是检查迭代器中剩余且未 request-local skip 的不同 channel，并非所有下游准入检查的历史快照。

本样本：
- 3 个 `next_provider` decision 均后接真实不同 channel attempt。
- 1 个 `protocol_or_provider`、2 个 `next_candidate` 也均后接真实下一渠道。
- 没有“trace 明示 next_provider，但最后没有下一 attempt”的样本。不能为了凑 B 类而猜一个 replay/budget/candidate-exhaustion 原因。

## 8. 完整 attempt 目录

更完整的 FailureDomain、FailureScope、CredentialRevision、RuntimeEffect/State/CooldownUntil、CircuitEffect、OutlierEffect、ReplaySafety、DispatchState、DownstreamCommitted、OuterContextState、OutboundContextCause、ProviderAttempt、WireAttempt 全部保留在 JSONL，null 明确代表原 trace 未序列化；没有补造取值。

| Relay log ID / attempt | channel/key 与渠道名 | 上游模型名（不是地址） | 耗时 ms | status | RuleID → RetryDirective | ReplaySafety | provider/wire |
| --- | --- | --- | ---: | --- | --- | --- | --- |
| `1789379700535 / 1` | `246/360` 773 公益站 \| oct-限免 (auto) | `z-ai/glm-5.3-free` | 30156 | failed | `first_token_timeout` → `terminal` | `downstream_committed` | 1/1 |
| `1789379754718 / 1` | `195/300` 黑与白-glm | `glm-5.3-flash` | 54017 | success | `success` → `complete` | `safe` | 1/1 |
| `1789379836071 / 1` | `195/300` 黑与白-glm | `glm-5.3-flash` | 79175 | failed | `downstream_committed` → `terminal` | `downstream_committed` | 1/1 |
| `1789379874326 / 1` | `195/300` 黑与白-glm | `glm-5.3-flash` | 38091 | success | `success` → `complete` | `safe` | 1/1 |
| `1789379961436 / 1` | `195/300` 黑与白-glm | `glm-5.3-flash` | 71626 | failed | `downstream_committed` → `terminal` | `downstream_committed` | 1/1 |
| `1789379969479 / 1` | `158/243` liWAN LAB \| 国模 (auto) | `DeepSeek-V4-Flash-0731` | 7913 | success | `success` → `complete` | `safe` | 1/1 |
| `1789379991463 / 1` | `158/243` liWAN LAB \| 国模 (auto) | `DeepSeek-V4-Flash-0731` | 21904 | failed | `client_cancel` → `terminal` | `client_canceled` | 1/1 |
| `1789380201934 / 1` | `233/345` Fengwind \| oct-稳定dp (auto) | `glm-5.3-flash` | 30164 | failed | `first_token_timeout` → `terminal` | `downstream_committed` | 1/1 |
| `1789380368856 / 1` | `244/358` abrdns \| level 2 group (auto) | `GLM-5.3-Flash` | 30160 | failed | `ambiguous_transport_cancel` → `next_provider` | `unknown_upstream_outcome` | 1/1 |
| `1789380368856 / 2` | `251/365` 午夜-默认 | `glm-5.3-flash` | 80567 | failed | `model_capability` → `protocol_or_provider` | `safe` | 2/2 |
| `1789380368856 / 3` | `190/290` omniroute | `午夜/glm-5.3-flash` | 56027 | failed | `client_cancel` → `terminal` | `client_canceled` | 3/3 |
| `1789380482030 / 1` | `158/243` liWAN LAB \| 国模 (auto) | `DeepSeek-V4-Flash-0731` | 2696 | failed | `status_4xx_request` → `next_candidate` | `safe` | 1/1 |
| `1789380482030 / 2` | `219/328` 霸气公益平台 \| 英伟达 group (auto) | `deepseek-v4-flash-free` | 20078 | failed | `first_token_timeout` → `terminal` | `downstream_committed` | 2/2 |
| `1789380759127 / 1` | `70/149` 烁 \| mac1 (auto) | `deepseek-v4-flash-0731` | 126078 | failed | `provider_intercept` → `next_provider` | `safe` | 1/1 |
| `1789380759127 / 2` | `66/145` 烁 \| opensource-slow group (auto) | `deepseek-v4-flash-0731` | 125764 | failed | `provider_intercept` → `next_provider` | `safe` | 2/2 |
| `1789380759127 / 3` | `190/290` omniroute | `deepseek-v4-flash-0731-cb` | 25128 | failed | `client_cancel` → `terminal` | `client_canceled` | 3/3 |
| `1789380765954 / 1` | `123/202` 123nhh \| 9rt (auto) | `deepseek-v4-flash-0731` | 1775 | failed | `status_4xx_request` → `next_candidate` | `safe` | 1/1 |
| `1789380765954 / 2` | `218/327` 773 公益站 \| user group (auto) | `deepseek-v4-flash-0731` | 4283 | success | `success` → `complete` | `safe` | 2/2 |

## 9. 核验、隐私与后续边界

已执行的只读核验：
- 部署状态清单、Docker inspect、OCI revision、tag commit、Git tree 交叉核对。
- SQLite `PRAGMA quick_check` 返回 `[('ok',)]`。
- 24 个相关源码文件与部署 Git 对象逐文件 SHA-256 核对，全匹配；23 个文件对审计基线的 diff 为空。`relay_request.go` 仅有已核验的请求头模板渲染差异，其 `sendRequest` 与本次终止 gate 未变。
- 19:00:32 再读的请求、runtime、group/settings 与前一任务 18:34:59 采集内容一致；19:13:32 增补部署指纹、完成关联和空行分类后再次采集。
- 服务器执行 `bash -n scripts/check-governance.sh`、`bash scripts/check-governance.sh --repo`。

关键原始输出：

```text
REQUESTS 12 ATTEMPTS 18
REQUEST_OUTCOMES {'failed': 8, 'success': 4}
ATTEMPT_OUTCOMES {'failed': 14, 'success': 4}
ERROR_CLASS_UNCLASSIFIED 0
NEXT_PROVIDER_WITHOUT_LATER_ATTEMPT []
ALL_RUNTIME_COMPLETIONS_MATCHED True
governance check passed: repository rules, versions, tags, workflows and sensitive-file boundary
```

本任务没有执行 Go/TS 测试、发起故障注入或制造真实模型请求；这是**诊断证据交付，不是修复验收**。文档发布的 CI 状态以本诊断分支 / PR 的检查为准。生产未部署、未重启，SQLite 未写入；源码外的机器台账与生产配置未改。

隐私边界：
- 原始数据库内容和 docker 原始日志只在服务器进程内处理；仅白名单路由字段及固定错误类别出站。
- upstream 一律 `<upstream>`；credential 只保留 numeric key ID。
- 不发布 Authorization、Cookie、Bearer token、任何密钥、客户端 IP、私有 upstream URL/IP/query、用户 prompt、messages/input、tool arguments 或请求/响应 body。
- JSONL 的 group 与派生诊断字段明确区分“采集时快照 / 原始持久化 trace / 源码支持的推断”。未以不完整的 API 输出或任意正则替代字段白名单。
- 本地临时采集程序与证据快照不进入公共 Git 仓库；只提交本报告和 companion JSONL。

本任务不修改 classifier。后续若另行批准实现，最小验证应先隔离复现“早期 heartbeat 已写、首 token 超时、第二 provider 仍可用”这一具体链路，同时锁定“真实 payload 已输出后 INTERNAL_ERROR 不 replay”的安全不变量。无历史候选快照时，不承诺重试必然成功；不得整体移除 downstream commitment 保护或放宽未知结果重放上限。

## 10. A–F 分类汇总（按最终失败请求互斥归类）

本表仅计 8 个最终失败请求；4 个成功对照不属于失败分类。E 的部署差异是所有事件的版本属性，不重复作为失败根因计数。F=0 仅指最终终止 gate 已定位，不表示已还原历史候选状态、上游异常根因或客户端取消发起者。

| 类别 | 数量 | Relay log ID | 证据结论 |
| --- | ---: | --- | --- |
| A. 本应保留 failover 资格，但 control-flow/classifier 未继续 | 3 | `1789379700535`、`1789380201934`、`1789380482030` | timeout 已识别；无模型 payload，heartbeat/header 的通用 Written 标记触发 terminal；历史 eligible alternative 仍 unknown |
| B. 本应 failover，但被 replay/attempt/candidate budget 正确阻止 | 0 | — | 未观察到预算耗尽终止；不猜测一个预算原因 |
| C. 实际是 client/proxy cancellation | 3 | `1789379991463`、`1789380368856`、`1789380759127` | 最终 outer=canceled、rule=client_cancel；其中后两请求此前已多次切换 |
| D. 已有真实 downstream payload，因此正确 terminal | 2 | `1789379836071`、`1789379961436` | 正 FTUT 与非 heartbeat payload 写出路径相互印证；post-output INTERNAL_ERROR 不 replay |
| E. 部署版本过旧/与 current main 不一致导致 | 0 个可归因失败 | — | SHA 确有落差，但本次 gate/classifier/trace 相关源码已逐文件核实一致，不能据版本号归因 |
| F. 信息不足以判定最终终止 gate | 0 | — | 8 个失败的直接 gate 均有 trace 与源码证据；其余观测缺口已单独列明 |
