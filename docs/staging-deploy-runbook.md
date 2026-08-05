# Staging Deploy Runbook — REVIEW-1 (5 真实用户试用)

> 目的：把 `ai-cloud-ops` 部署到一个**真实** staging 环境，让 5 个运维同事
> 在 1 周内试用、反馈 REVIEW-1 关心的"AI 诊断是否真的可用"问题。

## 0. 前置条件（缺一不可）

| 项 | 来源 | 备注 |
|---|---|---|
| 阿里云账号 × ≥1（生产/预发均可） | 你 / Ops 同事 | 该账号给 ai-cloud-ops 只读 + STS AssumeRole 权限 |
| 阿里云 RAM Role | 你 | `acs:ram::${account_id}:role/ai-cloud-ops`，权限模板见 §5 |
| `ANTHROPIC_API_KEY` | Anthropic 团队 | 推荐 `claude-sonnet-4-5`，详见 `.env.example` |
| 服务器 × 1（≥ 2 vCPU, ≥ 4 GB RAM） | 任意云 / 物理机 | OS: Ubuntu 22.04 LTS；公网 IP；TCP 80/443/8080/8081 |
| 域名 + TLS 证书（可选） | 你 | 没域名直接走 IP + 自签证书也行 |
| 5 个试用用户邮箱 | 你 | 详见 [`user-onboarding.md`](./user-onboarding.md) §1 |

**没有阿里云凭证 / `ANTHROPIC_API_KEY` → 不要启动部署**。会卡在 webhook 签名校验 + AI 诊断空响应上。

## 1. 准备服务器（Ubuntu 22.04）

```bash
# 1.1 装基础
sudo apt update && sudo apt install -y docker.io docker-compose-plugin git make

# 1.2 把当前用户加入 docker 组（避免每次 sudo）
sudo usermod -aG docker $USER
newgrp docker

# 1.3 验证
docker --version
docker compose version
git --version
```

## 2. 拉代码 + 准备配置

```bash
# 2.1 拉代码（用 SSH key 或 PAT）
git clone git@github.com:YOUR_ORG/ai-cloud-ops.git /opt/ai-cloud-ops
cd /opt/ai-cloud-ops

# 2.2 切到待试用 commit
git checkout <PINNED_COMMIT>

# 2.3 复制 env 模板 + 写入真实凭证
cp .env.example .env
chmod 0600 .env
$EDITOR .env   # 必填：ALIYUN_ACCESS_KEY_ID / SECRET / ACCOUNTS / REGIONS /
               #       ANTHROPIC_API_KEY / POSTGRES_PASSWORD / WEBHOOK_SIGNING_SECRET

# 2.4 复制阿里云账号配置
cp config/accounts.yaml.example config/accounts.yaml
$EDITOR config/accounts.yaml  # 把 role_arn / regions 替换为 staging 账号的实际值
```

### 2.3 .env 关键字段说明（节选 `.env.example`）

| 字段 | 必填 | 示例 / 说明 |
|---|---|---|
| `ALIYUN_ACCESS_KEY_ID` | ✅ | 仅用于 STS AssumeRole，**绝不**直连阿里云 API |
| `ALIYUN_ACCESS_KEY_SECRET` | ✅ | 同上 |
| `ALIYUN_ACCOUNTS` | ✅ | `{"prod":"acs:ram::123:role/ai-cloud-ops"}` |
| `ALIYUN_REGIONS` | ✅ | `{"prod":["cn-hangzhou","cn-beijing"]}` |
| `ANTHROPIC_API_KEY` | ✅ | `sk-ant-...` |
| `POSTGRES_PASSWORD` | ✅ | ≥ 16 字符随机 |
| `WEBHOOK_SIGNING_SECRET` | ✅ | 阿里云 CloudMonitor 联系单配置 webhook 时要填这个 |
| `EXEC_RATE_LIMIT` | ⚙️ | staging 默认 10/h；想放大改 100 |
| `AICO_ROLLBACK_ENABLED` | ⚙️ | 默认 `false`；试用想测回滚改 `true` |

## 3. 启动服务（Docker Compose）

