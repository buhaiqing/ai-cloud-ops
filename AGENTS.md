# AGENTS.md · ai-cloud-ops

> 项目级开发规范索引。**≤ 500 行硬上限**。细节放 `docs/dev-*.md`。
> 用户级规则见 `~/.pi/agent/AGENTS.md`（CP-1..6、karpathy、ponytail、GCL、self-improving）。
> 冲突时项目级优先。

---

## 0. 项目快照

- **业务**：AI-Native Alibaba Cloud 多账号运维控制台（告警 → AI 诊断 → Dashboard → 人工审批 → 执行）
- **后端**：Go 1.23+ / chi / pgx / cobra（Python 已停，6 个月兼容期）
- **前端**：Next.js 14 (app router) / TypeScript / Tailwind / vitest + happy-dom
- **DB**：PostgreSQL 16（alerts 月分区 / JSONB / pgcrypto）
- **里程碑**：M1 ✅ / M2 ✅ / M3+ ⏸（见 `TODOS.md`）

---

## 1. Contract-First 并行开发工作流 ⭐

> **M2 关键教训**：后端先做完再派前端 = 伪并行。真正的并行 = 契约先行。

### 1.1 流程

```
1. 写契约  →  audit-results/contract-{feature}.json
            (endpoint / request / response_200 / sql_queries / ts_type_unchanged)
2. 派工    →  后端 subagent (Go handlers + 测试)
            →  前端 subagent (页面 + 客户端 + 测试)
            两个 subagent 同时启动，concurrency ≤ 3
3. 集成    →  主 agent 实际跑 go test / tsc / 校验契约字段
4. 关闭    →  更新 TODOS.md + audit-results/{feature}-DELIVERY.md
```

### 1.2 契约文件最小 schema

```json
{
  "contract_id": "M{n}-{feature}",
  "version": "1.0",
  "endpoint": "GET|POST|... /api/v1/...",
  "auth": "public|session-required|csrf-required",
  "request":  { /* JSON schema or null */ },
  "response_200": { "type": "object", "required": [...], "properties": {...} },
  "sql_queries": { "name": "SELECT ..." },
  "frontend_contract": "import { X } from 'web/lib/api.ts' — 已导出",
  "ts_type_unchanged": true,
  "tdd_target": "N 个测试 case"
}
```

### 1.3 何时用 / 何时不用

| 场景 | 必走契约 |
|---|---|
| 跨前后端新功能（REST/WS endpoint + UI） | ✅ |
| 数据迁移 / schema 变更 | ✅（migration 本身就是契约） |
| 纯前端组件 / 纯后端 handler | 不需要 |
| 紧急 hotfix | 不需要 |

### 1.4 反模式

- ❌ "后端先做完再让前端动" — 浪费并行预算
- ❌ 契约只在 chat 里口头约定 — 必落盘
- ❌ 前后端 subagent 共享未写入契约的隐式约定

**完整 GCL / subagent 分派规则** → `docs/dev-workflow.md`

---

## 2. 后端 (Go) 速查

**详细规范** → `docs/backend-standards.md`

| 主题 | 规则 |
|---|---|
| Layout | `cmd/<bin>/main.go` + `internal/<pkg>/<file>.go` + `*_test.go` 同包同目录 |
| TDD | 表驱动 `for _, tc := range []struct{...}`；DB 测试用接口替身 |
| Handler | chi + `writeJSON(w, status, body)` 助手；DB down → 503 |
| 密码 | bcrypt；bad-user / bad-password 都走 CompareHashAndPassword（防 timing） |
| Session | HttpOnly + SameSite=Lax + Secure-when-TLS |
| CSRF | double-submit cookie + `X-CSRF-Token` header；state-changing 必校验 |
| SQL | 参数化 `$1,$2`；写必带 `ON CONFLICT`；月分区表查必带 `created_at` 范围 |
| 迁移 | 新增 `db/migrations/NNNN_xxx.sql`，**永不**改已 ship 的 |
| WS | 简单场景手写 RFC 6455（~50 行）替 `gorilla/websocket`；Hub `sync.RWMutex + map` |
| 提交前 | `go build ./...` && `go test -count=1 ./...` && `gofmt -l .` 全 0 |

