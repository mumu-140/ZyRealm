# Octopus 开发治理

本文件把根目录 `AGENTS.md` 的规则落实为可执行的开发手册，回答“修改什么、在哪里改、怎么改、
最低验证是什么、哪些做法禁止”。本手册只记录能由当前代码、Git、CI、生产状态或已复盘事故
确认的事实，不把历史目录、旧 tag 或口头推测写成现状。

## 规则优先级与真值

规则冲突时按以下顺序处理：

1. 根目录 `AGENTS.md`；
2. 本手册；
3. `docs/octopus-production.md` 中与生产、候选、数据和回滚有关的约束；
4. README、USAGE 和历史记录。

运行事实冲突时，以当前代码、Git 对象、`deploy/fwq57ys/production-state.json` 和 Docker
inspect 证据为准。以下四项必须分别记录，不得相互推导：

1. 当前 `main` commit；
2. 运行应用源码 commit；
3. 运行镜像 tag / image ID；
4. 运行容器 ID。

`main` 更新、Release 成功、镜像已存在和容器已切换是四个不同状态。

## 生产真值来源

运行指纹只从两处读取，本手册和交付报告都不复制具体数值：

1. `deploy/fwq57ys/production-state.json`：唯一机器可读台账（release/tag/sourceCommit、
   镜像与 image ID、容器 ID、StartedAt、restart count、数据挂载、回滚快照）；
2. fwq57ys 上的实时 `docker inspect` 与 `scripts/check-governance.sh --live`。

两者冲突时以实时 inspect 为准，先查明漂移再动手。回滚网的命名与保留规则见
`docs/octopus-production.md`。

界面“当前版本”等于运行镜像的构建 tag：`Dockerfile.build` 用 `GIT_VERSION` 把版本烤进后端
二进制和前端产物，与工作树中的版本字段无关；界面“最新版本”是对 GitHub Release 的更新检查
结果。工作树版本字段落后于界面版本本身不是缺陷，见下方发布与部署字段矩阵。

| 目录 | 路径 | 用途 | 禁止 |
| --- | --- | --- | --- |
| 唯一源码 | `/opt/octopus-mumu/` | 开发、测试、构建、提交 | 不在第二份源码中继续工作 |
| 生产控制面 | `/opt/octopus/` | Compose 副本、真实数据、备份、部署证据 | 不初始化 Git、不放源码、不构建 |
| 生产数据 | `/opt/octopus/data/` | 仅获批的生产读写 | 不用于开发、单测或候选 |
| 历史源码 | `/opt/octopus-src*` | 只读追溯 | 不恢复旧改动、不构建、不部署 |
| 构建缓存 | `/opt/octopus-build-cache/` | 只作缓存 | 不视为源码或发布证据 |
| 生产声明 | `deploy/fwq57ys/compose.yaml` | 版本控制的目标 Compose | 不直接挂候选数据 |
| 发布目标与运行指纹 | `deploy/fwq57ys/production-state.json` | staging 记录目标 release/image，切换后记录 live 指纹 | 不把 staging 状态声称为已运行 |

仓库根目录没有通用生产 `docker-compose.yml`。开发环境必须使用独立配置和数据，不得为了
“方便”复用生产控制面。

## 修改路由

先按变更类型找到主文件，再检查同一行中的联动区域。Handler 不应绕过 `internal/op/`
直接实现业务规则；模型、迁移、备份和恢复必须作为一个数据契约审查。

