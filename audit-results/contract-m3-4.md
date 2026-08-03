{
  "contract_id": "M3-4",
  "version": "1.0",
  "endpoints_consumed": [
    "POST /api/v1/exec/plan",
    "POST /api/v1/exec/approve",
    "POST /api/v1/exec/{exec_id}/execute",
    "GET /api/v1/exec/{exec_id}"
  ],
  "new_frontend_pages": {
    "/analyses/[id]": {
      "purpose": "Display AI diagnosis with structured Recommendations",
      "components": ["DiagnosisHeader", "RecommendationCard (per rec)", "DryRunSummary", "ApproveBar"],
      "data_testids": ["analysis-root", "rec-card-{idx}", "risk-pill-{idx}", "rollback-code-{idx}", "btn-dry-run", "btn-approve", "btn-modify", "btn-reject"]
    },
    "/executions/[id]": {
      "purpose": "Show execution progress + audit trail",
      "components": ["ExecutionStatus", "ActionList (with status per action)", "AuditTrail"],
      "data_testids": ["exec-status", "exec-progress-bar", "action-row-{idx}", "audit-row-{idx}"]
    },
    "/executions": {
      "purpose": "List of all executions for current account (history view)",
      "components": ["ExecutionTable"],
      "data_testids": ["executions-table", "exec-row-{id}"]
    }
  },
  "new_lib_files": {
    "web/lib/exec.ts": {
      "exports": ["plan(diagnosisId, dryRun)", "approve(planId, note)", "execute(execId)", "getExecution(execId)", "listExecutions(account?)"],
      "shape": "matches contract-m3-5.md endpoints"
    }
  },
  "state_machine_for_executions": {
    "states": ["planned", "approved", "running", "completed", "failed", "rolled_back"],
    "transitions": {
      "planned": ["approved", "rejected (terminated)"],
      "approved": ["running"],
      "running": ["completed", "failed"],
      "failed": ["rolled_back (if rollback possible)", "completed (if no rollback)"]
    },
    "ui_disabled_states": ["completed", "rolled_back"]
  },
  "frontend_types": {
    "Recommendation (extended)": "import from web/lib/api.ts; add preconditions, rollback_command, risk_level, estimated_downtime_s",
    "PlannedAction": "import from web/lib/api.ts; same fields as backend contract",
    "ExecPlan": "{ id: int, diagnosis_id: int, would_execute: PlannedAction[], blocked_by_policy: string[], status: string, created_at: string }",
    "Execution": "{ id: int, plan_id: int, status: string, actions_total: int, actions_completed: int, started_at: string, completed_at: string|null, audit_trail: ExecAuditRow[] }"
  },
  "ux_requirements": {
    "risk_pill_color": "low=green, medium=amber, high=red, irreversible=purple",
    "dry_run_default_on": true,
    "approve_requires_dry_run_viewed": "boolean flag — disable Approve button until user has seen dry-run results",
    "reject_modal": "textarea for reason + confirm button"
  },
  "tdd_target": "12-15 tests covering: RecommendationCard renders all fields, DryRun button calls /plan and shows would_execute, Approve button disabled until dry-run viewed, Reject opens modal, ExecutionPage shows status + audit trail"
}