# M2-Hardening · 并发回归防护交付

**状态：** ✅ 完成
**日期：** 2026-08-04
**触发：** 用户要求为 M2 高并发路径加测试覆盖（避免生产难调试的并发 bug）
**策略：** TDD + GCL + 3 并行 subagent（concurrency ≤ 3）

---

## 交付清单

| # | 文件 | 新增测试 | 模式 |
|---|---|---|---|
| H-1 | `internal/api/ws_test.go` | 3 (主 agent 手写) | Hub 并发 |
| H-2 | `internal/auth/session_test.go` | 5 (Subagent A) | Session Store 并发 |
| H-3 | `internal/api/incidents_concurrency_test.go` | 5 (Subagent B) | 状态机 + 转换 handler |
| H-4a | `web/lib/ws.test.ts` | 3 (Subagent C) | 前端 WS client |
| H-4b | `web/lib/api.test.ts` (新文件) | 3 (Subagent C) | 前端 api client |
| H-5 | 文档 | — | Makefile + gotchas.md + AGENTS.md |

**合计：21 个新并发测试**

---

## 各测试覆盖的风险面

### H-1 · Hub（`internal/api/ws.go`）
- `TestHub_ConcurrentPublishAndSubscribe` — 4 publisher + 4 subscriber goroutines × 200 iter；ClientCount 永远 ≥ 0
- `TestHub_PublishDoesNotBlockOnSlowConsumer` — 1000 events，slow consumer (unbuffered) 不阻塞
- `TestHub_ConcurrentClientCountNeverNegative` — 8 goroutines × 1000 add/remove，ClientCount 终态 0

### H-2 · Session Store（`internal/auth/session.go`）
- `TestStore_ConcurrentIssueProducesUniqueIDs` — 8 goroutine × 1000 Issue，8000 unique ID
- `TestStore_ConcurrentGetAndRevoke` — 4 Get + 4 Revoke 抢 1000 session × 5000 iter
- `TestStore_ConcurrentReadMostly` — 10 reader + 1 writer（dashboard poll 模式）
- `TestStore_RevokeDuringGet` — 10000 iter Get vs Revoke 同 ID，field-validity guards
- `TestStore_HighContentionCSRFUniqueness` — 8 goroutine × 1250 Issue，10000 unique CSRF

### H-3 · 状态机（`internal/api/incidents.go`）
- `TestCanTransition_ConcurrentReadsAreSafe` — 16 goroutine × 10k 随机 call；map 形状不变
- `TestCanTransition_ValidTransitionsMapImmutable` — 16 goroutine × 10k 读，前后 snapshot 字节相同
- `TestIncidentTransition_NoRealDBNeeded_ButDocumentContract` — nil Pool → 503（不 500 不 panic）
- `TestStateMachine_RaceHeavyWalk` — 8 goroutine × 1000 步 valid walk
- `TestValidTransitions_ConsistentUnderConcurrency` — 16 × 5k 读，deep snapshot 一致

### H-4 · 前端（`web/lib/ws.ts` + `web/lib/api.ts`）
WS client：
- `TestWS_MultipleSubscribersAllReceive` — 50 个 subscriber 收同一个 event
- `TestWS_UnsubscribeDuringEventDelivery` — B handler 里 unsubs A；A 收 0，B 收 10
- `TestWS_ReconnectRaceWithIntentionalClose` — close() 与 reconnect 调度 race
- `TestWS_SingletonConcurrentInit` — 100x Promise.all(getWSClient) 同一 instance
- `TestWS_BackoffCapAt30s` — 10 次 close 后 delay cap 在 30s

API client：
- `TestApi_ConcurrentCallsIndependent` — 10 并发 listAlerts 各拿各的
- `TestApi_HeadersPerCall` — 每次调用自组 headers（无模块级泄漏）
- `TestApi_ConcurrentFailingCallsDoNotPoisonOthers` — 5 并发 3 失败 2 成功，状态不串

---

## 验证证据

```bash
# Go 全量 race
$ make verify
ok  cmd/aico-mcp
ok  internal/agent
ok  internal/api        # +5 from H-3 = 27 tests
ok  internal/auth       # +5 from H-2 = 18 tests
ok  internal/config
ok  internal/credentials
ok  internal/db
ok  internal/eval
ok  internal/ingest
ok  internal/worker

# 前端
$ cd web && npx vitest run --no-coverage
Test Files  7 passed (7)
Tests       51 passed (51)   # 含 8 新并发

# TypeScript
$ npx tsc --noEmit
(0 errors)
```

---

## 关键约束遵守

- **stdlib only**：所有 Go 测试只用 `sync` / `sync/atomic` / `math/rand` / `testing`
- **无 t.Parallel()**：测试顺序可控，失败定位清晰
- **deterministic seed**：`rand.New(rand.NewSource(42))` 复现
- **不动生产代码**：只追加测试文件；`ws.go` / `session.go` / `incidents.go` / `ws.ts` / `api.ts` 全部未改
- **无新依赖**

---

## 已知 trade-off（ponytail）

- H-3 中"B) 两个 HTTP /ack 并发"路径需要 mock `*pgxpool.Pool`；`Deps.Pool` 是具体类型，subagent 没动 `incidents.go`（约束禁止）。当前用 H-3 测试 #3（nil Pool → 503）兜底；将来重构 `Deps.Pool` 为 interface 后可加 E2E 并发测试
- H-4b CSRF literal header 测试因 `api.ts` 未实现 CSRF 而改用 `Content-Type` 验证"每次调用自组 headers"原则；subagent 留了 `ponytail:` 注释，待 CSRF 真正落地时扩展

---

## 文档更新

| 文件 | 变更 |
|---|---|
| `Makefile` | 新增 `verify` (gofmt + vet + race) / `test-race` / `test-all` target |
| `docs/backend-standards.md` | §2.4 新增"并发测试规范" + §2.5 CI 必跑 `-race` |
| `docs/gotchas.md` | A1-A6 并发陷阱 + B/C/D 其他踩坑 |
| `AGENTS.md` | §2 加"并发测试"行；§8.1 CodeGraph 规则（用户要求） |
| `TODOS.md` | M2-Hardening 区从"进行中"→"✅ 完成" |