| 变更类型 | 主要修改位置 | 必须同步检查 |
| --- | --- | --- |
| 配置、默认值、版本信息 | `internal/conf/` | `cmd/`、`web/src/lib/info.ts`、配置文档；发布时再检查版本矩阵 |
| 数据模型、索引、迁移 | `internal/model/`、`internal/db/migrate/` | `internal/db/`、`internal/op/backup.go`、迁移/备份测试 |
| 业务规则、缓存、查询 | `internal/op/` | 调用它的 Handler、缓存失效、并发和事务边界 |
| 管理 API、路由、响应 | `internal/server/handlers/`、`internal/server/router/` | `internal/server/resp/`、鉴权、中间件、`web/src/api/` |
| Relay、负载均衡、协议 | `internal/relay/`、`internal/protocolroute/`、`internal/transformer/` | Chat、Responses、Anthropic、流式/非流式、重试和计量路径 |
| 站点/渠道同步 | `internal/sitesync/` | 渠道模型、管理 API、同步报告和对应前端 |
| 价格与费用 | `internal/price/`、`scripts/updatePrice.py` | 文本、Responses、图片、输入估算和历史数据边界 |
| 三维统计与排行榜 | `internal/model/stats_leaderboard.go`、`internal/op/stats_leaderboard*.go` | `internal/relay/*metrics.go`、备份恢复、统计 Handler、Web 排行榜和覆盖提示 |
| Web API | `web/src/api/` | 后端响应契约、React Query 缓存键和错误/空状态 |
| Web 页面、状态、路由 | `web/src/components/modules/`、`web/src/stores/`、`web/src/route/`、`web/src/app/` | 三套 locale、桌面/窄屏、键盘、加载/空/错误状态 |
| 前端依赖 | `web/package.json`、`web/pnpm-lock.yaml` | `web/pnpm-workspace.yaml`、`Dockerfile.build`、CI pnpm 版本 |
| 生产构建 | `Dockerfile.build`、`scripts/build-production-image.sh` | OCI labels、固定摘要、源码 tree、前后端完整构建 |
| CI 与 Release | `.github/workflows/` | tag 触发、权限、GHCR、归档、治理 job |
| 生产声明 | `deploy/fwq57ys/compose.yaml`、`production-state.json` | 只在发布/部署各自阶段修改；不得把文本变更写成已部署 |
| 治理文档与守卫 | `AGENTS.md`、两份 Octopus 手册、`scripts/check-governance.sh` | README、USAGE、`CLAUDE.md` 的入口链接 |

### 发布与部署字段矩阵

当前流程分为三个提交/状态阶段，不得合并描述：

1. **应用源码与 Release**：功能源码 commit 经测试后创建新 tag；`Dockerfile.build` 通过
   `GIT_VERSION` 注入发布版本，因此 tag 所指源码中的默认版本字段可以仍是当前生产版本。
2. **部署 staging**：Release/GHCR 成功后，单独提交同步
   `internal/conf/version.go`、`web/package.json`、`web/src/lib/info.ts`、受管 Compose，
   并把 `production-state.json` 的目标 release/image 更新为新版本。此时 live 容器 ID、
   StartedAt 等字段仍是旧生产；`--repo` 应通过，`--live` 应因尚未切换而不通过。
3. **切换后运行证据**：后台切换成功后按 Docker inspect 更新 container ID、StartedAt、
   restart count、回滚快照等 live 字段，再提交运行状态；此时 `--repo` 和 `--live` 都应通过。

Release tag 指向应用源码 commit，不指向后续部署 staging commit。任何汇报必须标明当前处于
“源码/Release”“staging 待切换”还是“live 已核验”，不得把 staging 文件内容说成已部署。

## 通用开发流程

### 开始

```bash
cd /opt/octopus-mumu
scripts/check-governance.sh --repo
git status --short --branch
git fetch origin
git switch -c codex/<topic> origin/main
```

工作树不干净、目录不是规范源码、`origin/main` 无法确认时停止。不得新建第二个克隆规避问题。

### 实现

1. 一个分支只处理一个目标；先写或补能暴露问题的测试。
2. 只改“修改路由”列出的必要文件，不顺带搬运历史分支或旧版本实现。
3. 数据结构变化同时处理迁移、备份/恢复和旧数据兼容。
4. 协议变化覆盖流式、非流式、失败、重试和计量，不只测成功响应。
5. Web 变化复用现有组件与状态模式，同时处理加载、空、错误、窄屏和键盘操作。
6. 每个提交保持可审查；数据库、凭据、构建产物、缓存和候选数据不得入 Git。

### 推送与晋级

