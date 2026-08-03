# Subagent Playbook · ai-cloud-ops

> M2 踩坑后沉淀。用 subagent 前必读。
> 配合 AGENTS.md §1 (Contract-First) + §4 (TDD/GCL) 一起用。

---

## 1. 何时用 subagent

| 场景 | 用 | 不用 |
|---|---|---|
| 跨多文件 / 多包改动 | ✅ | |
| 并行 fan-out 3+ 同构任务 | ✅ | |
| 复用已写好的模板/契约 | ✅ | |
| 单文件 typo / 一行修复 | | ❌（主 agent 直接改） |
| 读一个文件 / 查一个变量 | | ❌ |
| 单步确定性操作（`ls` / `grep` / `find`） | | ❌ |
| 已有 subagent 写过同类 | ❌ 复用之前的结果 | |

**核心**：默认**不扇出**。subagent 是为并行，不是为偷懒。

---

## 2. 派工前 checklist ⭐⭐⭐

> **派工前**主 agent 必跑这 6 步。漏一步 = 必然踩坑。

### 2.1 文件所有权矩阵

列出每个 subagent **唯一**写哪些文件，**禁止跨边界**：

```
Subagent A: internal/auth/session_test.go       (新增并发测试)
Subagent B: internal/api/incidents_concurrency_test.go  (新文件 OK)
Subagent C: web/lib/ws.test.ts, web/lib/api.test.ts  (前端测试)

共同文件 (vitest.config.ts): 谁先到谁写，最后写赢
→ 解法：让 Subagent A（最小任务）写共享 config，其他读不写
```

### 2.2 共享文件识别

派工前 grep 列出可能冲突的文件：

```bash
# 例：派 3 个前端 subagent 前
grep -r 'vitest.config\|setup.ts\|package.json' web/ | sort -u
```

任何 subagent 的 prompt **必须显式列**：
- ✅ 允许写：path1, path2
- 🚫 禁止写：path3, path4（含 path-to-shared-config）
- ⚠️  只读：path-to-shared-config（如必须修改，先确认无并发）

### 2.3 依赖顺序

```
有依赖：
  Subagent A (写 config) → Subagent B/C (用 config)

无依赖（可并行）：
  Subagent B + C + D
```

判断标准：subagent 启动时是否需要其他 subagent 的产物。

### 2.4 共享测试基础设施

`vitest.config.ts` / `tests/setup.ts` / `conftest.py` / `_test.go` helpers：

- **只允许一个** subagent 写
- 其他 subagent 在 prompt 里写："Don't touch `vitest.config.ts`，如果需要新 include glob，加注释说 subagent X 会更新"
- 主 agent 派工**前**先把这些文件写好（如果已有则不动）

### 2.5 共享依赖安装

`npm install` / `go mod tidy` 不要在并行 subagent 里跑：

- Subagent A 装依赖（先）
- Subagent B/C 假设依赖已装
- 如果某个 subagent 必须装：在 prompt 里**显式说**"如果 node_modules 不存在，先跑 npm install，**只跑一次**"

### 2.6 派工时机

- 一组并行 subagent 启动 → 主 agent **不阻塞等待**（subagent 工具是 fire-and-forget）
- 但**记录 dispatch 时间**到 orchestrator trace

---

## 3. Subagent prompt 模板 ⭐

### 3.1 通用骨架（所有 subagent 必含）

```
You are Subagent {X} for {project context}.
Work strictly with TDD. All code lives in {absolute paths}.

READ FIRST (load context):
- {path1} — {why}
- {path2} — {why}

CONSTRAINTS (HARD):
- Allowed to write: {list}
- FORBIDDEN to modify: {list, especially shared config files}
- No new dependencies: {yes/no, which can be added}
- Follow style of: {reference file path}
- Reproducibility: {seed / fixed inputs / no real network}

YOUR DELIVERABLES (write tests FIRST, then implementation):

1. {deliverable 1 — exact file path + test coverage}
2. {deliverable 2}
...

EVIDENCE (run and report):
- {exact command 1} → must pass
- {exact command 2} → must pass (e.g. `go test -race ./...`)
- Report: file(s) modified, test count, pass/fail

Output: ≤ {N} lines summary. {No essays / No emojis / Concise only.}
```

### 3.2 Backend (Go) subagent 必加