```bash
# 3.1 启动（后台）
docker compose up -d

# 3.2 看启动日志（30 秒内应看到 postgres healthy + redis healthy + backend started）
docker compose logs -f --tail=50 backend

# 3.3 看健康检查
curl -fsS http://localhost:8081/healthz   # → {"status":"ok"} or 503 if not ready
curl -fsS http://localhost:8081/readyz    # → {"status":"ready"} or 503 if deps missing

# 3.4 跑迁移（compose init.sql 自动跑完，可手动验证）
docker compose exec postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    -c "\dt" | grep -E '^( accounts| resources| alerts| analyses| alert_rules| exec_plans)$'
```

**预期输出**：`accounts`, `resources`, `alerts`, `analyses`, `alert_rules`, `exec_plans` 共 6 张表 + 月分区。

## 4. 启动 Web Dashboard

```bash
# 4.1 装前端依赖
cd web
npm ci

# 4.2 改 .env.local 指后端（默认是 localhost，可改）
cat > .env.local <<EOF
NEXT_PUBLIC_API_BASE=http://<SERVER_IP>:8080
NEXT_PUBLIC_WS_BASE=ws://<SERVER_IP>:8080
EOF

# 4.3 build + 启动（用 PM2 / systemd 二选一）
npm run build
PORT=3000 npm start &     # 开发用；生产见 §6
cd ..
```

打开浏览器访问 `http://<SERVER_IP>:3000`，应该看到登录页（详见 §5 auth 准备）。

## 5. 阿里云侧准备（一次性）

### 5.1 RAM Role 权限模板（最小权限）

把以下策略绑定到 `ai-cloud-ops` role（**只读 + 必要的执行权限**）：

```json
{
  "Version": "1",
  "Statement": [
    { "Effect": "Allow", "Action": [
        "ecs:Describe*",
        "rds:Describe*",
        "slb:Describe*",
        "vpc:Describe*",
        "oss:GetBucket*", "oss:ListBuckets",
        "actiontrail:LookupEvents"
      ],
      "Resource": "*"
    },
    { "Effect": "Allow", "Action": [
        "ecs:RebootInstance",
        "rds:RestartDBInstance",
        "slb:RemoveBackendServers", "slb:AddBackendServers"
      ],
      "Resource": "*",
      "Condition": { "StringEquals": { "acs:RequestTag/aico-managed": "true" } }
    }
  ]
}
```

**说明**：
- 第一段只读，给 AI Agent tool whitelist 用。
- 第二段执行权限（M3-5 WRITE_TOOLS）打 tag 条件，避免误操作未打标资源。

### 5.2 CloudMonitor Webhook 配置

1. 阿里云控制台 → CloudMonitor → 报警服务 → webhook 集成
2. URL：`https://<你的域名或 IP>/webhook/cloudmonitor`
3. 签名密钥：填 `WEBHOOK_SIGNING_SECRET` 的值
4. 触发条件：选 `severity ∈ {critical, warning}` 的所有规则（避免告警风暴）

## 6. 生产化（可选，但试用建议做）

`docker compose up` + `npm start &` 适合内网试用；要公网试用（5 个用户远程访问）：

| 组件 | 推荐方案 |
|---|---|
| 后端 | systemd unit 跑 `docker compose up -d` + `Restart=always` |
| 前端 | Nginx 反代 `localhost:3000` + Let's Encrypt TLS |
| HTTPS | Nginx TLS termination + 后端走 HTTP（内网） |
| 监控 | Prometheus 抓 `/metrics` + Grafana dashboard（暂未提供，see §8） |

最小 Nginx 反代片段：

```nginx
server {
    listen 443 ssl;
    server_name aico.example.com;
    ssl_certificate /etc/letsencrypt/live/aico.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/aico.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:3000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }

    location /api/ { proxy_pass http://127.0.0.1:8080; }
    location /webhook/ { proxy_pass http://127.0.0.1:8080; }
    location /ws { proxy_pass http://127.0.0.1:8080; proxy_http_version 1.1; proxy_set_header Upgrade $http_upgrade; proxy_set_header Connection "upgrade"; }
}
```

## 7. 冒烟测试 checklist（部署完必跑）

跑完下面 8 步 = staging 启动成功，可分发账号给用户：

