# CLAUDE.md

> **强制入口**：开始工作前必须先读取根目录 `AGENTS.md`——本仓库唯一真值（目录职责、生产身份、
> Git 规则、停止条件）都在那里。本文件只做路由，不重复规则。

## 加载路由

| 目的 | 读 |
| --- | --- |
| 一切规则（唯一真值 / 会话边界 / Git / 构建发布 / 生产数据边界）| `AGENTS.md` |
| 开发与分支治理、最低验证、禁止事项、已知回归 | `docs/octopus-development-governance.md` |
| 候选、备份、后台切换、回滚 | `docs/octopus-production.md` |
| 机器可读生产真值（镜像 tag / compose / 运行指纹）| `deploy/fwq57ys/production-state.json` |
| 部署前校验 | `scripts/check-governance.sh --repo`；fwq57ys 上核对线上用 `--live` |

## 红线（详见 AGENTS.md）

- 禁止在本机执行任何构建与测试（`go build/test/vet`、`docker build`、`pnpm
  install/lint/test/build`、旁路镜像构建）；验证只在 GitHub CI 或 fwq57ys 固定版本容器内做。
  唯一例外是 `scripts/check-governance.sh --repo`（纯文本/Git 检查）。见 `AGENTS.md` §0。
- 生产 compose/container 生命周期命令需明确维护窗口授权；发布完成 ≠ 已部署。
- 数据只挂载 `/opt/octopus/data`；生产 SQLite 不用于开发测试。
- 禁止 force push、`--no-verify` 绕过 `.githooks`。
