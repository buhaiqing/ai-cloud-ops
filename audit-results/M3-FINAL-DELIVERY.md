# M3-FINAL — AI Agent 增强 Final Delivery

> Milestone M3 of `ai-cloud-ops`. Closes out M3-1 / M3-6 / M3-7 / M3-8 on top
> of the M3-2/3/4/5 shipped in commit `2417f1c` (audit-results/M3-DELIVERY.md).

**Status**: ✅ **M3 milestone complete**. 4 remaining P1/P2 items shipped this session.
**Provider combo** (this session): `qwen3.8-max` (planner/critic) + `MiniMax-M2.7` (subagent worker) — first in-flight use of the cross-provider combo registered at `~/.agents/skills/subagent-orchestrator/assets/provider_policies/aliyun-qwen38max+m27.md`.

---

## 1. Contracts

| Contract | Version | Purpose | Backend consumer | Frontend consumer |
| --- | --- | --- | --- | --- |
| `audit-results/contract-m3-1.md` | 1.0 | `ActionTrailFetcher` interface + `Client.WithActionTrail` injection hook. Stub mode behavior unchanged. | `internal/agent/context.go` (new), `internal/agent/client.go` (2 mount points) | — |
| `audit-results/contract-m3-6.md` | 1.1 | opt-in `ExecRollbackMarker` interface + handler pre-state capture + reverse-order rollback gated on `AICO_ROLLBACK_ENABLED`. v1.1 changed from extending `ExecStore` to a separate opt-in interface (mirrors `ExecLister`) to avoid breaking the existing `memExecStore` fake. | `internal/api/exec.go` (interface + handler), `internal/api/exec_store.go` (`MarkAuditRolledBack` SQL) | (UI already supports `rolled_back` in executions list/detail — M3-4 contract anticipated it) |
| `audit-results/contract-m3-7.md` | 1.0 | All-HTTP E2E in `internal/api/e2e_m3_test.go` covering webhook shape, plan/approve/execute, rollback on failure, ActionTrail context visibility, and rate-limit 429. In-memory fakes only. | `internal/api/e2e_m3_test.go` (new) | — |
| `audit-results/contract-m3-8.md` | 1.0 | `baseline_samples.json` 10→30 samples + 4 schema integrity tests. Distribution: ECS×3, RDS×3, Redis×3, MongoDB×2, SLB×1, VPC×2, K8s×2, Security×2, Edge×2. Difficulty easy 6 / medium 9 / hard 5. | `internal/eval/baseline_samples.json` (+20 samples, version 0.2.0), `internal/eval/samples_test.go` (new) | — |

---

## 2. Backend deliverable

| File | Role |
| --- | --- |
| `internal/agent/context.go` (new) | `ActionTrailEvent` struct, `ActionTrailFetcher` interface, `DefaultActionTrailWindow = 10 * time.Minute`, `attachActionTrail` (nil-safe, 4 downgrade paths). |
| `internal/agent/context_test.go` (new) | 5 contract tests / 9 subtests covering both stub and mock-inference paths. |
| `internal/agent/client.go` (diff) | `Client.actionTrail` field, `WithActionTrail` chainable setter, two mount points in `runDiagnosis` (stub branch + after `group.Wait`). |
| `internal/api/exec.go` (diff) | `ExecRollbackMarker` opt-in interface, `Deps.RollbackAction` field on `Deps`, `execExecuteHandler` pre-state capture + reverse-order rollback + `rolled_back` response field, all gated on `AICO_ROLLBACK_ENABLED`. |
| `internal/api/router.go` (diff) | `RollbackAction` field on `Deps`. |
| `internal/api/exec_store.go` (diff) | `MarkAuditRolledBack(ctx, execID, seqs)` SQL: `UPDATE exec_audit SET status='rolled_back' WHERE exec_id=$1 AND seq = ANY($2::int[]) AND status='success'`. |
| `internal/api/rollback_test.go` (new) | 6 contract tests + `rollbackStore` in-memory fake + `plainExecStore` (hides marker). `exec_test.go` is **untouched** — opt-in interface pattern kept it compiling. |
| `internal/api/e2e_m3_test.go` (new) | 4 E2E tests: HappyPath, ExecuteFailure_Rollback, ActionTrailContextVisible, RateLimit429. Uses `newExecRouter` + `authedRequest` from `exec_test.go`. |
| `internal/eval/baseline_samples.json` (diff) | 20 new samples appended, `version 0.2.0`, original 10 byte-identical. |
| `internal/eval/samples_test.go` (new) | 4 schema integrity tests. |

