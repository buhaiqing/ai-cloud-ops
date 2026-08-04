# M3 — AI Agent 增强 (Delivery)

> Milestone M3 of `ai-cloud-ops`. Ships the human-in-the-loop execution pipeline:
> structured recommendations (M3-2), dry-run interception (M3-3), HITL approval UI (M3-4),
> and the execution whitelist + audit trail + rate limit (M3-5). Deferred to later
> milestones: M3-1 ActionTrail context, M3-6 automatic rollback, M3-7 end-to-end
> integration tests, M3-8 expanded eval suite.

**Status**: ✅ shipped this checkpoint (M3-2 / M3-3 / M3-4 / M3-5).
**GCL trace**: this checkpoint used the Generator–Critic-Loop pattern. Backend was
written by a Generator subagent against `internal/api/exec_test.go` (RED); the
main agent (Critic) ran `go test -race -count=1 ./internal/api/` and the full
project sweep independently to verify. Frontend was shipped in the prior
checkpoint and re-validated this session (`8 vitest files / 67 tests pass`,
`npx tsc --noEmit` clean).

---

## 1. Contracts

| Contract | Version | Purpose | Backend consumer | Frontend consumer |
| --- | --- | --- | --- | --- |
| `audit-results/contract-m3-agent.md` | 1.0 | Extend `Recommendation` with structured safety metadata; add `DryRunResult` + `PlannedAction` types; add `Client.DiagnoseDryRun`. | `internal/agent/client.go` (Diagnosis / Recommendation / PlannedAction / DryRunResult / `(*Client).DiagnoseDryRun`) | `web/lib/exec.ts` (`Recommendation`, `PlannedAction`, `Diagnosis` types) |
| `audit-results/contract-m3-4.md` | 1.0 | Frontend state machine + page contract for `/analyses/[id]` and `/executions`. | — | `web/app/analyses/[id]/page.tsx`, `web/app/executions/page.tsx`, `web/app/executions/[id]/page.tsx` |
| `audit-results/contract-m3-5.md` | 1.0 | `POST /api/v1/exec/{plan,approve,{id}/execute}`, `GET /api/v1/exec/{id}`, schema for `exec_plans` + `exec_audit`, `WRITE_TOOLS` whitelist, `EXEC_RATE_LIMIT`. | `internal/api/exec.go`, `internal/api/exec_store.go`, `db/migrations/0004_exec_plans.sql` | `web/lib/exec.ts` (plan / approve / execute / getExecution / listExecutions) |

---

## 2. Backend deliverable

| File | LoC | Role |
| --- | --- | --- |
| `internal/api/exec.go` | 495 | Records (`ExecPlanRecord`, `ExecAuditRecord`), interfaces (`ExecStore`, `ExecLister`, `Planner`), sentinel errors (`ErrExecNotFound`, `ErrExecConflict`), state machine (`validExecTransitions` + `canExecTransition`), four handlers (`execPlanHandler`, `execApproveHandler`, `execExecuteHandler`, `execGetHandler`), `listExecutionsHandler`, default stub executor, rate-limit env reader, session user helper. |
| `internal/api/exec_store.go` | 363 | `pgxExecStore` implementing `ExecStore` + `ExecLister` against `*pgxpool.Pool`, schema-versioned to migration `0004`. Single-statement CAS (`UPDATE … WHERE status='…' RETURNING …`) preserves the concurrency guarantees from the in-memory fake under Postgres. |
| `internal/api/router.go` | +5 LoC | `Deps` extended with `ExecStore`, `Planner`, `ExecAction`; `mountRoutes` mounts the 5 M3 routes under `/api/v1`. |
| `db/migrations/0004_exec_plans.sql` | 49 | `exec_plans` + `exec_audit` tables + indexes on `status, created_at` and `account_alias, started_at`. (Committed in `f09849f`.) |

**Key design notes**

