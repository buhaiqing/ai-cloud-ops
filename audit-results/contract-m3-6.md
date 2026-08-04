# contract-m3-6 — 操作回滚机制（M3-6）

> version 1.1 · 2026-08-04 · 主 agent（qwen3.8-max）定义，subagent（M2.7）实现
> Generator 只做"按契约填空"，不改契约。
> v1.1 变更：回滚标记改为 opt-in 接口（仿 ExecLister），不扩展 ExecStore，
> 避免破坏 exec_test.go 的 memExecStore 编译。

## 目标

execute 失败时自动回滚已成功的操作：pre-state 捕获 → 反序 rollback → audit 标记。
opt-in（env 开关），默认关闭 = 现有行为逐字节不变。

## 1. Deps 扩展（internal/api/router.go Deps struct）

```go
// M3-6: per-action rollback executor. Called in reverse order for actions
// that completed when a later action fails. Same (postState, errMsg, ok)
// triple as ExecAction; ok=false → rollback failed for that action.
RollbackAction func(ctx context.Context, action agent.PlannedAction, preState json.RawMessage) (json.RawMessage, string, bool)
```

## 2. opt-in 回滚标记接口（internal/api/exec.go）

仿照 `ExecLister` 的既有模式（exec.go 中 ExecLister 定义处），**不扩展** `ExecStore`：

```go
// ExecRollbackMarker is opt-in: stores that can flip successful audit rows
// to 'rolled_back' satisfy it. Kept separate from ExecStore (like ExecLister)
// so existing test fakes don't have to grow a no-op implementation.
type ExecRollbackMarker interface {
	// MarkAuditRolledBack flips audit rows with status='success' for the
	// given exec and seqs to 'rolled_back'. Returns count updated.
	MarkAuditRolledBack(ctx context.Context, execID int64, seqs []int) (int, error)
}
```

`exec_test.go` 的 memExecStore **不动**（接口未扩展，编译不破坏）。

## 3. execExecuteHandler 改动（internal/api/exec.go）

在现有执行循环处：

1. **pre-state 捕获**：循环内每个 action 执行前：
   ```go
   var preState json.RawMessage
   if deps.ExecAction != nil {
       preState, _, _ = deps.ExecAction(r.Context(), a)
   }
   preStates = append(preStates, preState)
   ```
   （`preStates []json.RawMessage` 在循环外声明；ExecAction nil → preState 为 nil，与现状一致。）
2. **失败后回滚**（`failed == true` 且满足以下全部条件才触发）：
   - `os.Getenv("AICO_ROLLBACK_ENABLED") == "true"`
   - `deps.RollbackAction != nil`
   - `completed > 0`
   逻辑：
   ```go
   rolledSeqs := make([]int, 0, completed)
   rollbackErrors := []string{}
   // reverse order: actions[completed-1] down to actions[0]
   for i := completed - 1; i >= 0; i-- {
       _, rbErr, rbOK := deps.RollbackAction(r.Context(), actions[i], preStates[i])
       if rbOK {
           rolledSeqs = append(rolledSeqs, i+1)
       } else {
           rollbackErrors = append(rollbackErrors, fmt.Sprintf("action %d: %s", i+1, rbErr))
       }
   }
   if len(rolledSeqs) > 0 {
       if marker, ok := deps.ExecStore.(ExecRollbackMarker); ok {
           if _, rbStoreErr := marker.MarkAuditRolledBack(r.Context(), id, rolledSeqs); rbStoreErr != nil {
               rollbackErrors = append(rollbackErrors, rbStoreErr.Error())
           }
       } else {
           slog.Warn("exec.rollback.store_marker_unavailable")
       }
   }
   ```
3. **最终状态**：
   - 全部成功回滚（`len(rollbackErrors) == 0 && len(rolledSeqs) > 0`）→ `finalStatus = "rolled_back"`
   - 否则保持 `"failed"`（rollbackErrors 用 slog.Warn 输出）。
   - `FinishExecution` 一次性写终态；直接写 `"rolled_back"`，**不改 validExecTransitions 表**，
     不改 migration（0004 CHECK 已含 'rolled_back'）。
4. **响应体**：保持现有字段，新增 `"rolled_back": len(rolledSeqs)`（int，无回滚时 0）。

## 4. pgxExecStore（internal/api/exec_store.go）

- 实现 `ExecRollbackMarker`：
  ```sql
  UPDATE exec_audit SET status='rolled_back', completed_at=now()
  WHERE exec_id=$1 AND seq = ANY($2::int[]) AND status='success'
  ```
  返回受影响行数。
- 检查 `FinishExecution` 是否对 status 有白名单校验；如有，允许 `rolled_back`。

## 5. 测试（internal/api/rollback_test.go，新文件）

自带 in-memory fake（独立于 exec_test.go 的 memExecStore，避免改共享文件；
fake 同时实现 `ExecStore` 与 `ExecRollbackMarker`）：

1. `TestExecExecute_RollbackOnFailure` — AICO_ROLLBACK_ENABLED=true（t.Setenv），
   3 actions：ExecAction 前 2 成功第 3 失败；RollbackAction 全成功 →
   plan 终态 `rolled_back`，audit seq1/seq2 = `rolled_back`，seq3 = `failed`，
   响应 `rolled_back: 2`。
2. `TestExecExecute_RollbackDisabledByDefault` — 不设 env：同样失败场景 →
   终态 `failed`，audit seq1/seq2 = `success`（现状不变）。
3. `TestExecExecute_RollbackPartialFailure` — 2 成功 1 失败；RollbackAction 对 seq1 失败 →
   终态 `failed`，audit seq2 = `rolled_back`，seq1 = `success`（回滚失败不回改）。
4. `TestExecExecute_RollbackReverseOrder` — RollbackAction 记录调用序列 → 断言 [seq2, seq1]。
5. `TestExecExecute_NilPreStateCapture` — deps.ExecAction == nil（走 defaultStubExecutor 路径）
   + rollback 开启 + 失败 → preStates 全 nil 也能走通，不 panic。
6. `TestExecExecute_StoreWithoutMarker` — fake 只实现 ExecStore（不实现 marker）→
   回滚仍执行、终态 rolled_back，audit 行保持 success（仅 slog warn，不报错）。

## 文件所有权

- ✅ 允许写：`internal/api/exec.go`（opt-in 接口 + handler）、`internal/api/exec_store.go`（MarkAuditRolledBack + FinishExecution 校验）、`internal/api/router.go`（仅 Deps struct 加 RollbackAction 字段）、`internal/api/rollback_test.go`（新）
- 🚫 禁止写：`internal/api/exec_test.go`（memExecStore 不动——接口未扩展）、`internal/agent/*`、`internal/eval/*`、migration、前端

## EVIDENCE

```bash
go test -race -count=1 ./internal/api/
gofmt -l internal/api/   # 空输出
go vet ./internal/api/
```