---

## 3. Key design notes

- **ActionTrail injection is single-goroutine**: both mount points are after `group.Wait()` (or in the stub branch), so `attachActionTrail` is safe to mutate `Diagnosis.EvidenceChains` without locks.
- **Pre-state capture reuses `deps.ExecAction`**: the contract chose to call the executor twice per action (once for pre-state, once for execution). Wasteful in production but trivially testable with the existing fake; documented as a known cost.
- **Rollback marker is opt-in**: following the existing `ExecLister` pattern (not extending `ExecStore`) so `exec_test.go`'s `memExecStore` doesn't have to grow a no-op method. `pgxExecStore` and `rollbackStore` (M3-6) implement it; the e2e test fake (M3-7) implements it.
- **ActionTrail interface is dependency-free**: `ActionTrailFetcher` only depends on `time.Duration` and `context.Context`. Real Aliyun SDK client is the only thing not shipped — required for production use; deferred to a session with credentials (interface + stub suffice for M3 completion).
- **E2E uses in-memory fakes throughout**: a real Postgres round-trip is not in scope per the contract. The M2-Python / Go mismatch on the worker `runCycle` is out of scope (the worker is still stub-mode for AI analysis).

---

## 4. Test evidence

```
$ go test -race -count=1 ./...
ok  github.com/buhaiqing/ai-cloud-ops/cmd/aico-mcp        1.849s
ok  github.com/buhaiqing/ai-cloud-ops/internal/agent      3.443s
ok  github.com/buhaiqing/ai-cloud-ops/internal/api        2.719s   ← 60+ tests, race-clean
ok  github.com/buhaiqing/ai-cloud-ops/internal/auth       2.115s
ok  github.com/buhaiqing/ai-cloud-ops/internal/config     4.213s
ok  github.com/buhaiqing/ai-cloud-ops/internal/credentials 7.100s
ok  github.com/buhaiqing/ai-cloud-ops/internal/db         5.143s
ok  github.com/buhaiqing/ai-cloud-ops/internal/eval       8.050s
ok  github.com/buhaiqing/ai-cloud-ops/internal/ingest     8.056s
ok  github.com/buhaiqing/ai-cloud-ops/internal/worker     7.590s

$ go fix ./...                                # 0 output (no deprecated APIs to fix)
$ gofmt -l internal/api/rollback_test.go internal/api/e2e_m3_test.go \
           internal/api/exec.go internal/api/exec_store.go internal/api/router.go \
           internal/agent/context.go internal/agent/context_test.go \
           internal/eval/samples_test.go
                                            # empty (M3 files clean; pre-existing drift
                                            # in unrelated files is not in scope)

$ cd web && npx tsc --noEmit                  # 0 errors
$ npx vitest run --no-coverage
 Test Files  8 passed (8)
      Tests  67 passed (67)
```

---

## 5. Notable test cases

Inside `internal/api`:
- `TestExecExecute_RollbackOnFailure` — 3 actions, 3rd fails, full rollback → plan = `rolled_back`, audit seq1/2 = `rolled_back`, seq3 = `failed`, response `rolled_back: 2`.
- `TestExecExecute_RollbackDisabledByDefault` — env unset → behavior byte-for-byte identical to before M3-6.
- `TestExecExecute_RollbackReverseOrder` — rollback order = [seq2, seq1].
- `TestExecExecute_StoreWithoutMarker` — `plainExecStore` (no marker) → rollback still runs, plan = `rolled_back`, audit rows stay `success` (only the marker can flip them).
- `TestE2E_M3_WebhookToExecution_HappyPath` — full chain (ingest → /exec/plan → approve → execute → GET).
- `TestE2E_M3_RateLimit429` — pre-seed 10 plans, 11th = 429, plan stays `approved`.

Inside `internal/agent`:
- `TestClient_WithActionTrail_FetchError` — covers both `stub` and `mock inference` branches with the same fake fetcher returning an error → diagnosis still succeeds, no evidence chain appended, no `actiontrail_context_attached` marker.
- `TestClient_WithActionTrail_NoResourceID` — three subcases: missing / empty / non-string `resource_id` → fetcher call count = 0.

