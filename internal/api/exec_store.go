// M3-5 production wiring: pgxExecStore implements ExecStore + ExecLister
// against the schema in db/migrations/0004_exec_plans.sql.
//
// Code paths here are exercised by the in-memory fake in exec_test.go
// during `go test ./internal/api/`. A real Postgres connection is needed
// only when cmd/serve starts the API for an environment that has one.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/buhaiqing/ai-cloud-ops/internal/agent"
)

// pgxExecStore is a thin wrapper around a pgx connection pool. It exists
// so the same ExecStore contract used by the in-memory fake powers the
// production wiring — handlers stay free of SQL.
type pgxExecStore struct {
	pool *pgxpool.Pool
}

// newPgxExecStore builds a pgxExecStore backed by pool.
func newPgxExecStore(pool *pgxpool.Pool) *pgxExecStore {
	return &pgxExecStore{pool: pool}
}

// Compile-time interface satisfaction checks.
var (
	_ ExecStore  = (*pgxExecStore)(nil)
	_ ExecLister = (*pgxExecStore)(nil)
)

const planColumns = `id, diagnosis_id, account_alias, dry_run,
       would_execute, blocked_by_policy, status, approver_note,
       created_by, approved_by, created_at, approved_at,
       started_at, completed_at, actions_total, actions_completed`

func scanPlan(row pgx.Row, p *ExecPlanRecord) error {
	return row.Scan(
		&p.ID, &p.DiagnosisID, &p.AccountAlias, &p.DryRun,
		&p.WouldExecute, &p.BlockedByPolicy, &p.Status, &p.ApproverNote,
		&p.CreatedBy, &p.ApprovedBy, &p.CreatedAt, &p.ApprovedAt,
		&p.StartedAt, &p.CompletedAt, &p.ActionsTotal, &p.ActionsCompleted,
	)
}

// CreatePlan inserts a new planned row and returns the assigned id +
// server-side created_at.
func (s *pgxExecStore) CreatePlan(ctx context.Context, p ExecPlanRecord) (int64, error) {
	row := s.pool.QueryRow(ctx,
		`INSERT INTO exec_plans
           (diagnosis_id, account_alias, dry_run, status,
            would_execute, blocked_by_policy, created_by, actions_total)
         VALUES ($1, $2, $3, 'planned', $4, $5, $6, $7)
         RETURNING id, created_at`,
		p.DiagnosisID, p.AccountAlias, p.DryRun,
		p.WouldExecute, p.BlockedByPolicy, p.CreatedBy, p.ActionsTotal)
	if err := row.Scan(&p.ID, &p.CreatedAt); err != nil {
		return 0, fmt.Errorf("insert exec_plan: %w", err)
	}
	p.Status = "planned"
	return p.ID, nil
}

// GetPlan returns (nil, nil) when no row matches — handlers translate that
// to 404.
func (s *pgxExecStore) GetPlan(ctx context.Context, id int64) (*ExecPlanRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+planColumns+` FROM exec_plans WHERE id = $1`, id)
	var p ExecPlanRecord
	if err := scanPlan(row, &p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select exec_plan: %w", err)
	}
	return &p, nil
}

