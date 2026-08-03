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

## 1. 核心方法论 ⭐⭐⭐

> **重要规则（不可违背）**：任何开发任务必须遵循以下三点。

### 1.1 **Use TDD**
- 测试先行：先写失败测试 → 看到 RED → 写最小实现过测试（GREEN）→ 重构
- 提交前必跑：`go test -race ./...` + `npx tsc --noEmit` + `npx vitest`，全 0 才允许 commit
- 并发路径必加并发测试（详见 `docs/gotchas.md §A`）

### 1.2 **Use subagents**
- 多文件 / 多包 / 多阶段任务 → fan out（concurrency ≤ 3）
- 默认**不扇出**：单文件 / 单测 / 单步操作主 agent 直接做
- 派工前必跑 preflight（详见 `docs/subagent-playbook.md`）
- subagent 完成主 agent **必须独立验证**（不信任自报）

### 1.3 **Use GCL** (Generator-Critic-Loop)
- Generator = subagent（写代码 + 写测试）
- Critic = 主 agent（独立跑测试 + tsc + 契约对齐 + 修复失败）
- 任何 generator 输出必经过 critic 审查才 claim done
- 失败时重派或细化 prompt；不轻易放过

---

## 2. Karpathy 工程准则 ⭐⭐

> 源头：用户级 `~/.pi/agent/AGENTS.md` §karpathy-guidelines。本节是项目场景下的简短复述。

### 2.1 想清楚再写
- 不瞎猜；不确定就问；多种理解都摆出来
- 显式说出假设；哪里不清楚就停下来
- 该反对就反对（有更简单的就直说）

### 2.2 简单优先
- 用最少的东西解决问题，**不**做没要求的事
- 不为一次性任务搭通用框架；不加"未来可能用"的灵活性
- 交付完自问：资深的人会不会觉得过度复杂？会 → 砍
- 避免抽象成瘾：3 个相似 case 再考虑抽象，1 个就 hardcode

### 2.3 外科手术式改动
- 只动你必须动的；只收拾你自己制造的乱
- 不"顺手改进"无关内容；不翻新没坏的
- 跟着原本的风格走，哪怕你个人偏好不同写法
- 每处改动都要能直接追溯到需求

### 2.4 目标驱动执行
- "做到什么算成功" 先定清楚，对着标准跑
- 写代码："加个校验" → 先写非法输入测试 → 让它通过
- 修 bug：先写能复现的测试 → 让它通过
- 多步任务：简短计划 + 每步验证点
- 成功标准够强 → 能自验；标准太虚（"弄好就行"）→ 必来问

---

## 3. Contract-First 并行开发工作流 ⭐

> **M2 关键教训**：后端先做完才派前端 = 伪并行。真正的并行 = 契约先行。

### 3.1 流程

```
1. 写契约  →  audit-results/contract-{feature}.json
            (endpoint / request / response_200 / sql_queries / ts_type_unchanged)
2. 派工    →  后端 subagent (Go handlers + 测试)
            →  前端 subagent (页面 + 客户端 + 测试)
            两个 subagent 同时启动，concurrency ≤ 3
3. 集成    →  主 agent 实际跑 go test / tsc / 校验契约字段
4. 关闭    →  更新 TODOS.md + audit-results/{feature}-DELIVERY.md
```

### 3.2 契约文件最小 schema

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

### 3.3 何时用 / 何时不用

| 场景 | 必走契约 |
|---|---|
| 跨前后端新功能（REST/WS endpoint + UI） | ✅ |
| 数据迁移 / schema 变更 | ✅（migration 本身就是契约） |
| 纯前端组件 / 纯后端 handler | 不需要 |
| 紧急 hotfix | 不需要 |

### 3.4 反模式

- ❌ "后端先做完再让前端动" — 浪费并行预算
- ❌ 契约只在 chat 里口头约定 — 必落盘
- ❌ 前后端 subagent 共享未写入契约的隐式约定

**完整 GCL / subagent 分派规则** → `docs/subagent-playbook.md`

---

## 4. 后端 (Go) 速查

**详细规范** → `docs/backend-standards.md`

| 主题 | 规则 |
|---|---|
| Layout | `cmd/<bin>/main.go` + `internal/<pkg>/<file>.go` + `*_test.go` 同包同目录 |
| TDD | 表驱动 `for _, tc := range []struct{...}`；DB 测试用接口替身 |
| **并发测试** | **高并发路径必写**（`sync.Mutex` / 全局 map / goroutine 启停 / channel drop / state machine）。CI 必跑 `go test -race ./...`，0 race 才允许 merge |
| Handler | chi + `writeJSON(w, status, body)` 助手；DB down → 503 |
| 密码 | bcrypt；bad-user / bad-password 都走 CompareHashAndPassword（防 timing） |
| Session | HttpOnly + SameSite=Lax + Secure-when-TLS |
| CSRF | double-submit cookie + `X-CSRF-Token` header；state-changing 必校验 |
| SQL | 参数化 `$1,$2`；写必带 `ON CONFLICT`；月分区表查必带 `created_at` 范围 |
| 迁移 | 新增 `db/migrations/NNNN_xxx.sql`，**永不**改已 ship 的 |
| WS | 简单场景手写 RFC 6455（~50 行）替 `gorilla/websocket`；Hub `sync.RWMutex + map` |
| 提交前 | `go build ./...` && `go test -count=1 ./...` && `gofmt -l .` 全 0 |