```bash
git push -u origin codex/<topic>
# 等待该 SHA 的 GitHub CI 全部成功并完成审查
git switch main
git merge --ff-only codex/<topic>
OCTOPUS_MAIN_PROMOTION=1 git push origin main
```

只允许普通快进。禁止 `--no-verify`、force push、移动公开 tag、rebase 已发布主线或把失败
CI 的 SHA 晋级。发布和部署另行授权，合并 `main` 不自动触发生产切换。

## 按改动类型验证

下表是最低门禁，不替代针对缺陷新增的测试。**执行位置按 `AGENTS.md` §0：下表所有构建与
测试命令一律禁止在本机开发工作站执行，只在 GitHub CI 或 fwq57ys 的固定版本容器内运行。**
唯一例外是 `scripts/check-governance.sh --repo`（纯文本/Git/JSON 检查，不编译不跑测试），
本机保持可执行以支撑 `.githooks/pre-push`。fwq57ys 宿主当前没有 `go` 命令；
`go: command not found` 不是通过证据，后端全量测试必须由 GitHub CI 或等价的固定 Go 1.25
环境完成。本机跑出的测试输出不得写入完成报告。

| 改动 | 最低服务器侧验证（CI 或 fwq57ys 容器） | 额外证据 |
| --- | --- | --- |
| 仅文档/治理脚本 | `bash -n scripts/check-governance.sh`；`scripts/check-governance.sh --repo` | 新旧路径搜索、主题分支 governance CI |
| 普通 Go 逻辑 | 相关 package 测试；`go test -buildvcs=false ./...` | 失败用例先失败、修复后通过；backend CI |
| 模型/迁移/备份 | `go test -buildvcs=false ./internal/db/... ./internal/op/...`；全量 Go 测试 | 旧库副本迁移、导入导出、`quick_check` |
| Relay/协议 | 相关 `internal/relay/`、`protocolroute/`、`transformer/` 测试；全量 Go 测试 | 受影响协议的流式/非流式和失败重试样例 |
| 三维统计 | `go test -buildvcs=false ./internal/op ./internal/relay ./internal/server/handlers`；全量 Go 测试 | 独立数据副本回填；模型/最终渠道/请求分组逐项对账 |
| Web/API | `pnpm install --frozen-lockfile`、`pnpm lint`、`pnpm build`（均在 `web/`） | 桌面和 320px/390px 窄屏、键盘、空/错误状态截图或记录 |
| 构建/依赖/Release | shell 语法、治理守卫、前后端全量、使用计划中的新版本做完整旁路镜像构建 | OCI version/revision/source tree/build time；无生产挂载 |
| Compose/状态清单 | `docker compose -f deploy/fwq57ys/compose.yaml config`、`--repo` | 获批部署后再运行 `--live` 并提交真实 inspect 指纹 |

UI 或协议改动不能只以“编译通过”验收；统计或迁移不能只以“新表存在”验收。

## 基础镜像来源

`Dockerfile.build` 的三个 `FROM`（node / golang / debian）都用 `docker.1ms.run` 镜像站加固定
摘要。运行时基础层自建于固定摘要的 `debian:bookworm-slim`，不 `FROM` 任何上游应用镜像。

不要把这三行改成 `docker.io/library/…`：`auth.docker.io` 与 `registry-1.docker.io` 从本地开发机、
fwq10ys、fwq57ys 都不可达，`docker.1ms.run/v2/` 三处都可达（401 是未认证的正常应答）。换回官方源
只有 GitHub runner 能拉，机器上就无法自建镜像。

镜像站偶发缺层会让 Release 失败（`could not fetch content descriptor … not found`）。先按摘要
复核 blob 可达性，确认是镜像站瞬时故障就 `gh run rerun --failed`，不要为此改摘要或换基础镜像。

需要在机器上留基础镜像时：先按摘要查本地是否已有（`docker images --digests` 或
`docker image inspect <repo>@<digest>`），已有就不动；缺的直接从 `docker.1ms.run` 按同一摘要拉，
不经开发机中转。拉完核对本地 image ID 与摘要对应。

## 价格与费用契约

价格单位为美元/百万 Token，当前统一规则为：

