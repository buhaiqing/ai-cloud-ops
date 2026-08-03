{
  "contract_id": "M3-2+M3-3",
  "version": "1.0",
  "endpoint": "(no new endpoint — extends existing types)",
  "auth": "n/a",
  "scope": "internal/agent/ types + Client methods",
  "type_changes": {
    "Recommendation": {
      "existing_fields": ["action", "command", "expected_outcome"],
      "new_fields": {
        "preconditions": "[]string — must be true before running (e.g. ['service healthy', 'maintenance window open'])",
        "rollback_command": "string — how to undo; empty if irreversible",
        "risk_level": "string — 'low' | 'medium' | 'high' | 'irreversible'",
        "estimated_downtime_s": "int — 0 if none"
      },
      "rationale": "M1 had action/command/expected_outcome. M3 needs structured safety metadata so HITL UI can render risk badges and rollback hints."
    },
    "DryRunResult": {
      "new_type": true,
      "fields": {
        "diagnosis": "Diagnosis",
        "dry_run": "bool — true",
        "would_execute": "[]PlannedAction — what would happen",
        "estimated_total_downtime_s": "int",
        "blocked_by_policy": "[]string — e.g. ['tool X not in WRITE_TOOLS whitelist', 'no recent backup']"
      }
    },
    "PlannedAction": {
      "new_type": true,
      "fields": {
        "tool_name": "string",
        "command": "string",
        "target_resource": "string",
        "risk_level": "string",
        "rollback": "string",
        "preconditions_met": "[]string — observed state vs required"
      }
    }
  },
  "method_changes": {
    "Client.Diagnose": "unchanged signature — existing callers keep working",
    "Client.DiagnoseDryRun": {
      "new": true,
      "signature": "(ctx context.Context, alert map[string]any) (*DryRunResult, error)",
      "behavior": [
        "Runs the same prompt/tool loop as Diagnose, but",
        "intercepts any tool call where IsAllowed(tool) AND tool.Category == Write",
        "instead of executing, builds PlannedAction with observed preconditions",
        "if tool NOT in WRITE_TOOLS whitelist, adds 'blocked_by_policy' entry and skips",
        "returns DryRunResult with diagnosis + would_execute + blocked_by_policy"
      ]
    }
  },
  "frontend_contract": "no TS type change for now (UI changes in M3-4 contract)",
  "ts_type_unchanged": true,
  "tdd_target": "8-10 tests covering: empty Recommendations, all-ReadOnly actions (no PlannedAction), mixed Read/Write, blocked tool, preconditions computation, rollback present/missing"
}