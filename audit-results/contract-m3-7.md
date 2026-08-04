# contract-m3-7 — M2+M3 集成 E2E 测试（M3-7）

> version 1.0 · 2026-08-04 · 主 agent（qwen3.8-max）定义，subagent（M2.7）实现
> **Wave 2**：依赖 M3-1 / M3-6 的产物已合入（接口与 handler 行为以落盘代码为准）。

## 目标

一条测试走通全链路：webhook 入库（形状契约）→ AI 诊断（stub planner + ActionTrail 上下文）
→ plan → approve → execute → **失败回滚验证**，全部用内存替身，`go test ./...` 默认可跑
（不依赖真实 Postgres / 云凭证）。真实 DB round-trip 留 integration 标签后续。

## 1. 新文件：internal/api/e2e_m3_test.go

### Fake 清单（全部定义在本文件内，不改 exec_test.go / rollback_test.go）

- `e2eStore` — 实现 `ExecStore` **+ `ExecRollbackMarker`**（M3-6 的 opt-in 接口，
  定义见落盘的 internal/api/exec.go）。
  参考 rollback_test.go 里 Wave-1 已写的内存 fake（可复制其实现到本文件并改名）。
  exec_test.go 的 memExecStore **不需要也不允许改动**（ExecStore 接口未扩展）。
- `e2ePlanner` — 实现 `Planner`：`DiagnoseDryRun` 返回固定 `*agent.DryRunResult`：
  - `Diagnosis.RootCause = "e2e: recent change caused metric breach"`，
    `EvidenceChains` 含一条 `SupportingTool: "lookup_actiontrail_events"`（模拟 M3-1 上下文已注入）。
  - `WouldExecute` = 3 个 `agent.PlannedAction`（ToolName 用 WRITE_TOOLS 中真实工具名，
    TargetResource 各不同）。
  - `LoadDiagnosisAlert` 返回 `(alertMap, accountAlias, nil)`。
- ExecAction / RollbackAction：测试内闭包，可配置"第 N 个失败"与"回滚全成功"。

### 测试用例（≥4）

1. `TestE2E_M3_WebhookToExecution_HappyPath`
   - a) **webhook 形状契约**：用 `internal/ingest.Alert` 构造一条告警，`json.Marshal` →
     POST 到 `ingest.WebhookHandler(insertStub, zap.NewNop())`（httptest），
     insertStub 断言收到的 Alert 字段（alert_id/severity/resource_id）不丢失，返回固定 id；
     断言响应 2xx。签名：`WEBHOOK_SIGNING_SECRET` t.Setenv 为空串或用 ingest 的签名工具算出
     （读 ingest/signature.go 决定，走与 webhook_test.go 相同的现成模式）。
   - b) `api.Mount` 挂全套 Deps{ExecStore: e2eStore, Planner: e2ePlanner, ExecAction: 全成功}（Auth 留 nil，走无鉴权路径，与 router_test.go 一致）。
   - c) POST /api/v1/exec/plan → 201/200，取 plan_id（读 exec.go execPlanHandler 实际状态码与字段名，以代码为准）。
   - d) POST /api/v1/exec/approve → approved。
   - e) POST /api/v1/exec/{id}/execute → 200，final_status `completed`，actions_total 3。
   - f) GET /api/v1/exec/{id} → status completed，audit_trail 长度 3 全 success。
2. `TestE2E_M3_ExecuteFailure_Rollback`
   - AICO_ROLLBACK_ENABLED=true；ExecAction 第 3 个失败；RollbackAction 全成功。
   - execute → final_status `rolled_back`，响应 `rolled_back: 2`；
     GET → audit seq1/2 = rolled_back，seq3 = failed。
3. `TestE2E_M3_ActionTrailContextVisible`
   - e2ePlanner 断言：DiagnoseDryRun 收到的 alert map 非空；返回的 DryRunResult
     EvidenceChains 里的 actiontrail evidence 最终出现在 GET /api/v1/analyses 无关——
     改为断言 plan 记录里 would_execute JSON 能 unmarshal 回 3 个 PlannedAction 且
     diagnosis evidence 链在 planner 侧被调用（fake 记录调用次数 ≥1）。
4. `TestE2E_M3_RateLimit429`
   - 预先 BeginExecution 10 个 plan（EXEC_RATE_LIMIT 默认 10），第 11 个 execute → 429，
     plan 仍 approved（复用 exec_test.go 的既有断言模式，只验证链路位置正确）。

### 约束

- 所有 HTTP 调用走 httptest + `api.Mount` 构造的 router；不启真实端口。
- 无 t.Parallel()；无 time.Sleep；无网络。
- import 路径用 `github.com/buhaiqing/ai-cloud-ops/internal/...`。

## 2. 文件所有权

- ✅ 允许写：`internal/api/e2e_m3_test.go`（新）
- 🚫 禁止写：`internal/api/exec_test.go`、一切非 `_test.go` 的 Go 文件、前端、migration、eval、agent

## 3. EVIDENCE

```bash
go test -race -count=1 ./internal/api/
go test -race -count=1 ./internal/ingest/
gofmt -l internal/api/ internal/ingest/   # 空输出
go vet ./internal/api/ ./internal/ingest/
```
