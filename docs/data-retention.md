# T18 — Data Retention & Residency Policy

> **必读**：本文档定义哪些数据流向第三方（Claude / Qwen / DeepSeek）、保留多久、用户能不能删。合规层面对中国《数据安全法》《个人信息保护法》和 GDPR 都要交代。运营前必须明示。

---

## 1. 数据流分类

### 1.1 数据类别

| 类别 | 例子 | 敏感度 | 默认保留 |
|---|---|---|---|
| **凭证** | ALIYUN_ACCESS_KEY_ID, ALIYUN_ACCESS_KEY_SECRET, STS token | 🔴 高 | 仅运行时内存；**绝不入库 / 不发 LLM** |
| **告警 payload** | alert_id, severity, metric value, resource_id | 🟡 中 | 90 天后自动匿名化（删除 resource_id） |
| **资源元数据** | instance_name, tags, config | 🟡 中 | 30 天（cache TTL 已限） |
| **AI 响应** | root_cause, recommendations, evidence_chains | 🟢 低 | 永久（项目终止时清空） |
| **Prompt** | 输入的 alert + 工具调用 | 🟢 低 | 仅 eval 模式保留 30 天 |
| **审计日志** | 谁在什么时间看了哪条 alert | 🔴 高 | 365 天 |

### 1.2 流向第三方 LLM 的数据

**默认开启**：
- ✅ Alert payload（去 AccessKey/SecurityToken 字段）
- ✅ Resource metadata（实例名/标签/状态）
- ✅ Prompt 模板版本号 + 模型 ID

**默认关闭（opt-in）**：
- ❌ 完整原始 CloudMonitor payload（含可能的业务字段如订单量）
- ❌ Alert 描述中的业务注释（如果 OSS 用户填了）
- ❌ Eval 样本回传（默认只跑本地 eval，不上传）

**绝不发送**：
- 🚫 任何凭证字段（AccessKey / SecurityToken / STS / password）
- 🚫 用户在 prompt 里贴的 .env 内容
- 🚫 阿里云 RAM 角色 trust policy 详情

---

## 2. 保留期限

| 数据 | 保留 | 删除触发 |
|---|---|---|
| 凭证（.env） | 不入库；本地文件 | `chmod 0600` 用户自己删 |
| STS Token | 内存（最长 1 小时） | 自动过期 |
| 告警 (`alerts` 表) | **90 天** | cron job 每天清超过 90 天的行 |
| 资源元数据 (`resources` 表) | **30 天** | 与 TTL cache 同步 |
| AI 分析结果 (`analyses` 表) | **永久** | 项目终止时全表删除 |
| Prompt eval samples | **30 天** | 仅当 `AI_EVAL_MODE=true` 时记录 |
| DLQ 任务 (`dlq` 表) | **14 天** | 解决后保留 14 天便于复盘 |
| 结构化日志 | **30 天** | logrotate |
| Worker heartbeat | **7 天** | 仅用于 ops |

---

## 3. 用户控制（opt-in / opt-out / delete）

### 3.1 配置项（`.env`）

```bash
# 主开关：是否记录 AI eval 样本到本地（用于 T9 CI gate）
AI_EVAL_MODE=true   # 默认 true；设为 false 不记录任何 eval 数据

# 是否向 LLM 发送原始 alert payload（含业务字段）
AI_SEND_RAW_PAYLOAD=false   # 默认 false；只发结构化字段

# 数据保留天数（覆盖默认）
DATA_RETENTION_ALERTS_DAYS=90
DATA_RETENTION_LOGS_DAYS=30
```

### 3.2 删除自己的数据

OSS 用户随时可以：
```bash
# 删除所有 alert / analyses / logs
docker compose exec backend python -c "
from ai_cloud_ops.db import get_session
import asyncio
async def nuke():
    async with get_session() as s:
        await s.execute('TRUNCATE alerts, analyses, resources, dlq, worker_heartbeat CASCADE')
        await s.commit()
asyncio.run(nuke())
"
```

### 3.3 用户级数据隔离（OSS 单租户模式）

- **单实例部署**：一个 Docker Compose = 一个"租户"的所有数据；用户自己负责备份和删除
- **数据绝不跨账号**：`ACCOUNTS_CONFIG_PATH` 配置的多个阿里云账号属于同一用户，AI 上下文可以跨账号（用户明确授权）；**绝不**与 OSS 其他用户共享数据

---

## 4. 合规声明

### 4.1 中国《数据安全法》《个人信息保护法》

- ✅ 不收集个人信息（PII）——告警数据是 IT 运维元数据，不是自然人信息
- ✅ 数据境内处理（OSS 用户部署在自己的阿里云账号内）
- ⚠️ 如果用户配置跨账号 RAM 角色（多账号），需要确认所有账号在同一地域 / 同一合规域
- ❌ 不向境外传输（除非用户主动配 Claude / OpenAI；本项目默认推荐国内模型 Qwen/DeepSeek）

### 4.2 GDPR（仅供国际用户参考）

- ✅ 用户可访问 / 修改 / 删除自己的数据（见 3.2）
- ✅ 数据最小化原则：只向 LLM 发必要字段
- ⚠️ "数据处理者"角色：OSS 项目方是 processor，OSS 用户是 controller
- ❌ 项目方不收集任何 telemetry（默认 `TELEMETRY=off`）

### 4.3 阿里云用户协议

- 通过 STS AssumeRole 调用 OpenAPI，权限最小化（每个账号只授予读权限）
- 不调用阿里云 ActionTrail 之外的服务
- 不向阿里云上传任何数据（除了 OpenAPI 调用本身）

---

## 5. 第三方 LLM Provider 政策

| Provider | 数据中心 | 训练使用 | 推荐场景 |
|---|---|---|---|
| **Claude (Anthropic)** | 美国 | API 数据默认 30 天后删除（ToS） | 海外用户 / 结构化诊断最强 |
| **OpenAI GPT-4** | 美国 | API 数据 30 天后删除（ToS） | 备选 |
| **Qwen (通义千问)** | 中国（杭州） | 不确定（ToS 没明说） | 国内合规首选 |
| **DeepSeek** | 中国 | 不确定（ToS 没明说） | 国内成本最低 |

**默认**：推荐配置 Qwen 作为国内用户的 default provider；OSS demo 可让用户切换。

---

## 6. 应急响应（数据泄露）

如果发现凭证泄露 / 日志污染 / 第三方违规：

```markdown
- [ ] 立即 rotate `ALIYUN_ACCESS_KEY_ID/SECRET`
- [ ] 撤销所有 STS Role 的 trust policy
- [ ] 暂停 worker（docker compose stop backend）
- [ ] 检查阿里云 CloudTrail 看异常 API 调用
- [ ] 通知用户（如有活跃外部用户）
- [ ] 写 post-mortem → docs/postmortems/YYYY-MM-DD-data-leak.md
```

---

**最后更新**：M1 完（T+4 周）+ MCP checkpoint 完（T+5 周）