```bash
# 7.1 后端健康
curl -fsS http://localhost:8081/healthz    # → ok
curl -fsS http://localhost:8081/readyz     # → ready

# 7.2 DB 可达
docker compose exec postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" -c "SELECT 1"

# 7.3 创建 1 个试用账号（用 admin CLI，先手动开）
# 详见 docs/admin-cli.md（待写；现阶段通过 SQL 直接插 bcrypt 密码 hash）

# 7.4 模拟 1 条 webhook
curl -X POST http://localhost:8080/webhook/cloudmonitor \
     -H "Content-Type: application/json" \
     -H "X-Alico-Signature: <按 WEBHOOK_SIGNING_SECRET 算的 HMAC-SHA256>" \
     -d '{"alert_id":"smoke-test-1","severity":"critical","resource_type":"ECS","resource_id":"i-test","name":"smoke test","created_at":"2026-08-05T10:00:00Z","payload":{}}'

# 7.5 看 alert 是否入库
docker compose exec postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    -c "SELECT alert_id, severity, status, created_at FROM alerts WHERE alert_id='smoke-test-1'"

# 7.6 等 30 秒看 analysis 是否出来（取决于 ANTHROPIC_API_KEY 可达性）
docker compose exec postgres psql -U "$POSTGRES_USER" -d "$POSTGRES_DB" \
    -c "SELECT id, model, latency_ms, root_cause FROM analyses ORDER BY created_at DESC LIMIT 1"

# 7.7 Web 登录页可达
curl -fsS http://localhost:3000/login

# 7.8 WS 端点可达（用 wscat 或浏览器 devtools）
wscat -c ws://localhost:8080/ws   # 或浏览器看 Network → WS 帧
```

**任意一步 ❌ → 修完再发账号**，不要让用户在坏环境里产生负面印象。

## 8. 试用期间监控

| 信号 | 阈值 | 动作 |
|---|---|---|
| `/readyz` 长时间返回 503 | > 5 min | 重启 `docker compose restart backend` |
| DLQ 表新增行 | > 0 行持续 30 min | 看 `docker compose logs backend \| grep ERROR` |
| `analyses.latency_ms` P95 | > 60s | 切到 `claude-haiku-4-5` 或加并发 worker |
| `EXEC_RATE_LIMIT` 429 爆发 | > 5/h | 把 `EXEC_RATE_LIMIT` 调大 |
| 试用用户反馈 | 任何 | 入 [`user-onboarding.md`](./user-onboarding.md) §4 反馈表 |

## 9. 试用结束（回收）

```bash
# 9.1 停服务
docker compose down

# 9.2 保留数据（如果用户同意）/ 删除（如果协议不允许留）
docker volume ls | grep postgres-data   # 记下 volume 名
docker volume rm ai-cloud-ops_postgres-data   # 物理删除

# 9.3 撤销阿里云凭证（强烈建议）
# RAM 控制台 → Users → AccessKey → Disable
# RAM 控制台 → Roles → ai-cloud-ops → Delete

# 9.4 关 webhook
# CloudMonitor 控制台 → 报警服务 → webhook 集成 → 删除
```

## 10. 已知陷阱（部署必看）

| 坑 | 症状 | 解法 |
|---|---|---|
| `POSTGRES_PASSWORD` 留空 | compose 启动直接退出 | 必须设置 ≥ 8 字符 |
| `chmod 0644 .env` | 凭证文件可被同机其他用户读 | `chmod 0600 .env` 强制 |
| `WEBHOOK_SIGNING_SECRET` 跟阿里云对不上 | 所有 webhook 401 | 重新生成，对齐两边 |
| `ALIYUN_ACCOUNTS` 是空 JSON `{}` | worker 启动后无账号可轮询 | 至少 1 个账号 |
| 后端连不上 postgres | `/readyz` 返回 503 | 检查 `POSTGRES_*` env + `depends_on: postgres_healthy` 是否生效 |
| ANTHROPIC_API_KEY 无效 | `analyses` 表永远空 | 看 `docker compose logs backend \| grep anthropic` |
| webhook 端口被占用 | `bind: address already in use` | 改 `WEBHOOK_PORT=8082` 等 |
| 没装 `npm ci` 直接 `npm start` | 前端启动失败 / 404 静态资源 | 必须先 `cd web && npm ci` |
| 没建 admin 账号 | 用户登录返回 401 | 详见 §7.3（待写 admin-cli.md） |

---

**文档版本**：v1.0 · 最后更新 T+7 周
**关联**：[`user-onboarding.md`](./user-onboarding.md) · [`kill-criterion.md`](./kill-criterion.md) · [`data-retention.md`](./data-retention.md)