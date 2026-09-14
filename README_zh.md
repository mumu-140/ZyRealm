<div align="center">

<img src="web/public/logo.svg" alt="ZyRealm 自由界" width="108" height="108">

# ZyRealm · 自由界

**自适应 LLM 路由网关**

面向多供应商、多凭据与多协议流量的稳定控制平面。

[English](README.md) · [快速开始](USAGE_zh.md) · [Releases](https://github.com/mumu-140/ZyRealm/releases)

</div>

> **项目来源与归属说明**
>
> ZyRealm（自由界）**基于 [bestruirui/octopus](https://github.com/bestruirui/octopus) 开发和持续演进**，继续遵循 Octopus 原有的 AGPL-3.0 许可证。本仓库在 relay 路由、供应商恢复、凭据调度、协议兼容、可观测性和生产运维方面已经进行了大量独立扩展，但不会删除上游项目的版权、许可证和来源说明。

## ZyRealm 是什么？

ZyRealm 是一个自适应 LLM 网关。它的目标不是单纯“把多个 API 放到一起”，而是在客户端协议和模型名保持稳定的前提下，根据实时状态、安全边界和失败语义选择合适的上游路径。

核心链路可以概括为：

```text
请求
 ↓
Provider 候选
 ↓
Credential 可用性
 ↓
Capability 兼容性
 ↓
Runtime 恢复 / 冷却
 ↓
公平调度
 ↓
RoutingDecision
 ↓
真实上游请求
```

## 为什么从 Octopus 演进出 ZyRealm？

Octopus 提供了优秀的聚合、协议转换和管理基础。ZyRealm 保留这些基础能力，并把主要工程投入放在真实多供应商场景下最容易出问题的部分：故障分类、切换安全、凭据公平性、协议兼容和恢复策略。

主要扩展包括：

- **自适应失败路由**：按失败语义区分 Provider、Provider×Model、Credential、Capability 与 Request 级故障。
- **Replay Safety**：区分“根本没有发出”与“已经可能发出但结果未知”，减少重复执行和重复计费。
- **凭据公平调度**：同一 Provider 内多凭据公平使用，不再用历史成本充当调度器。
- **Capability Negative Cache**：短期记住某个 Provider/Model/协议对特定请求形状不兼容，避免重复踩坑。
- **Retry-After 恢复**：识别上游恢复时间提示，并进行有上限的等待和冷却。
- **Anthropic 兼容增强**：对 HTTP 200 假成功、MessageContent schema 不兼容等情况进行语义校验和安全 failover。
- **逐次尝试追踪**：真实 wire attempt 会记录 RoutingDecision、失败域和恢复动作。
- **可配置请求预算**：单请求真实上游尝试上限可配置到 20，同时保留独立的 Provider budget 与 unknown-outcome replay guard。
- **站点 / 渠道 / 模型 / 分组 / 价格管理**。
- **OpenAI Chat / Responses / Images、Anthropic Messages 与相关 WebSocket relay 支持**。

## 新 UI

ZyRealm 的界面定位为“LLM Routing Control Plane”，而不是普通后台模板：

- 桌面端固定路由侧栏，移动端保留紧凑底部导航；
- ZyRealm / 自由界独立品牌、边界式 Logo 与冷蓝青视觉系统；
- Provider、Channel、Group、Model、Log、Setting 信息层级更明确；
- 可靠性配置直接进入控制平面；
- 支持暗色 / 亮色主题；
- 支持管理员登录和 API Key 登录模式。

## 快速开始

### Docker / GHCR

生产或长期运行建议使用不可变版本标签，不使用 `latest`：

```bash
docker pull ghcr.io/mumu-140/zyrealm:<version>
```

GitHub 仓库正式名称为 **`mumu-140/ZyRealm`**，后续正式版本统一发布到 **`ghcr.io/mumu-140/zyrealm`**。历史镜像仍可能保留在旧的 `ghcr.io/mumu-140/octopus-concurrency` 包路径下。

最小本地配置：

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8080
  },
  "database": {
    "type": "sqlite",
    "path": "data/data.db"
  },
  "log": {
    "level": "info"
  }
}
```

全新安装的默认账号密码为 `admin` / `admin`，首次登录后应立即修改。

### 本地开发

```bash
cd web
pnpm install --frozen-lockfile
NEXT_PUBLIC_API_BASE_URL="http://127.0.0.1:8080" pnpm run dev

# 另一个终端，在仓库根目录
go run . start
```

## 支持的客户端协议

ZyRealm 可以通过统一模型分组向客户端提供：

- OpenAI Chat Completions；
- OpenAI Responses；
- OpenAI Images；
- Anthropic Messages；
- 部分工具工作流所需的 WebSocket relay。

在兼容范围内，网关可以进行协议转换，并在某个供应商不支持特定请求结构时继续选择其他可用路径，而不要求客户端更换模型名。

## 路由安全边界

当前有三个不同概念的预算，不能混为一谈：

1. **Provider budget**：限制一个客户端请求最多进入多少个不同供应商。
2. **Wire-attempt budget**：限制真实发送到上游的次数，可配置，硬上限为 20。
3. **Unknown-outcome replay budget**：对“上游可能已经执行但结果未知”的跨 Provider 重放保持严格限制，降低重复执行风险。

Disabled、runtime cooldown、并发已满、RPM 已满、circuit 不可用等本地 skip 没有真正发送上游，因此不会消耗 wire attempt。

## 为什么代码里还有 Octopus？

这是刻意保留的兼容边界。当前仍可能看到：

- 上游继承的 Go module/import path；
- `OCTOPUS_*` 环境变量；
- 数据库 / migration 中的历史兼容命名；
- 某些历史协议示例和迁移记录。

这些名字如果机械替换，可能导致配置、构建、迁移或已有部署失效。因此：

**产品品牌、GitHub 仓库名和新镜像命名空间都统一为 ZyRealm / 自由界；Octopus 名称只在需要兼容或 attribution 的位置保留。**

## 许可证与上游

ZyRealm 基于 Octopus 开发，继续采用 **GNU AGPL-3.0**。完整条款见 [LICENSE](LICENSE)。

上游项目：[bestruirui/octopus](https://github.com/bestruirui/octopus)

本项目不会把上游 Octopus 代码声明为自己的原创；本仓库负责维护基于其上继续开发的差异和新增能力。