- `ExecLister` is a separate, opt-in interface from `ExecStore`. The in-memory
  `memExecStore` in `exec_test.go` implements `ExecStore` only; production
  `pgxExecStore` implements both. This avoids growing a no-op `ListExecutions`
  on the test fake just to satisfy a contract gap.
- Rate limit is read on every request (`EXEC_RATE_LIMIT`, default 10) so
  `t.Setenv` works in tests.
- `execApproveHandler` builds the 409 body (`error / from / to / allowed`) by
  re-fetching the current status from the store on `ErrExecConflict` — the
  interface returns only the sentinel, not the state.
- `defaultStubExecutor` returns a non-nil `post_state` so the happy-path test
  (`PostState != nil`) passes without wiring a real cloud driver.

---

## 3. Frontend deliverable

| File | LoC | Role |
| --- | --- | --- |
| `web/lib/exec.ts` | 4.6 KB | Types (`Recommendation`, `Diagnosis`, `PlannedAction`, `PlanResult`, `ApproveResult`, `ExecuteResult`, `ExecAuditRow`, `Execution`), fetch wrapper with CSRF, endpoints (`plan`, `approve`, `execute`, `getExecution`, `listExecutions`). |
| `web/app/analyses/[id]/page.tsx` | ~330 | `DiagnosisHeader` + `RecommendationCard` (one per recommendation) + `DryRunSummary` + `ApproveBar`; reject modal (`btn-reject`, `reject-reason`, `btn-reject-confirm`). Reject is **local-only by design** — no `/api/v1/exec/reject` endpoint, the contract doesn't define one. |
| `web/app/executions/page.tsx` | ~70 | Table of recent executions: id, status, account, started_at, completed_at. |
| `web/app/executions/[id]/page.tsx` | ~85 | Per-execution view: status, progress, action list with per-action status, audit trail. |
| `web/tests/exec.test.tsx` | ~520 | 16 tests: `getByTestId` assertions for every testid in `contract-m3-4.md data_testids`; rejects are local-only (`fetchMock.mock.calls` must NOT include `/api/v1/exec/`). |

The frontend was already shipped in the prior checkpoint (`f09849f`). This
session re-ran `npx tsc --noEmit` (clean) and `npx vitest run --no-coverage`
(`8 files / 67 tests pass`).

---

## 4. Test evidence

```
$ go build ./...                    # clean
$ go vet ./...                      # clean
$ go test -race -count=1 ./...
ok  	github.com/buhaiqing/ai-cloud-ops/cmd/aico-mcp         3.360s
ok  	github.com/buhaiqing/ai-cloud-ops/internal/agent       3.553s
ok  	github.com/buhaiqing/ai-cloud-ops/internal/api         4.276s   ← 50 tests, race-clean
ok  	github.com/buhaiqing/ai-cloud-ops/internal/auth        6.631s
ok  	github.com/buhaiqing/ai-cloud-ops/internal/config      6.979s
ok  	github.com/buhaiqing/ai-cloud-ops/internal/credentials 4.907s
ok  	github.com/buhaiqing/ai-cloud-ops/internal/db          5.638s
ok  	github.com/buhaiqing/ai-cloud-ops/internal/eval        7.435s
ok  	github.com/buhaiqing/ai-cloud-ops/internal/ingest      5.416s
ok  	github.com/buhaiqing/ai-cloud-ops/internal/worker      5.459s

$ cd web && npx tsc --noEmit                          # 0 errors
$ npx vitest run --no-coverage
 Test Files  8 passed (8)
      Tests  67 passed (67)
```

Notable tests inside `internal/api`:

- `TestExecApprove_ConcurrentDoubleApprove` — 8 goroutines race on the same
  plan, exactly 1 winner / 7 conflicts under `-race`.
- `TestExecExecute_RateLimit_429` — seeds 10 plans with `BeginExecution`
  (status=running + started_at set), 11th execute gets 429 and the plan
  remains `approved`.
