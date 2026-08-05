package worker

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/buhaiqing/ai-cloud-ops/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultPollInterval = 30 * time.Second
	workerID            = "alert-worker"

	selectUnanalyzedAlerts = `SELECT a.id, a.created_at
		FROM alerts AS a
		LEFT JOIN analyses AS analysis ON analysis.alert_id = a.id
		WHERE analysis.id IS NULL AND a.status <> 'analyzed'
		ORDER BY a.created_at DESC
		LIMIT 50`
	markAlertAnalyzed = `UPDATE alerts
		SET status = 'analyzed', updated_at = $1
		WHERE id = $2 AND created_at = $3`
	upsertHeartbeat = `INSERT INTO worker_heartbeat (worker_id, last_heartbeat_at)
		VALUES ($1, $2)
		ON CONFLICT (worker_id) DO UPDATE
		SET last_heartbeat_at = EXCLUDED.last_heartbeat_at`
)

type database interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type alertKey struct {
	id        int64
	createdAt time.Time
}

// Worker polls for alerts that still need analysis.
type Worker struct {
	pool     database
	cfg      *config.Config
	interval time.Duration
	logger   *slog.Logger

	lifecycle sync.Mutex
	cancel    context.CancelFunc
	wg        sync.WaitGroup
}

// New creates a stopped worker with a 30-second poll interval.
func New(pool *pgxpool.Pool, cfg *config.Config) *Worker {
	if pool == nil {
		return newWorker(nil, cfg)
	}
	return newWorker(pool, cfg)
}

func newWorker(pool database, cfg *config.Config) *Worker {
	return &Worker{
		pool:     pool,
		cfg:      cfg,
		interval: defaultPollInterval,
		logger:   slog.Default(),
	}
}

// Start launches the polling loop. Repeated calls while running are no-ops.
func (w *Worker) Start(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("start worker: nil context")
	}

	w.lifecycle.Lock()
	defer w.lifecycle.Unlock()
	if w.cancel != nil {
		return nil
	}
	if w.pool == nil {
		return fmt.Errorf("start worker: nil database pool")
	}

	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.wg.Go(func() {
		w.Run(runCtx)
	})
	return nil
}

// Stop cancels the polling loop and waits for its goroutine to finish.
func (w *Worker) Stop() error {
	w.lifecycle.Lock()
	defer w.lifecycle.Unlock()
	if w.cancel == nil {
		return nil
	}

	w.cancel()
	w.wg.Wait()
	w.cancel = nil
	return nil
}

// Run polls immediately, then waits the configured interval between cycles.
func (w *Worker) Run(ctx context.Context) {
	interval := w.interval
	if interval <= 0 {
		interval = defaultPollInterval
	}
	logger := w.logger
	if logger == nil {
		logger = slog.Default()
	}

	logger.Info("worker.started", "poll_interval", interval)
	defer logger.Info("worker.stopped")

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		startedAt := time.Now()
		processed, err := w.runCycle(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			logger.Error("worker.cycle.failed", "err", err)
		} else {
			logger.Info("worker.cycle.complete", "processed", processed, "duration", time.Since(startedAt))
		}

		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (w *Worker) runCycle(ctx context.Context) (int, error) {
	rows, err := w.pool.Query(ctx, selectUnanalyzedAlerts)
	if err != nil {
		return 0, fmt.Errorf("query unanalyzed alerts: %w", err)
	}

	alerts := make([]alertKey, 0, 50)
	for rows.Next() {
		var alert alertKey
		if err := rows.Scan(&alert.id, &alert.createdAt); err != nil {
			rows.Close()
			return 0, fmt.Errorf("scan unanalyzed alert: %w", err)
		}
		alerts = append(alerts, alert)
	}
	rowsErr := rows.Err()
	rows.Close()
	if rowsErr != nil {
		return 0, fmt.Errorf("iterate unanalyzed alerts: %w", rowsErr)
	}

	processed := 0
	var cycleErr error
	for _, alert := range alerts {
		if ctx.Err() != nil {
			return processed, fmt.Errorf("analyze alerts: %w", ctx.Err())
		}

		// ponytail: AI analysis is intentionally a stub until the Go agent client lands.
		w.logger.Debug("worker.alert.analyze.stub", "alert_id", alert.id)
		if _, err := w.pool.Exec(ctx, markAlertAnalyzed, time.Now().UTC(), alert.id, alert.createdAt); err != nil {
			cycleErr = errors.Join(cycleErr, fmt.Errorf("mark alert %d analyzed: %w", alert.id, err))
			continue
		}
		processed++
	}

	if _, err := w.pool.Exec(ctx, upsertHeartbeat, workerID, time.Now().UTC()); err != nil {
		cycleErr = errors.Join(cycleErr, fmt.Errorf("write heartbeat: %w", err))
	}
	return processed, cycleErr
}