```
- Run `go test -count=1 -race ./...` (CI gate)
- Run `gofmt -l .` and `go vet ./...` (must be clean)
- If pool *pgxpool.Pool is used, write integration test with `//go:build integration` tag
- Use math/rand seeded deterministically (rand.NewSource(42)) for any concurrency test
- No t.Parallel() (keep tests sequential for clear failure attribution)
```

### 3.3 Frontend (TS) subagent 必加

```
- Run `cd web && npx vitest run --no-coverage` (NOT from repo root — config won't be found)
- Run `cd web && npx tsc --noEmit` (0 errors)
- Mock fetch globally with vi.fn() in beforeEach; restoreAllMocks in afterEach
- For WS: mock global WebSocket class (vi.stubGlobal)
- Use `data-testid` on interactive elements for testability
- Use getByRole / getByTestId for queries (avoid getByText)
```

### 3.4 Test-only subagent 必加

```
- ONLY add tests; do not modify production code
- If existing tests break: STOP and report; don't fix
- Concurrent test naming: TestX_Concurrent{Y} or TestX_Race{Y} or TestX_HighContention{Y}
- 8+ goroutines × 1000+ iterations minimum
- Use sync.WaitGroup + atomic counters; never time.Sleep in tests
```

---

## 4. 收到 subagent 结果后 checklist ⭐⭐⭐

> **subagent 自报永远不可信**。主 agent 必独立验证。

### 4.1 必跑的验证

| 检查 | 命令 |
|---|---|
| Go 编译 | `go build ./...` (0 error) |
| Go 全测 | `make test-race` 或 `go test -race -count=1 ./...` |
| 前端 tsc | `cd web && npx tsc --noEmit` (0 error) |
| 前端 vitest | `cd web && npx vitest run --no-coverage` (全 pass) |
| Lint | `gofmt -l .` + `go vet ./...` (空输出) |
| 文件存在 | `ls -la {expected files}` |

### 4.2 契约对齐检查（Contract-First 流程）

subagent 完成后对照 `audit-results/contract-{feature}.json`：

```bash
# 例：检查 M2-5 契约字段是否对齐
grep -E '"id"|"name"|"severity"|"metric"' internal/api/rules.go
diff <(jq -r '.response_200.properties | keys[]' audit-results/contract-m3-5.md) \
     <(jq -r 'keys[]' internal/api/rules.go | sort -u)
```

### 4.3 subagent 报告验证

subagent 自报 "tests pass" → 主 agent 重跑：
- 不要假设 subagent 跑对了命令
- 实际跑命令，看输出
- 如果 subagent 说"在我的环境跑通"但你的环境失败 → **怀疑 subagent**

### 4.4 失败 / 冲突处理

| subagent 报告 | 主 agent 动作 |
|---|---|
| "All tests pass" 但 tsc 失败 | 修 tsc 错误（subagent 没跑 tsc） |
| "Modified vitest.config.ts" | 检查是否破坏其他 subagent 的测试 |
| "Skipped item X due to complexity" | 评估是真复杂还是偷懒；真复杂则拆下一轮 |
| "Found pre-existing failures" | 验证 pre-existing 真的 pre-existing；否则是 subagent 引入的回归 |
| 报告与磁盘 diff 不一致 | 立即重读文件，不要信报告 |

---

## 5. Subagent 并发 vs 串行决策

### 5.1 并行条件（必须全满足）

- ✅ 文件所有权矩阵不重叠
- ✅ 共享 config 已被一个 subagent 写入，其他不写
- ✅ 依赖安装完成（或其中一个 subagent 专责）
- ✅ 任务数 ≥ 2

### 5.2 串行条件（任一即串行）

- ❌ 需要 subagent A 产物作为 subagent B 输入
- ❌ 文件所有权有重叠
- ❌ 都需要安装依赖
- ❌ 高风险（auth、密钥、生产 schema）→ 必须串行

### 5.3 并发上限

**3 个 subagent 同时跑**（系统强制，不可突破）。

如果需要 4+ 个任务 → 分多轮。

---

## 6. 常见踩坑（M2 实测）

### 6.1 vitest.config.ts 抢写
- **症状**：3 subagent 都想写，最后写的赢，前面的失效
- **修复**：派工**前**主 agent 写好，或指定一个 subagent（最小任务那个）写

### 6.2 tsc 错误漏检
- **症状**：subagent 写完只跑 `vitest`，没跑 `tsc` → type 错误潜伏
- **修复**：subagent prompt 必含 `npx tsc --noEmit`；主 agent 收到后必独立跑

### 6.3 cwd 错误（vitest 找不到 config）
- **症状**：在 repo root 跑 `npx vitest` → node env → 35 个假失败
- **修复**：subagent prompt 必写 `cd web`；主 agent 验证时也 `cd web`

### 6.4 后端先做完才派前端 = 伪并行
- **症状**：浪费并行预算，前端 subagent 等后端
- **修复**：Contract-First（AGENTS.md §1）

### 6.5 npm install 并发
- **症状**：3 subagent 同时 `npm install` → lock 冲突、重复下载
- **修复**：只让一个 subagent 装，其他假设已装

### 6.6 subagent 改生产代码（违反约束）
- **症状**：subagent 修了测试就跑了的生产代码 → 改坏业务逻辑
- **修复**：subagent prompt 必写 "FORBIDDEN to modify: list"；主 agent 用 `git diff` 核对

---

## 7. 反模式（绝对禁止）

- ❌ 派 subagent 不写 prompt 模板（裸 "fix this bug"）
- ❌ subagent prompt 不含 "READ FIRST" 步骤
- ❌ subagent prompt 不含 EVIDENCE 命令
- ❌ subagent prompt 不含 FORBIDDEN 列表
- ❌ 主 agent 信 subagent 自报，不独立跑测试
- ❌ 并行 subagent 不预先规划文件所有权
- ❌ subagent 完成不更新 TODOS.md
- ❌ subagent 失败不读 trace、不复盘

---

## 8. 复用模板（直接复制填充）

- 后端 subagent：`templates/subagent-prompt-backend.md`
- 前端 subagent：`templates/subagent-prompt-frontend.md`
- 测试 subagent：`templates/subagent-prompt-test.md`
- 契约 JSON：`templates/contract-template.json`