1. `actual_model_name` 有非零精确价格时，使用实际模型价格；
2. 实际模型缺价或只有全零占位时，回退到 `request_model_name` 的请求分组官方价格；
3. 请求分组明确为零价时保持零，不臆造价格；
4. 文本、Responses、输入估算兜底和图片路径必须使用同一规则；
5. 当前只有 `codex-auto-review`（无公开定价）和
   `sensenova-6.7-flash-lite`（免费方案）明确保留零价。

历史日志不会随价格表自动重算。价格数据刷新和历史费用回填是两个独立任务：前者只有显式
`UPDATE_PRICE_DATA=1` 才执行并必须先审查生成差异；后者需要生产快照、数据范围和审计，
不得通过直接写 SQLite 顺带完成。

## 明确禁止

- 禁止在 `main`、生产控制面、历史源码或 build-cache 中开发。
- 禁止从旧 tag、backup 分支、失败发布或隔离分支复制整套实现覆盖当前主线。
- 禁止直接修改生产 SQLite 来验证代码；候选只能挂独立一致性副本。
- 禁止 Handler 直接复制一套与 `internal/op/` 不一致的业务规则。
- 禁止协议转换器发明上游未要求的字段，或只凭单一供应商成功响应判定兼容。
- 禁止只更新一个版本字段、移动既有 tag 或用同名镜像覆盖旧构建。
- 禁止把 `latest`、上游基础镜像、Docker Hub 同名镜像或本地测试镜像当生产镜像。
- 禁止设置 `UPDATE_PRICE_DATA=1` 顺带刷新价格；价格更新必须独立审查差异。
- 禁止因宿主缺少 Go/pnpm 而跳过测试并把静态阅读写成验证通过。
- 禁止在本机开发工作站执行构建与测试（`go build/test/vet`、`docker build`、`pnpm
  install/lint/test/build`、旁路镜像构建）；也禁止把本机跑出的结果当验证证据。
  见 `AGENTS.md` §0。
- 禁止未获维护窗口授权时执行生产容器生命周期命令或生产数据写入。
- 禁止在当前代理 API 所依赖的前台 SSH 会话中 stop/restart/recreate Octopus。

## 已知缺陷与历史坑