Inside `internal/eval`:
- `TestEvaluateBaseline_StubSmoke_30Samples` — fake judge returns 25/25 → `res.Pass == true`, `len(PerSample) == 30`.

---

## 6. GCL trace

This checkpoint used the **Generator–Critic-Loop** pattern with a **cross-provider combo**:
- **Planners/Critics**: `qwen3.8-max` (current session model).
- **Generators**: `minimax-code-cn/MiniMax-M2.7` via `task` agent role (per `modelRoles.task`, per the previous M3-backend trace convention).
- **Fan-out**: Wave 1 dispatched 3 generators in parallel (M3-1, M3-6, M3-8) with the 4th slice (M3-7) held back on a real dependency (M3-6's `ExecRollbackMarker`).

Deviations:
- 2 specialist-type critic agents (`code-reviewer`) failed with 401 (broken `anthropic` route, also seen in the prior M3 trace). Re-dispatched via `task` role; one then hit a 429 quota-exhausted error from the M2.7 provider. Per project AGENTS.md §1.5 escalation: main agent took over the critic role and ran all 6 M3-1 contract assertions directly against the diff (zero defects), plus the M3-8 content review (zero defects). M3-6's generator failed mid-implementation; main agent completed the production code + tests (verifiable in `git log`).
- 2 subagents (M36Rollback + 2 critic agents) all required fallback paths documented above. Net: all 4 M3 slices delivered.

Safety gates (executed this session):
- `gofmt -l` on M3 files → empty
- `go vet ./internal/api/ ./internal/agent/ ./internal/eval/` → clean
- `go test -race -count=1 ./...` → 10 packages green
- `npx tsc --noEmit && npx vitest run` → 0 errors / 67 tests
- `go fix ./...` → 0 output (no deprecated APIs)

---

## 7. Open work / known gaps

| # | Item | Reason | Tracking |
| --- | --- | --- | --- |
| M3-1-real | Real Aliyun ActionTrail HTTP client (ACS3 signing). | Requires real AK credentials; interface is in place. | (out of M3; future session) |
| — | `Recommendation.preconditions / rollback_command / risk_level / estimated_downtime_s` not persisted in `analyses.recommendations` JSONB. | Frontend renders defensively; the AI Agent prompt would need to populate them on every call. | (next milestone) |
| — | `pgxExecStore` has no live-DB Go unit tests. | No live Postgres in CI; SQL is hand-audited against migration 0004 + the new `MarkAuditRolledBack`. | (when CI gains a DB service) |
| — | `cmd/aico serve` is still a TODO stub (Phase 2). | Production wiring of `pgxExecStore` + `agent.Client.WithActionTrail` + `Deps.RollbackAction` is deferred. | TODOS.md (Phase 2) |
| — | `internal/api/exec_test.go` `memExecStore` deliberately not updated to implement `ExecRollbackMarker` (opt-in pattern). When the team is ready, can be added without breaking anything. | (intentional, opt-in) |
| — | No `POST /api/v1/exec/reject` | UI treats reject as local terminal state. | (intentional, contract-m3-4) |
| — | Python track packaging pre-existing failure (`ModuleNotFoundError: ai_cloud_ops`). Unrelated to M3 Go work. | TODOS.md risk #1 (existing) |
| — | Provider combo `qwen3.8-max + M2.7` first in-flight validation pending; M2.7 hit 429 mid-session (quota 5h exhaustion). Combo policy `aliyun-qwen38max+m27.md` documents the contract; judge score to be backfilled in `references/providers/experiments/aliyun/round1/`. | (provider-policies README) |
| — | `gofmt -l .` shows pre-existing drift in 5 files (incidents.go, ws.go, router_test.go, rules_test.go, etc.). Not introduced this session; M3 files clean. | (separate cleanup pass) |

---

## 8. Final verification commands

```bash
# Backend
go fix ./...
go build ./...
go vet ./...
go test -race -count=1 ./...
gofmt -l internal/agent/ internal/api/rollback_test.go internal/api/e2e_m3_test.go \
        internal/eval/

# Frontend
cd web && npx tsc --noEmit && npx vitest run --no-coverage
```

**Total tests**: 10 Go packages (race-clean) + 8 web vitest files / 67 tests — all green.
