-- ============================================================================
-- M2-4: alert_rules table for the Rules CRUD UI.
-- Light schema; the engine that actually evaluates rules against CloudMonitor
-- lives in the worker (Phase 4 — separate scope).
-- ============================================================================

CREATE TABLE IF NOT EXISTS alert_rules (
    id BIGSERIAL PRIMARY KEY,
    account_alias TEXT NOT NULL,
    name TEXT NOT NULL,
    severity TEXT NOT NULL,
    metric TEXT NOT NULL,
    threshold NUMERIC,
    channel JSONB NOT NULL DEFAULT '{}'::jsonb,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_alert_rules_account ON alert_rules(account_alias);
CREATE INDEX IF NOT EXISTS idx_alert_rules_enabled ON alert_rules(enabled) WHERE enabled = TRUE;