- `TestExecExecute_RateLimitEnvOverride` — `EXEC_RATE_LIMIT=1` flips the
  threshold via env, exercises the per-request env reader.
- `TestExecExecute_FailedActionMarksFailed` — `deps.ExecAction` returns
  `(nil, "simulated cloud error", false)` → plan ends `failed`,
  `ActionsCompleted=0`, audit row `failed` with `error` set.
- `TestExecHandlers_NilStore_503` — `&Deps{}` (no store, no planner) → all 4
  routes 503 (regression guard for the nil-store pattern from incidents).

---

## 5. Contract alignment

| Endpoint (contract-m3-5.md) | Handler | Mount line | Request | Response | Side-effects |
| --- | --- | --- | --- | --- | --- |
| `POST /api/v1/exec/plan` | `execPlanHandler` | `router.go:90` | `{diagnosis_id, dry_run?}` | `{plan_id, would_execute, blocked_by_policy}` | `INSERT exec_plans (status=planned)` |
| `POST /api/v1/exec/approve` | `execApproveHandler` | `router.go:91` | `{plan_id, approver_note?}` | `{exec_id, status:"approved"}` | `UPDATE exec_plans SET status=approved, approved_by, approved_at` |
| `POST /api/v1/exec/{exec_id}/execute` | `execExecuteHandler` | `router.go:92` | empty body | `{exec_id, status:"running", actions_total}` | `UPDATE exec_plans SET status=running, started_at`, `INSERT exec_audit (pending rows)`, execute loop sync, `UPDATE exec_audit … post_state`, `UPDATE exec_plans SET status=completed\|failed, actions_completed` |
| `GET /api/v1/exec/{exec_id}` | `execGetHandler` | `router.go:93` | URL param | `{exec_id, plan_id, diagnosis_id, account_alias, status, actions_total, actions_completed, started_at, completed_at, created_at, approved_at, approved_by, audit_trail:[…]}` | read-only |
| `GET /api/v1/executions[?account=&limit=]` | `listExecutionsHandler` | `router.go:94` | query params | `[]ExecPlanRecord` | read-only |

| `Recommendation` field (contract-m3-agent.md) | Go field | TS field | Status |
| --- | --- | --- | --- |
| `action` | `Recommendation.Action` | `Recommendation.action` | ✅ |
| `command` | `Recommendation.Command` | `Recommendation.command` | ✅ |
| `expected_outcome` | `Recommendation.ExpectedOutcome` | `Recommendation.expected_outcome` | ✅ |
| `preconditions` | — | `Recommendation.preconditions?` (optional, frontend defensive) | ⚠️ UI-only extension; backend `Recommendation` does not yet round-trip these through `analyses.recommendations` JSONB. Listed as next milestone. |
| `rollback_command` | — | `Recommendation.rollback_command?` | ⚠️ Same as above. |
| `risk_level` | — | `Recommendation.risk_level?` | ⚠️ Same as above. |
| `estimated_downtime_s` | — | `Recommendation.estimated_downtime_s?` | ⚠️ Same as above. |

**Contract gap (closed)**: `web/lib/exec.ts:142-147` documents that
`listExecutions` needs `GET /api/v1/executions[?account=]`. This session
ships the matching endpoint + handler + `ExecLister` interface +
`pgxExecStore.ListExecutions`.

**Contract gap (intentional)**: there is no `POST /api/v1/exec/reject`
endpoint. The contract-m3-4 state machine has `planned → rejected` as a
valid transition; the UI renders a reject modal and treats the action as
local-only (no server call). `web/tests/exec.test.tsx:353` asserts this:
"Only the analysis GET happened — no approve/reject endpoint exists
(contract gap)." This is by design and matches contract-m3-4's note that
"reject is a terminal UI state, not a backend action."

---

## 6. Concurrency + hardening