---

## 5. 前端 (TS / Next.js) 速查

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

## 6. 提交 / 分支

- 主分支：`main`（保护）
- Feature：`feature/<short-kebab>` 或 `fix/<short-kebab>`
- Commit：`<scope>: <imperative>`（如 `api: add rules CRUD handler`）
- 必带：测试 + 更新 `TODOS.md` 状态

---

## 7. 项目特定 gotchas

**完整列表** → `docs/gotchas.md`（A 并发 / B DB / C 安全 / D WS / E 前端 / F 部署 / G 流程）

最常见的 3 个：

- **dashboard 暴露云清单** → 必须过 M2-5 auth（`/api/v1/{ping,stats,auth/login,auth/logout,ws}` 之外）
- **LLM 调用慢** → `latency_ms` 必填；webhook 异步化不阻塞签名校验
- **multi-account 凭据** → 配置文件 `chmod 0600`；env var 兜底；运行时只读 STS 临时 token
- **高并发路径无测试** → 必加并发测试 + `go test -race`（详见 gotchas §A1-A6）

---

## 8. 必须维护的文档

| 文件 | 何时更新 |
|---|---|
| `TODOS.md` | 每个 M 状态变化 |
| `audit-results/contract-*.json` | 新增跨端点契约 |
| `audit-results/M{N}-DELIVERY.md` | milestone 完成 |
| `audit-results/gcl-trace-*.json` | 每次 subagent 编排 |
| `docs/subagent-playbook.md` | subagent 派工流程变更 |
| `docs/backend-standards.md` | Go 规范变更 |
| `docs/frontend-standards.md` | TS 规范变更 |
| `docs/gotchas.md` | 新踩坑时 |
| `docs/patterns/*.md` | 新增可复用模式 |
| `docs/adr/*.md` | 新增架构决策 |
| `docs/data-retention.md` | 合规要求变更 |
| `docs/kill-criterion.md` | AI 准确率门槛变更 |
| `design.md` | 架构级决策（不轻易动） |

---

## 9. 工具与流程

### 9.1 CodeGraph MCP ⭐ （代码理解优先）

> **代码理解类任务必须优先用 CodeGraph MCP**（`codegraph_codegraph_explore`）。
> 不要直接 grep——CodeGraph 基于符号索引，上下文更完整。

**使用场景**：
- 了解 X 函数 / 类在哪定义、被谁调用
- 跨文件关系 / 架构理解
- 修改前看调用链 / 隐含依赖
- 任何 "这个模块/函数怎么工作" 的问题

**原则**：
1. **执行前先 sync**：在重要工作开始前（多文件改动 / 重构 / 跨包调试）跑一次 CodeGraph sync
2. **explore 优先**：理解任务第一步必走 `codegraph_codegraph_explore`（一个调用拿到完整上下文 + 调用路径）
3. **Fallback 层级**（CodeGraph 不可用时）：
   - `codegraph` → `hypa_grep` / `grep` / `hypa_find` / `find` / `read`
   - 不要跳过 explore 直接 grep，会丢上下文
4. **不要凭印象改代码**：先 explore 拿实际签名 / 字段 / 调用点，再下手

**反模式**：
- ❌ 拿训练数据记忆的函数签名下手改代码（未读磁盘）
- ❌ 第一反应是 grep 而不是 explore
- ❌ 在多文件重构前不 sync index

### 9.2 其他工具

| 任务 | 工具 |
|---|---|
| 调试 / 查调用链 / 符号 | `codegraph_codegraph_explore` |
| 大范围 grep（可能爆量） | `hypa_grep`（压缩 + 截断） |
| 小范围精确 grep | `bash` + `rg` |
| 读大文件 | `hypa_read`（带 offset/limit） |
| 后台服务 | `process` 工具（不要 `nohup`/`&`） |
| 子任务并行 | `subagent`（≤ 3 并发） |

---

## 10. 红线（禁止事项）

- ❌ 改已 ship 的 `db/migrations/NNNN_*.sql`（建新文件 N+1）
- ❌ `internal/agent` 暴露给 HTTP（agent 只能由 worker 调用）
- ❌ LLM raw response 透给未授权用户
- ❌ 跳过 `go test` / `npx tsc` 直接 commit
- ❌ 生产 DB 直接 DDL（必须先提交 migration）
- ❌ AccessKey / 私钥 commit 到 git
- ❌ 新增依赖不更新 `package.json` / `go.mod`
- ❌ AGENTS.md 超过 500 行（先 `wc -l` 再写）
- ❌ docs/*.md 超过 500 行未拆分（跑 `make check-doc-size` 自动检查）
- ❌ 改代码前不 `codegraph_codegraph_explore`（违反 9.1）
- ❌ 违反 §1 三大方法论（TDD / subagents / GCL）
- ❌ 违反 §2 Karpathy 四准则

---

**本文件 ≤ 500 行**。新规则前先 `wc -l`；> 400 谨慎，> 500 必精简 + 拆 `docs/`。