// LoadDiagnosisAlert joins analyses→alerts to surface the alert map +
// account the dry-run planner needs. Returns (nil, "", nil) when no
// analysis matches the given id.
func (s *pgxExecStore) LoadDiagnosisAlert(ctx context.Context, diagnosisID int64) (map[string]any, string, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT a.alert_id, a.account_alias, a.region, a.severity,
                a.resource_type, a.resource_id, a.name, a.metric,
                a.tags, a.payload
         FROM analyses an
         JOIN alerts a ON a.id = an.alert_id
         WHERE an.id = $1`, diagnosisID)

	var (
		alertID      string
		accountAlias string
		region       string
		severity     string
		resourceType *string
		resourceID   *string
		name         *string
		metric       json.RawMessage
		tags         json.RawMessage
		payload      json.RawMessage
	)
	if err := row.Scan(&alertID, &accountAlias, &region, &severity,
		&resourceType, &resourceID, &name, &metric, &tags, &payload); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", nil
		}
		return nil, "", fmt.Errorf("load diagnosis alert: %w", err)
	}

	alert := map[string]any{
		"alert_id":      alertID,
		"account_alias": accountAlias,
		"region":        region,
		"severity":      severity,
		"tags":          decodeJSONMap(tags),
		"payload":       decodeJSONMap(payload),
	}
	if resourceType != nil {
		alert["resource_type"] = *resourceType
	}
	if resourceID != nil {
		alert["resource_id"] = *resourceID
	}
	if name != nil {
		alert["name"] = *name
	}
	if len(metric) > 0 {
		alert["metric"] = decodeJSONMap(metric)
	}
	return alert, accountAlias, nil
}

// ApprovePlan moves a row planned→approved atomically. 0 rows affected
// means either the row is missing (→ ErrExecNotFound) or its current
// status forbids the transition (→ ErrExecConflict).
func (s *pgxExecStore) ApprovePlan(ctx context.Context, id int64, approver, note string) (*ExecPlanRecord, error) {
	row := s.pool.QueryRow(ctx,
		`UPDATE exec_plans
            SET status='approved', approved_by=$2,
                approved_at=now(), approver_note=$3
          WHERE id=$1 AND status='planned'
          RETURNING `+planColumns,
		id, approver, note)
	var p ExecPlanRecord
	if err := scanPlan(row, &p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return s.classifyCASMiss(ctx, id)
		}
		return nil, fmt.Errorf("approve exec_plan: %w", err)
	}
	return &p, nil
}

// BeginExecution flips approved→running and seeds one exec_audit row per
// PlannedAction. All writes happen in a single tx so a partial audit
// trail is never visible.
func (s *pgxExecStore) BeginExecution(ctx context.Context, id int64, actions []agent.PlannedAction) error {
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) // safe no-op after Commit

	row := tx.QueryRow(ctx,
		`UPDATE exec_plans
            SET status='running', started_at=now(), actions_total=$2
          WHERE id=$1 AND status='approved'
          RETURNING `+planColumns,
		id, len(actions))
	var p ExecPlanRecord
	if err := scanPlan(row, &p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// Status changed under us — read state to disambiguate.
			tx.Rollback(ctx)
			cur, gerr := s.GetPlan(ctx, id)
			if gerr != nil {
				return gerr
			}
			if cur == nil {
				return ErrExecNotFound
			}
			return ErrExecConflict
		}
		return fmt.Errorf("begin execution: %w", err)
	}

	preState := []byte(`{"status":"captured","source":"stub"}`)
	for seq, a := range actions {
		raw, mErr := json.Marshal(a)
		if mErr != nil {
			return fmt.Errorf("marshal action: %w", mErr)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO exec_audit
                (exec_id, seq, action, action_name, target_resource, pre_state, status)
             VALUES ($1,$2,$3,$4,$5,$6,'pending')`,
			id, seq+1, raw, a.ToolName, a.TargetResource, preState); err != nil {
			return fmt.Errorf("insert exec_audit: %w", err)
		}
	}
	return tx.Commit(ctx)
}

// RecordActionResult updates one audit row with the post-state + outcome.
func (s *pgxExecStore) RecordActionResult(ctx context.Context, execID int64, seq int, postState json.RawMessage, status, errMsg string) error {
	var errPtr *string
	if errMsg != "" {
		errPtr = &errMsg
	}
	// nil postState → SQL NULL.
	var ps any
	if len(postState) > 0 {
		ps = postState
	}
	cmd, err := s.pool.Exec(ctx,
		`UPDATE exec_audit
            SET post_state=$3, status=$4, error=$5, completed_at=now()
          WHERE exec_id=$1 AND seq=$2`,
		execID, seq, ps, status, errPtr)
	if err != nil {
		return fmt.Errorf("update exec_audit: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrExecNotFound
	}
	return nil
}

// FinishExecution stamps the final plan row: status, completed count,
// completed_at.
func (s *pgxExecStore) FinishExecution(ctx context.Context, id int64, status string, completed int) error {
	cmd, err := s.pool.Exec(ctx,
		`UPDATE exec_plans
            SET status=$2, actions_completed=$3, completed_at=now()
          WHERE id=$1`,
		id, status, completed)
	if err != nil {
		return fmt.Errorf("finish exec_plan: %w", err)
	}
	if cmd.RowsAffected() == 0 {
		return ErrExecNotFound
	}
	return nil
}

// CountExecutionsSince counts plans on the given account that have already
// started and are still in flight or recently completed, since `since`.
// Mirrors the in-memory fake's filter exactly so the rate-limit window
// behaves identically.
func (s *pgxExecStore) CountExecutionsSince(ctx context.Context, accountAlias string, since time.Time) (int, error) {
	var count int
	err := s.pool.QueryRow(ctx,
		`SELECT count(*) FROM exec_plans
          WHERE account_alias=$1
            AND started_at IS NOT NULL
            AND started_at >= $2
            AND status IN ('running','completed','failed','rolled_back')`,
		accountAlias, since).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count executions: %w", err)
	}
	return count, nil
}

