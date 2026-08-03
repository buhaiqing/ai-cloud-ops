package db

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AlertRow mirrors one row of the alerts table (T11).
// metric/tags/payload are stored as JSONB; we expose them as json.RawMessage.
type AlertRow struct {
	ID           int64           `db:"id"`
	AlertID      string          `db:"alert_id"`
	AccountAlias string          `db:"account_alias"`
	Region       string          `db:"region"`
	Severity     string          `db:"severity"`
	ResourceType *string         `db:"resource_type"`
	ResourceID   *string         `db:"resource_id"`
	Name         *string         `db:"name"`
	Metric       json.RawMessage `db:"metric"`
	Tags         json.RawMessage `db:"tags"`
	Payload      json.RawMessage `db:"payload"`
	Status       string          `db:"status"`
	CreatedAt    time.Time       `db:"created_at"`
	UpdatedAt    time.Time       `db:"updated_at"`
}

// InsertAlert inserts one alert. ON CONFLICT DO NOTHING makes the UNIQUE
// (alert_id, created_at) constraint idempotent (T4).
func InsertAlert(ctx context.Context, pool *pgxpool.Pool, a AlertRow) error {
	if a.CreatedAt.IsZero() {
		a.CreatedAt = time.Now().UTC()
	}
	if a.Status == "" {
		a.Status = "open"
	}
	if len(a.Tags) == 0 {
		a.Tags = []byte("{}")
	}
	_, err := pool.Exec(ctx, `INSERT INTO alerts
		(alert_id, account_alias, region, severity, resource_type, resource_id,
		 name, metric, tags, payload, status, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12, now())
		ON CONFLICT (alert_id, created_at) DO NOTHING`,
		a.AlertID, a.AccountAlias, a.Region, a.Severity,
		a.ResourceType, a.ResourceID, a.Name,
		a.Metric, a.Tags, a.Payload, a.Status, a.CreatedAt,
	)
	return err
}

// GetAlertByID returns the most recent alert with that alert_id.
func GetAlertByID(ctx context.Context, pool *pgxpool.Pool, accountAlias, region, alertID string) (*AlertRow, error) {
	row := pool.QueryRow(ctx, `SELECT id, alert_id, account_alias, region, severity,
		resource_type, resource_id, name, metric, tags, payload, status, created_at, updated_at
		FROM alerts WHERE alert_id=$1 AND account_alias=$2 AND region=$3
		ORDER BY created_at DESC LIMIT 1`,
		alertID, accountAlias, region,
	)
	var a AlertRow
	if err := row.Scan(&a.ID, &a.AlertID, &a.AccountAlias, &a.Region, &a.Severity,
		&a.ResourceType, &a.ResourceID, &a.Name, &a.Metric, &a.Tags, &a.Payload,
		&a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
		if err == pgx.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

// ListRecentAlerts returns open alerts newer than now-hoursBack, capped at limit.
func ListRecentAlerts(ctx context.Context, pool *pgxpool.Pool, accountAlias, region string, hoursBack, limit int) ([]AlertRow, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}
	rows, err := pool.Query(ctx, `SELECT id, alert_id, account_alias, region, severity,
		resource_type, resource_id, name, metric, tags, payload, status, created_at, updated_at
		FROM alerts WHERE account_alias=$1 AND region=$2 AND status='open'
		  AND created_at > now() - ($3 || ' hours')::interval
		ORDER BY created_at DESC LIMIT $4`,
		accountAlias, region, hoursBack, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AlertRow, 0, limit)
	for rows.Next() {
		var a AlertRow
		if err := rows.Scan(&a.ID, &a.AlertID, &a.AccountAlias, &a.Region, &a.Severity,
			&a.ResourceType, &a.ResourceID, &a.Name, &a.Metric, &a.Tags, &a.Payload,
			&a.Status, &a.CreatedAt, &a.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