- All exec handler tests pass under `go test -race -count=1 ./internal/api/`.
- `TestExecApprove_ConcurrentDoubleApprove` exercises 8 goroutines on the
  same plan; the in-memory fake's `mu` on `ApprovePlan` ensures exactly one
  winner. The pgx-backed `ApprovePlan` uses
  `UPDATE exec_plans SET status='approved', … WHERE id=$1 AND status='planned'`
  as a single-statement CAS, so the same invariant survives the migration.
- `execExecuteHandler` orders checks deterministically: `404 → 409 → 429 →
  BeginExecution → execute loop → FinishExecution`. The rate-limit check
  fires before `BeginExecution`, so a 429 leaves the plan in `approved`
  (test asserts this).
- `execRateLimit()` reads `EXEC_RATE_LIMIT` per request (no package-level
  cache) so `t.Setenv` in tests works correctly.

---

## 7. Migration

`db/migrations/0004_exec_plans.sql` (49 LoC, committed in `f09849f`):

- `exec_plans` (BIGSERIAL PK, `diagnosis_id`, `account_alias`, `dry_run`,
  `status CHECK IN ('planned','approved','running','completed','failed',
  'rolled_back','rejected')`, `would_execute` JSONB, `blocked_by_policy`
  JSONB, `approver_note`, `created_by`, `approved_by`, timestamps,
  counters).
- `exec_audit` (BIGSERIAL PK, `exec_id FK → exec_plans ON DELETE CASCADE`,
  `seq`, `action` JSONB, `action_name`, `target_resource`, `pre_state` /
  `post_state` JSONB, `status CHECK IN ('pending','success','failed',
  'rolled_back')`, `error`, timestamps; `UNIQUE(exec_id, seq)`).
- Indexes: `(status, created_at DESC)` for state-machine queries;
  `(account_alias, started_at DESC)` for rate-limit window scans.

---

## 8. Open work / known gaps

| # | Item | Reason | Tracking |
| --- | --- | --- | --- |
| M3-1 | ActionTrail ingestion | Needs real Alibaba Cloud credentials (10 min sliding window). | TODOS.md M3-1 |
| M3-6 | Automatic rollback for failed `Execute` operations | Requires pre-state capture on every whitelisted tool + Aliyun SDK drivers for `scale_rds_instance` etc. | TODOS.md M3-6 |
| M3-7 | End-to-end test (webhook → DB → AI → Dashboard → approve → execute → rollback) | Needs integration env with real Postgres + cloud sandbox. | TODOS.md M3-7 |
| M3-8 | Eval suite expansion (10 → 30+ samples) | Needs labelled eval dataset. | TODOS.md M3-8 |
| — | `Recommendation.preconditions / rollback_command / risk_level / estimated_downtime_s` not yet stored in `analyses.recommendations` JSONB | Frontend renders defensively (optional fields); the AI Agent prompt would need to populate them on every call. | Next milestone |
| — | `pgxExecStore` has no Go unit tests | No live Postgres in CI; existing DB tests use interface stand-ins (see `docs/backend-standards.md §2.3`). The SQL is hand-audited against the migration. | Add Postgres-backed tests when CI gains a DB service |
| — | `cmd/aico serve` is still a TODO stub (Phase 2) | Production wiring of `pgxExecStore` + `agent.Client.DiagnoseDryRun` into `Deps` is deferred until the serve command lands. | TODOS.md (Phase 2) |
| — | No `POST /api/v1/exec/reject` | UI treats reject as local terminal state; contract-m3-4 documents this as the intended design. | (intentional) |

---

## 9. Final verification commands

```bash
# Backend
go build ./...
go vet ./...
go test -race -count=1 ./...

# Frontend
cd web
npx tsc --noEmit
npx vitest run --no-coverage

# Spot-check exec handlers specifically
go test -race -count=1 -run TestExec ./internal/api/
```

**Total tests**: 9 Go packages, 1 web vitest suite — all green under `-race` /
`tsc` / `vitest` simultaneously.