---

## 3. 前端 (TS / Next.js) 速查

**详细规范** → `docs/frontend-standards.md`

| 主题 | 规则 |
|---|---|
| Layout | `app/<feature>/page.tsx` + `components/` + `lib/api.ts` + `tests/` |
| 命名 | 组件 PascalCase.tsx；libs camelCase.ts；testid `noun-action` |
| TS | `strict: true`；禁用 `any`；用 `@/...` 别名；客户端组件标 `'use client'` |
| Tailwind | `darkMode: 'class'`；mobile-first；颜色用 config 自定义 token |
| 数据 | server component 直 await；client useEffect+useState；3 态（loading/error/empty）必齐 |
| Auth | 走 `lib/auth.ts`；CSRF 从 `aico_csrf` cookie 读 |
| 测试 | vitest + happy-dom；mock `globalThis.fetch`；prefer `getByRole` / `getByTestId` |
| 提交前 | `cd web && npx tsc --noEmit` && `npx vitest run --no-coverage` 全 0 |
| 跑测试 | **必须在 `web/` 下**，否则 vitest 找不到 config |

---

## 4. TDD + GCL 速查

**详细** → `docs/dev-workflow.md`

- TDD：Red（写测试看失败）→ Green（最简实现）→ Refactor → Verify
- GCL：Generator = subagent / Critic = 主 agent
- Subagent 自报必含：文件列表 + 测试数 + pass/fail + 未完成项
- 主 agent 看到结果**必做**：实际跑测试（不只信自报）+ 契约字段对齐检查 + 修失败

---

## 5. 提交 / 分支

- 主分支：`main`（保护）
- Feature：`feature/<short-kebab>` 或 `fix/<short-kebab>`
- Commit：`<scope>: <imperative>`（如 `api: add rules CRUD handler`）
- 必带：测试 + 更新 `TODOS.md` 状态

---

## 6. 项目特定 gotchas

**完整列表** → `docs/gotchas.md`

最常见的 3 个：

- **dashboard 暴露云清单** → 必须过 M2-5 auth（`/api/v1/{ping,stats,auth/login,auth/logout,ws}` 之外）
- **LLM 调用慢** → `latency_ms` 必填；webhook 异步化不阻塞签名校验
- **multi-account 凭据** → 配置文件 `chmod 0600`；env var 兜底；运行时只读 STS 临时 token

---

## 7. 必须维护的文档

| 文件 | 何时更新 |
|---|---|
| `TODOS.md` | 每个 M 状态变化 |
| `audit-results/contract-*.json` | 新增跨端点契约 |
| `audit-results/M{N}-DELIVERY.md` | milestone 完成 |
| `audit-results/gcl-trace-*.json` | 每次 subagent 编排 |
| `docs/dev-workflow.md` | GCL/TDD 流程变更 |
| `docs/backend-standards.md` | Go 规范变更 |
| `docs/frontend-standards.md` | TS 规范变更 |
| `docs/gotchas.md` | 新踩坑时 |
| `docs/data-retention.md` | 合规要求变更 |
| `docs/kill-criterion.md` | AI 准确率门槛变更 |
| `design.md` | 架构级决策（不轻易动） |

---

## 8. 红线（禁止事项）

- ❌ 改已 ship 的 `db/migrations/NNNN_*.sql`（建新文件 N+1）
- ❌ `internal/agent` 暴露给 HTTP（agent 只能由 worker 调用）
- ❌ LLM raw response 透给未授权用户
- ❌ 跳过 `go test` / `npx tsc` 直接 commit
- ❌ 生产 DB 直接 DDL（必须先提交 migration）
- ❌ AccessKey / 私钥 commit 到 git
- ❌ 新增依赖不更新 `package.json` / `go.mod`
- ❌ AGENTS.md 超过 500 行（先 `wc -l` 再写）

---

**本文件 ≤ 500 行**。新规则前先 `wc -l`；> 400 谨慎，> 500 必精简 + 拆 `docs/`。
