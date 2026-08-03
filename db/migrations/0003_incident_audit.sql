-- ============================================================================
-- M2-6: incident_audit table — records every state transition.
-- Provides replay + compliance trail.
-- ============================================================================

CREATE TABLE IF NOT EXISTS incident_audit (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL,
    from_status TEXT,
    to_status TEXT NOT NULL,
    actor TEXT NOT NULL DEFAULT 'system',  -- username or 'system' for /replay
    reason TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_incident_audit_alert ON incident_audit(alert_id, created_at DESC);