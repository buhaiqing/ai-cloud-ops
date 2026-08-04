-- ============================================================================
-- M3-5: HITL execution plans + audit trail.
-- exec_plans: one row per dry-run plan; status follows the M3-4 state machine
--   planned → approved|rejected; approved → running; running → completed|failed;
--   failed → rolled_back|completed.
-- exec_audit: one row per PlannedAction executed, ordered by seq.
-- Column set = contract-m3-5.md schema ∪ the fields its own GET endpoint
-- returns (actions_total/completed, started_at/completed_at, approver_note).
-- ============================================================================

CREATE TABLE IF NOT EXISTS exec_plans (
    id BIGSERIAL PRIMARY KEY,
    diagnosis_id BIGINT NOT NULL,          -- analyses.id (loose ref, like analyses.alert_id)
    account_alias TEXT NOT NULL DEFAULT '',
    dry_run BOOLEAN NOT NULL DEFAULT TRUE,
    status TEXT NOT NULL DEFAULT 'planned'
        CHECK (status IN ('planned','approved','running','completed','failed','rolled_back','rejected')),
    would_execute JSONB NOT NULL DEFAULT '[]'::jsonb,
    blocked_by_policy JSONB NOT NULL DEFAULT '[]'::jsonb,
    approver_note TEXT,
    created_by TEXT NOT NULL DEFAULT 'system',
    approved_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    approved_at TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    actions_total INTEGER NOT NULL DEFAULT 0,
    actions_completed INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_exec_plans_status ON exec_plans(status, created_at DESC);
-- Rate-limit lookups: per-account executions started within the last hour.
CREATE INDEX IF NOT EXISTS idx_exec_plans_account_started ON exec_plans(account_alias, started_at DESC);

CREATE TABLE IF NOT EXISTS exec_audit (
    id BIGSERIAL PRIMARY KEY,
    exec_id BIGINT NOT NULL REFERENCES exec_plans(id) ON DELETE CASCADE,
    seq INTEGER NOT NULL,
    action JSONB NOT NULL DEFAULT '{}'::jsonb,     -- full PlannedAction snapshot (carries rollback hook)
    action_name TEXT NOT NULL DEFAULT '',
    target_resource TEXT NOT NULL DEFAULT '',
    pre_state JSONB,
    post_state JSONB,
    status TEXT NOT NULL DEFAULT 'pending',        -- pending | success | failed | rolled_back
    error TEXT,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    UNIQUE (exec_id, seq)
);
CREATE INDEX IF NOT EXISTS idx_exec_audit_exec ON exec_audit(exec_id, seq);
