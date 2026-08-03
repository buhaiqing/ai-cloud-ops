package worker

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/buhaiqing/ai-cloud-ops/internal/config"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockPool struct {
	query func(context.Context, string, ...any) (pgx.Rows, error)
	exec  func(context.Context, string, ...any) (pgconn.CommandTag, error)
}

func (m *mockPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if m.query != nil {
		return m.query(ctx, sql, args...)
	}
	return emptyRows{}, nil
}

func (m *mockPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	if m.exec != nil {
		return m.exec(ctx, sql, args...)
	}
	return pgconn.NewCommandTag("OK"), nil
}

type emptyRows struct{}

func (emptyRows) Close()                                       {}
func (emptyRows) Err() error                                   { return nil }
func (emptyRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT 0") }
func (emptyRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (emptyRows) Next() bool                                   { return false }
func (emptyRows) Scan(...any) error                            { return fmt.Errorf("scan called on empty rows") }
func (emptyRows) Values() ([]any, error)                       { return nil, nil }
func (emptyRows) RawValues() [][]byte                          { return nil }
func (emptyRows) Conn() *pgx.Conn                              { return nil }

func newTestWorker(pool database) *Worker {
	w := newWorker(pool, &config.Config{})
	w.interval = time.Hour
	w.logger = slog.New(slog.NewTextHandler(io.Discard, nil))
	return w
}

func TestStartSpawnsGoroutineAndStopWaits(t *testing.T) {
	queryContext := make(chan context.Context, 1)
	releaseQuery := make(chan struct{})
	pool := &mockPool{query: func(ctx context.Context, _ string, _ ...any) (pgx.Rows, error) {
		queryContext <- ctx
		<-releaseQuery
		return emptyRows{}, nil
	}}
	w := newTestWorker(pool)

	if err := w.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	var cycleCtx context.Context
	select {
	case cycleCtx = <-queryContext:
	case <-time.After(time.Second):
		t.Fatal("Start() did not launch the polling goroutine")
	}

	stopped := make(chan error, 1)
	go func() { stopped <- w.Stop() }()
	select {
	case <-cycleCtx.Done():
	case <-time.After(time.Second):
		t.Fatal("Stop() did not cancel the polling context")
	}
	select {
	case err := <-stopped:
		t.Fatalf("Stop() returned before the active cycle finished: %v", err)
	default:
	}

	close(releaseQuery)
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Stop() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Stop() did not wait for the polling goroutine")
	}
}

func TestStopIsIdempotent(t *testing.T) {
	heartbeat := make(chan struct{}, 1)
	pool := &mockPool{exec: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "worker_heartbeat") {
			heartbeat <- struct{}{}
		}
		return pgconn.NewCommandTag("OK"), nil
	}}
	w := newTestWorker(pool)

	if err := w.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-heartbeat:
	case <-time.After(time.Second):
		t.Fatal("worker did not complete its first cycle")
	}
	if err := w.Stop(); err != nil {
		t.Fatalf("first Stop() error = %v", err)
	}
	if err := w.Stop(); err != nil {
		t.Fatalf("second Stop() error = %v", err)
	}
}

func TestContextCancellationStopsWorker(t *testing.T) {
	heartbeat := make(chan struct{}, 1)
	pool := &mockPool{exec: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "worker_heartbeat") {
			heartbeat <- struct{}{}
		}
		return pgconn.NewCommandTag("OK"), nil
	}}
	w := newTestWorker(pool)
	ctx, cancel := context.WithCancel(context.Background())

	if err := w.Start(ctx); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	select {
	case <-heartbeat:
	case <-time.After(time.Second):
		t.Fatal("worker did not complete its first cycle")
	}
	cancel()

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("worker did not stop after context cancellation")
	}
	if err := w.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
}

func TestHeartbeatWrittenEachCycle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var heartbeats atomic.Int32
	pool := &mockPool{exec: func(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
		if strings.Contains(sql, "worker_heartbeat") && heartbeats.Add(1) == 2 {
			cancel()
		}
		return pgconn.NewCommandTag("OK"), nil
	}}
	w := newTestWorker(pool)
	w.interval = time.Millisecond

	w.Run(ctx)
	if got := heartbeats.Load(); got != 2 {
		t.Fatalf("heartbeat writes = %d, want 2", got)
	}
}
