{
  "contract_id": "M3-5",
  "version": "1.0",
  "endpoints": {
    "POST /api/v1/exec/plan": {
      "auth": "session-required + csrf-required",
      "request": { "diagnosis_id": "int (required)", "dry_run": "bool (default true)" },
      "response_200": { "plan_id": "int", "would_execute": "[PlannedAction]", "blocked_by_policy": "[string]" },
      "side_effects": "INSERT into exec_plans (status='planned')"
    },
    "POST /api/v1/exec/approve": {
      "auth": "session-required + csrf-required",
      "request": { "plan_id": "int", "approver_note": "string (optional)" },
      "response_200": { "exec_id": "int", "status": "approved" },
      "side_effects": "UPDATE exec_plans SET status='approved', approved_by, approved_at"
    },
    "POST /api/v1/exec/{exec_id}/execute": {
      "auth": "session-required + csrf-required",
      "response_200": { "exec_id": "int", "status": "running", "actions_total": "int" },
      "side_effects": [
        "UPDATE exec_plans SET status='running', started_at",
        "INSERT exec_audit (pre_state JSONB, action, started_at)",
        "executes each PlannedAction in sequence (synchronous MVP; async via worker later)"
      ],
      "rate_limit": "max 10 executions per account per hour (config via env EXEC_RATE_LIMIT)"
    },
    "GET /api/v1/exec/{exec_id}": {
      "auth": "session-required",
      "response_200": {
        "exec_id": "int",
        "plan_id": "int",
        "status": "planned|approved|running|completed|failed|rolled_back",
        "actions_total": "int",
        "actions_completed": "int",
        "started_at": "string",
        "completed_at": "string|null",
        "audit_trail": "[ExecAuditRow]"
      }
    }
  },
  "write_tools_whitelist": {
    "path": "internal/agent/tools.go — extend with WRITE_TOOLS var",
    "tools": [
      {"name": "restart_ecs_instance", "category": "write", "aliyun_service": "ECS", "api_action": "RebootInstance", "risk": "medium", "rollback": "n/a (transient reboot)", "rate_limit_per_hour": 5},
      {"name": "scale_rds_instance", "category": "write", "aliyun_service": "RDS", "api_action": "ModifyDBInstanceSpec", "risk": "high", "rollback": "ModifyDBInstanceSpec (downgrade)", "rate_limit_per_hour": 2},
      {"name": "restart_rds_instance", "category": "write", "aliyun_service": "RDS", "api_action": "RestartDBInstance", "risk": "medium", "rollback": "n/a", "rate_limit_per_hour": 3},
      {"name": "remove_ecs_from_slb", "category": "write", "aliyun_service": "SLB", "api_action": "RemoveBackendServers", "risk": "medium", "rollback": "AddBackendServers", "rate_limit_per_hour": 5}
    ]
  },
  "new_db_tables": {
    "exec_plans": "(id BIGSERIAL PK, diagnosis_id BIGINT, account_alias TEXT, dry_run BOOLEAN, would_execute JSONB, blocked_by_policy JSONB, status TEXT, created_by TEXT, approved_by TEXT, created_at TIMESTAMPTZ, approved_at TIMESTAMPTZ)",
    "exec_audit": "(id BIGSERIAL PK, exec_id BIGINT REFERENCES exec_plans, action_name TEXT, target_resource TEXT, pre_state JSONB, post_state JSONB, status TEXT, error TEXT, started_at TIMESTAMPTZ, completed_at TIMESTAMPTZ)"
  },
  "new_migration": "db/migrations/0004_exec_plans.sql",
  "frontend_contract": "see contract-m3-4.md for the UI",
  "ts_type_unchanged": false,
  "tdd_target": "12-15 tests covering: plan creation, approval, execute happy path, execute with blocked tool (403/422), rate limit enforcement (11th call → 429), audit row inserted, exec_plans status transitions"
}