package db

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

func TestInit_EmptyDSN(t *testing.T) {
	resetForTest()
	t.Setenv("DATABASE_URL", "")
	if _, err := Init(context.Background(), ""); err == nil {
		t.Fatal("expected error for empty DSN")
	}
}

func TestInit_InvalidDSN(t *testing.T) {
	resetForTest()
	// "not a url" can't be parsed by pgxpool.ParseConfig
	if _, err := Init(context.Background(), "not a url"); err == nil {
		t.Fatal("expected error for malformed DSN")
	}
}

func TestInit_EnvFallback(t *testing.T) {
	resetForTest()
	t.Setenv("DATABASE_URL", "not a url")
	// dsn == "", should read env, should still error from parsing
	if _, err := Init(context.Background(), ""); err == nil {
		t.Fatal("expected error from env DSN parse")
	}
}

func TestInit_ValidDSN_NoConnect(t *testing.T) {
	resetForTest()
	// ParseConfig + NewWithConfig both succeed when only the URL is set;
	// the first Acquire would dial. We just verify config parsing and struct setup.
	dsn := "postgres://user:pass@localhost:5432/dbname?sslmode=disable"
	p, err := Init(context.Background(), dsn)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if p == nil {
		t.Fatal("Init returned nil pool")
	}
	if Pool() != p {
		t.Fatal("Pool() should return the same singleton")
	}
	// Idempotent: second call returns the same pool.
	p2, err := Init(context.Background(), dsn)
	if err != nil || p2 != p {
		t.Fatalf("Init should be idempotent: p=%v p2=%v err=%v", p, p2, err)
	}
	p.Close()
	resetForTest()
}

func TestPool_BeforeInit(t *testing.T) {
	resetForTest()
	if Pool() != nil {
		t.Fatal("Pool() before Init should be nil")
	}
}

func TestWithTx_BeforeInit(t *testing.T) {
	resetForTest()
	err := WithTx(context.Background(), func(_ pgx.Tx) error { return nil })
	if err == nil {
		t.Fatal("WithTx before Init should error")
	}
}
