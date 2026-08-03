# AI-Native Alibaba Cloud Multi-Account Ops Console

> **🚧 Go rewrite in progress (T+5 周).** Python 实现保留在 `pyproject.toml` / `src/ai_cloud_ops/` 作为参考；**新功能用 Go**。
> 详细迁移计划：[`docs/go-migration.md`](./docs/go-migration.md)。

## 项目目标

开源一个 AI 增强的多账号阿里云运维控制台，让告警从"阈值触发"升级为"AI 诊断"。差异化点：AI 深度参与 + 多账号聚合 + 告警规则 AI 提议。

## 快速开始（Go 版）

```bash
# 1. 构建
go build -o bin/aico ./cmd/aico
go build -o bin/aico-mcp ./cmd/aico-mcp

# 2. 配置
cp .env.example .env && chmod 0600 .env
cp config/accounts.yaml.example config/accounts.yaml

# 3. 启动 worker + webhook
docker compose up -d
./bin/aico serve  # 启动 worker + webhook
```

## MCP Server

```bash
./bin/aico-mcp   # stdio transport
```

## 架构

详见 [`design.md`](./design.md) + [`docs/architecture.md`](./docs/architecture.md)。

## 安全警告

⚠️ **绝不要把 `.env` commit 进 git。** `chmod 0600 .env`。