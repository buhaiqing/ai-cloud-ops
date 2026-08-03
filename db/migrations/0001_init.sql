-- ============================================================================
-- M1 initial schema (T11)
-- Per design.md decision T11: alerts table partitioned by month, key indexes,
-- UNIQUE constraint on alert_id for idempotency (T4).
-- ============================================================================

-- Enable extensions
CREATE EXTENSION IF NOT EXISTS pgcrypto;  -- gen_random_uuid()

-- ---------------------------------------------------------------------------
-- Accounts / regions (lightweight config cache; source of truth is YAML)
-- ---------------------------------------------------------------------------
CREATE TABLE accounts (
    id BIGSERIAL PRIMARY KEY,
    alias TEXT UNIQUE NOT NULL,
    role_arn TEXT NOT NULL,
    regions JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_accounts_alias ON accounts(alias);

-- ---------------------------------------------------------------------------
-- Resources (cached metadata for AI Agent context — T10)
-- ---------------------------------------------------------------------------
CREATE TABLE resources (
    id BIGSERIAL PRIMARY KEY,
    account_alias TEXT NOT NULL REFERENCES accounts(alias) ON DELETE CASCADE,
    region TEXT NOT NULL,
    resource_type TEXT NOT NULL,  -- 'ECS', 'RDS', 'SLB', 'OSS', ...
    resource_id TEXT NOT NULL,
    name TEXT,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_alias, region, resource_type, resource_id)
);
CREATE INDEX idx_resources_lookup ON resources(account_alias, region, resource_type, resource_id);

-- ---------------------------------------------------------------------------
-- Alerts — partitioned monthly, key composite index, UNIQUE on alert_id (T4)
-- ---------------------------------------------------------------------------
CREATE TABLE alerts (
    id BIGSERIAL,
    alert_id TEXT NOT NULL,
    account_alias TEXT NOT NULL,
    region TEXT NOT NULL,
    severity TEXT NOT NULL,  -- 'critical', 'warning', 'info'
    resource_type TEXT,
    resource_id TEXT,
    name TEXT,
    metric JSONB,
    tags JSONB NOT NULL DEFAULT '{}'::jsonb,
    payload JSONB NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',  -- 'open', 'acknowledged', 'suppressed', 'resolved'
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (id, created_at),
    UNIQUE (alert_id, created_at)  -- idempotency: same alert_id within same month = no dup
) PARTITION BY RANGE (created_at);

-- Initial partitions: current month + next month
CREATE TABLE alerts_default PARTITION OF alerts DEFAULT;
CREATE TABLE alerts_y2026m08 PARTITION OF alerts
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE alerts_y2026m09 PARTITION OF alerts
    FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE alerts_y2026m10 PARTITION OF alerts
    FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE alerts_y2026m11 PARTITION OF alerts
    FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');

-- Key composite indexes (T11)
-- "Show recent open alerts for an account" → (account_alias, status, created_at DESC)
CREATE INDEX idx_alerts_account_recent ON alerts(account_alias, status, created_at DESC);
-- "Show alerts for a specific resource" → (account_alias, resource_type, resource_id, created_at DESC)
CREATE INDEX idx_alerts_resource_history ON alerts(account_alias, resource_type, resource_id, created_at DESC);
-- "Show all open critical alerts" → (severity, status, created_at DESC) WHERE status='open'
CREATE INDEX idx_alerts_open_critical ON alerts(severity, created_at DESC) WHERE status = 'open';

-- ---------------------------------------------------------------------------
-- Analyses — AI Agent outputs (T6 / T9)
-- ---------------------------------------------------------------------------
CREATE TABLE analyses (
    id BIGSERIAL PRIMARY KEY,
    alert_id BIGINT NOT NULL,
    model TEXT NOT NULL,  -- 'claude-sonnet-4-5', 'gpt-4o', 'qwen-max', ...
    prompt_version TEXT NOT NULL,  -- git SHA or semver of prompt template
    score JSONB,  -- 5-dim eval scores from LLM-as-judge (T9)
    root_cause TEXT NOT NULL,
    recommendations JSONB NOT NULL DEFAULT '[]'::jsonb,
    evidence_chains JSONB NOT NULL DEFAULT '[]'::jsonb,
    raw_response JSONB,  -- full LLM response for debugging
    latency_ms INTEGER NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- alert_id here references alerts.id; partitioned parent makes FK tricky,
    -- so we store the alert_id only (no FK constraint)
);
CREATE INDEX idx_analyses_alert ON analyses(alert_id);
CREATE INDEX idx_analyses_created ON analyses(created_at DESC);

-- ---------------------------------------------------------------------------
-- Dead letter queue — failed ingestion/AI analysis jobs (T6)
-- ---------------------------------------------------------------------------
CREATE TABLE dlq (
    id BIGSERIAL PRIMARY KEY,
    job_type TEXT NOT NULL,  -- 'ingest', 'analyze', 'webhook'
    payload JSONB NOT NULL,
    error_message TEXT NOT NULL,
    error_class TEXT,  -- exception class name
    retry_count INTEGER NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMPTZ NOT NULL,
    next_retry_at TIMESTAMPTZ,
    resolved_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_dlq_unresolved ON dlq(next_retry_at) WHERE resolved_at IS NULL;
CREATE INDEX idx_dlq_created ON dlq(created_at DESC);

-- ---------------------------------------------------------------------------
-- Health / observability
-- ---------------------------------------------------------------------------
CREATE TABLE worker_heartbeat (
    worker_id TEXT PRIMARY KEY,
    last_heartbeat_at TIMESTAMPTZ NOT NULL DEFAULT now()
);