| 现象 | 已确认原因 | 正确做法 | 禁止的错误处理 |
| --- | --- | --- | --- |
| 三维统计页面能显示，但渠道/分组维度总量远小于累计总表 | 新表缺完整历史回填，且漏掉了独立计量写入路径 | 三维（模型 / 最终渠道 / 请求分组）成功、失败、输入/输出 Token、费用逐项对账；coverage 必须 `completed`，超出可回填历史必须显式提示部分覆盖 | 只检查页面能显示，或只断言新表存在 |
| Responses 上游报 `unknown_parameter`，工具调用链中断 | 曾自动合成或重写 `function_call_output.item_reference` | 保留类型化 item ID 规范化，但 `item_reference` 仅按协议和真实输入透传，并用供应商兼容样例回归 | 为“补全”字段而发明引用值 |
| 前端 Docker 安装阶段找不到/拒绝构建原生依赖 | pnpm 版本或原生依赖许可文件未同步进入构建上下文 | 以 `web/package.json` 的 `packageManager` 为准；安装前复制 lockfile 和 `web/pnpm-workspace.yaml` | 在 Dockerfile 单独升级 pnpm，或删除 allowBuilds |
| 构建时价格表意外变化 | 设置了 `UPDATE_PRICE_DATA=1` 会刷新仓库价格数据 | 默认使用已提交价格；价格任务先审查和提交差异，再构建 | 发布功能时顺带刷新价格 |
| 代码、Web、Compose 或状态清单看似混合新旧值 | 应用源码、部署 staging 和 live 指纹是三个阶段 | 按发布与部署字段矩阵分阶段同步；staging 只声称“待切换”，切换后再写真实 inspect | 把 staging 状态说成已运行，或强行让 tag 指向部署提交 |
| HealthFirst 各档候选顺序在连续请求间不变；或不限并发渠道被 LeastUsed/P2C 持续偏爱 | 档循环内每档各调一次 `nextRotation`，多档偏移按同一序列推进互相抵消；`MaxConcurrency<=0` 直接 return 不计数，负载恒为 0 | 每请求只取一次轮换偏移供全部档共用（档间顺序仍是 Healthy > Degraded > Bad）；不限并发只跳过上限检查，计数照常 | 只断言「顺序合法」或「不限并发能放行」——顺序冻结与零计数都满足这类断言 |
| 健康度/熔断写进去的键读侧永远读不到 | 写侧用了客户端请求模型名，读侧按渠道映射后的上游模型名取 | 模型键唯一来源是 `ItemUpstreamModel`（`GroupItem.ModelName` 为空时退回请求模型名）；compact 路径每个候选先按自己的上游模型名改写请求体再上报。粘性会话是唯一例外，仍按请求模型名存取 | 用请求模型名做健康/熔断键，或只验证「上报没报错」 |
| 熔断相关改动被 `go vet` 判为复制锁，或测试里状态机不迁移 | `circuitEntry` 内嵌 `sync.Mutex`，按值构造初态即复制锁；冷却推进若直接改状态会绕过状态机 | 用不含锁的 `circuitEntrySeed` 构造初态，用 `rewindCircuitFailure` 回拨 `LastFailureTime`，状态迁移交给 `IsTripped` | 为了让测试通过而按值复制 `circuitEntry` 或手写状态 |
| 按数值假设 `FailureKind` 取值 | `circuit.go` 同一 `const` 块内 `CircuitState` 与 `FailureKind` 共用 `iota`，`FailureHard` 实际是 3 而非 0 | 只在同类型间比较，用常量名不用字面量；需要修可读性时单独拆 `const` 块 | 假设每个类型从 0 开始，或顺带改常量数值 |
| 新改动被误判为 `go vet` 回归 | `internal/relay/protocol_attempt.go:187` 早有 copy-lock 告警（预存，非本轮引入） | 单独记录基线告警和新增告警；当前任务仍须通过规定的 Go tests/CI | 把既有告警说成本轮修复，或用它解释所有失败 |

## 未解决的已知限制

以下为明确记录、暂不修改的限制，改动它们属于独立任务，需要单独确认：

- `relay/compact.go` 全程不调 `TryAcquireChannel` 与 `TryConsumeChannelRPM`：`/v1/responses/compact`
  不受渠道并发上限与 RPM 约束。补上属于准入语义变更。
- `outlierwindow` 健康数据是进程内存、不持久化：多实例部署各自持有独立健康视图，重启后冷启动。
  当前设计不引入外部健康存储，此项是部署前提而非缺陷。

## 停止条件

出现任一情况立即停止写入或晋级，先恢复事实：

- 当前目录不是唯一源码，或工作树有来源不明的修改；
- 目标不是从最新 `origin/main` 建立，或需要合并未审查的隔离进度；
- 测试无法运行且没有 GitHub CI/固定环境的等价证据；
- 版本、tag、source commit、OCI revision 或 source tree 不一致；
- 数据结构变化没有迁移、备份/恢复和旧数据验证；
- 统计三维不一致、coverage 未完成或部分覆盖未明确展示；
- 候选触碰生产数据、生产端口或生产容器名；
- 任务会触发生产生命周期/SQLite 写入但没有明确维护窗口；
- 没有可验证快照、后台切换脚本或自动回滚。

停止不是改用旧目录、旧镜像或本地重建绕过门禁。

## 交付证据

每次交接至少写清：

```text
任务与范围：
源码目录：/opt/octopus-mumu
分支 / HEAD / origin SHA：
当前 main / 运行应用源码：
运行镜像 tag / image ID / 容器 ID：
修改文件与修改理由：
新增或修改的测试：
实际执行命令与结果：
GitHub CI / Release URL：
是否部署：否 / 是（维护窗口与后台任务证据）
数据与容器操作：
快照 / 回滚：
明确未修改：
临时资源清理：
已知限制与停止项：
```

没有部署时必须明确写“未部署”，不能用“已合并”“Release 成功”代替。
