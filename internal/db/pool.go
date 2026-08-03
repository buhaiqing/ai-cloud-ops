// Package db is the PostgreSQL access layer for ai-cloud-ops.
//
// Ponytail: pgxpool singleton + tx wrapper. No ORM, raw SQL.
package db

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	mu   sync.Mutex
	pool *pgxpool.Pool
	log  = slog.Default()
)

// Init creates (or returns) the singleton pool. Idempotent; the first
// successful call wins. dsn may be empty to fall back to $DATABASE_URL.
func Init(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	mu.Lock()
	defer mu.Unlock()
	if pool != nil {
		return pool, nil
	}
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		return nil, fmt.Errorf("DATABASE_URL is empty")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse dsn: %w", err)
	}
	p, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	pool = p
	return pool, nil
}

// resetForTest clears the singleton. Test-only.
func resetForTest() {
	mu.Lock()
	defer mu.Unlock()
	if pool != nil {
		pool.Close()
	}
	pool = nil
}

// Pool returns the singleton, or nil if Init has not succeeded.
func Pool() *pgxpool.Pool {
	mu.Lock()
	defer mu.Unlock()
	return pool
}

// WithTx runs fn inside a transaction. Rolls back on error or panic.
func WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	p := Pool()
	if p == nil {
		return fmt.Errorf("db pool not initialised")
	}
	tx, err := p.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(ctx)
			panic(r)
		}
	}()
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil {
			return fmt.Errorf("%w (rollback: %v)", err, rbErr)
		}
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}
