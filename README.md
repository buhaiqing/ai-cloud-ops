# AI-Native Alibaba Cloud Multi-Account Ops Console

> **OSS 早期阶段 — Day 1-2 脚手架。** 完整设计见 [design.md](./design.md)。M1 排期：4 周（M1 已从原 2 周调整为 4 周）。

## 项目目标

开源一个 AI 增强的多账号阿里云运维控制台，让告警从"阈值触发"升级为"AI 诊断"。差异化点：AI 深度参与（不是装饰）+ 多账号聚合（CloudMonitor 做不到）+ 告警规则 AI 提议。

## 当前进度

- [x] 设计文档（`design.md`）—— APPROVED via `/office-hours`
- [x] 架构评审 —— CLEARED via `/plan-eng-review` (14 issues + 4 cross-model tensions 全部 resolved)
- [ ] **M1 实现** —— Day 1-2 脚手架（当前阶段）
- [ ] M2 Web Dashboard
- [ ] M3 AI Agent 增强
- [ ] M4 高级功能（webhook、规则推荐、自动修复…）

## 快速开始（计划中，M1 完成前不工作）

```bash
# 1. 安装依赖
uv sync

# 2. 配置凭证（参见 .env.example）
cp .env.example .env
chmod 0600 .env
# 编辑 .env 填入阿里云凭证

# 3. 启动
docker compose up -d
uv run ai-cloud-ops analyze --account my-account --region cn-hangzhou
```

## MCP Server（REVIEW-3 / post-M1 checkpoint）

除了 Web Dashboard（Milestone 2），项目也支持 MCP 协议——你可以在 Claude Desktop / Cline / Cursor 里直接对话操作阿里云资源。

```bash
# 启动 MCP server (stdio transport)
uv run ai-cloud-ops-mcp
```

然后在 Claude Desktop 配置里加：

```json
{
  "mcpServers": {
    "ai-cloud-ops": {
      "command": "uv",
      "args": ["--directory", "/path/to/ai-cloud-ops", "run", "ai-cloud-ops-mcp"],
      "env": {"ALIYUN_ACCESS_KEY_ID": "...", "ANTHROPIC_API_KEY": "..."}
    }
  }
}
```

可用工具：

| 工具 | 用途 |
|---|---|
| `diagnose_alert(alert_id, region, account_alias)` | AI Agent 诊断告警 |
| `list_recent_alerts(region, account_alias, hours_back)` | 查询最近告警 |
| `describe_ecs_instances(region, account_alias)` | 列出 ECS |
| `describe_rds_instances(region, account_alias)` | 列出 RDS |
| `describe_slb_load_balancers(region, account_alias)` | 列出 SLB |
| `list_accounts()` | 显示已配置账号 |

**A/B test**：M2 是继续做 Next.js Dashboard，还是把 MCP 作为主要界面？这个选择会基于早期用户的反馈决定。详见 [`TODOS.md`](./TODOS.md) § M4-12。

## 关键技术决策

| 维度 | 选择 | 来源 |
|---|---|---|
| 凭证管理 | RAM Role + STS（进程内 LRU 缓存，TTL 2700s，提前 5min 续期） | T1 |
| AI Agent 工具 | 白名单只读工具（10+ Describe/Status/Metric/Tag），execute 独立审计 | T2 |
| 区域选择 | per-account 区域列表 + endpoint 静态字典 | T3 |
| 告警源 | CloudMonitor EventSubscription webhook（不轮询）+ alert_id UNIQUE | T4 |
| 凭证存储 | .env + .env.example + chmod 0600 + README 安全警告 | T5 |
| 错误处理 | 指数 backoff + DLQ + Prometheus /metrics | T6 |
| Worker 可靠性 | Docker restart: always + /healthz + JSON 日志 | T7 |
| 测试策略 | 三层金字塔（unit / integration / E2E） | T8 |
| AI 评估 | 10-15 告警 eval 集 + LLM-as-judge + CI gate | T9 |
| 资源缓存 | 进程内 TTL 缓存（5-15min）+ 主动 invalidate | T10 |
| DB schema | DDL + 关键复合索引 + alerts 按月分区 | T11 |
| **AI 质量基线** | **M1 第一周定义 — 见 [docs/ai-quality.md](./docs/ai-quality.md)** | **T13** |

## 安全警告

⚠️ **绝不要把 `.env` commit 进 git。** OSS 项目方有责任示范安全姿态。

```bash
# 推荐权限
chmod 0600 .env
```

## License

TBD (默认 Apache 2.0？)