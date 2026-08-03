package db

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// RunMigrations applies every *.sql file in dir, in lexical order.
// Idempotent: a schema_migrations table records applied filenames.
func RunMigrations(ctx context.Context, pool *pgxpool.Pool, dir string) error {
	if pool == nil {
		return fmt.Errorf("nil pool")
	}
	if err := ensureMigrationsTable(ctx, pool); err != nil {
		return err
	}
	files := listMigrationFiles(dir)
	applied, err := loadApplied(ctx, pool)
	if err != nil {
		return err
	}
	for _, name := range files {
		if applied[name] {
			log.Info("skip migration (already applied)", "name", name)
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if err := applyOne(ctx, pool, name, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		log.Info("applied migration", "name", name)
	}
	return nil
}

// listMigrationFiles returns *.sql entries in dir, alphabetically sorted.
func listMigrationFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out
}

func ensureMigrationsTable(ctx context.Context, pool *pgxpool.Pool) error {
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`)
	return err
}

func loadApplied(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `SELECT name FROM schema_migrations`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, err
		}
		out[n] = true
	}
	return out, rows.Err()
}

// ponytail: schema_migrations insert is in the same tx as the migration body
// so partial applies cannot leave the DB half-migrated.
func applyOne(ctx context.Context, pool *pgxpool.Pool, name, body string) error {
	return WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, body); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO schema_migrations(name) VALUES ($1)`, name)
		return err
	})
}