// AuditRows returns the per-action audit trail ordered by seq.
func (s *pgxExecStore) AuditRows(ctx context.Context, execID int64) ([]ExecAuditRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, exec_id, seq, action, action_name, target_resource,
                pre_state, post_state, status, error,
                started_at, completed_at
           FROM exec_audit
          WHERE exec_id=$1
          ORDER BY seq`, execID)
	if err != nil {
		return nil, fmt.Errorf("query exec_audit: %w", err)
	}
	defer rows.Close()
	var out []ExecAuditRecord
	for rows.Next() {
		var r ExecAuditRecord
		if err := rows.Scan(
			&r.ID, &r.ExecID, &r.Seq, &r.Action, &r.ActionName,
			&r.TargetResource, &r.PreState, &r.PostState,
			&r.Status, &r.Error, &r.StartedAt, &r.CompletedAt,
		); err != nil {
			return nil, fmt.Errorf("scan exec_audit: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// ListExecutions returns the most recent plans, optionally scoped to one
// account. Powers GET /api/v1/executions.
func (s *pgxExecStore) ListExecutions(ctx context.Context, account string, limit int) ([]ExecPlanRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.pool.Query(ctx,
		`SELECT `+planColumns+`
           FROM exec_plans
          WHERE ($1 = '' OR account_alias = $1)
          ORDER BY created_at DESC
          LIMIT $2`, account, limit)
	if err != nil {
		return nil, fmt.Errorf("list exec_plans: %w", err)
	}
	defer rows.Close()
	var out []ExecPlanRecord
	for rows.Next() {
		var p ExecPlanRecord
		if err := scanPlan(rows, &p); err != nil {
			return nil, fmt.Errorf("scan exec_plan: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// classifyCASMiss translates "UPDATE ... RETURNING matched 0 rows" into
// either ErrExecNotFound (row absent) or ErrExecConflict (row present but
// status forbids the transition). Called from ApprovePlan.
func (s *pgxExecStore) classifyCASMiss(ctx context.Context, id int64) (*ExecPlanRecord, error) {
	var status string
	err := s.pool.QueryRow(ctx,
		`SELECT status FROM exec_plans WHERE id=$1`, id).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrExecNotFound
		}
		return nil, fmt.Errorf("classify cas miss: %w", err)
	}
	return nil, ErrExecConflict
}

// decodeJSONMap best-effort decodes a json.RawMessage into a
// map[string]any; on any error returns nil so the planner still gets a
// usable (sparse) alert map.
func decodeJSONMap(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}

// MarkAuditRolledBack flips audit rows with status='success' for the given
// exec and seqs to 'rolled_back'. Only the rows currently in 'success' are
// affected; failed rows (the one that broke the run) are left untouched.
// Returns the count of rows actually updated.
func (s *pgxExecStore) MarkAuditRolledBack(ctx context.Context, execID int64, seqs []int) (int, error) {
	if len(seqs) == 0 {
		return 0, nil
	}
	tag, err := s.pool.Exec(ctx,
		`UPDATE exec_audit
		    SET status = 'rolled_back', completed_at = now()
		  WHERE exec_id = $1
		    AND seq = ANY($2::int[])
		    AND status = 'success'`,
		execID, seqs)
	if err != nil {
		return 0, fmt.Errorf("mark audit rolled back: %w", err)
	}
	return int(tag.RowsAffected()), nil
}
