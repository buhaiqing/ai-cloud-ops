package credentials

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"go.uber.org/zap"
)

// fakeAssumer is a controllable STS Assumer for tests.
type fakeAssumer struct {
	mu          sync.Mutex
	callCount   int
	expireAfter time.Duration // duration until returned creds expire
	failWith    error         // if non-nil, return this error
}

func (f *fakeAssumer) AssumeRole(ctx context.Context, account, roleARN string, dur int) (*StsCreds, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callCount++
	if f.failWith != nil {
		return nil, f.failWith
	}
	return &StsCreds{
		AccessKeyID:     "ak-" + account,
		AccessKeySecret: "secret",
		SecurityToken:   "token",
		Expiration:      time.Now().Add(f.expireAfter),
	}, nil
}

func TestFirstCallMissesCache(t *testing.T) {
	fa := &fakeAssumer{expireAfter: time.Hour}
	c := New(fa, zap.NewNop())
	creds, err := c.Get(context.Background(), "prod", "arn")
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if creds.AccessKeyID != "ak-prod" {
		t.Errorf("got ak=%q, want ak-prod", creds.AccessKeyID)
	}
	if fa.callCount != 1 {
		t.Errorf("got callCount=%d, want 1", fa.callCount)
	}
	if c.Size() != 1 {
		t.Errorf("got size=%d, want 1", c.Size())
	}
}

func TestSecondCallHitsCache(t *testing.T) {
	fa := &fakeAssumer{expireAfter: time.Hour}
	c := New(fa, zap.NewNop())
	_, _ = c.Get(context.Background(), "prod", "arn")
	_, _ = c.Get(context.Background(), "prod", "arn")
	if fa.callCount != 1 {
		t.Errorf("expected 1 call (cache hit), got %d", fa.callCount)
	}
}

func TestInvalidateTriggersRefresh(t *testing.T) {
	fa := &fakeAssumer{expireAfter: time.Hour}
	c := New(fa, zap.NewNop())
	_, _ = c.Get(context.Background(), "prod", "arn")
	c.Invalidate("prod")
	_, _ = c.Get(context.Background(), "prod", "arn")
	if fa.callCount != 2 {
		t.Errorf("expected 2 calls after invalidate, got %d", fa.callCount)
	}
}

func TestDifferentAccountsCachedSeparately(t *testing.T) {
	fa := &fakeAssumer{expireAfter: time.Hour}
	c := New(fa, zap.NewNop())
	_, _ = c.Get(context.Background(), "prod", "arn-prod")
	_, _ = c.Get(context.Background(), "staging", "arn-staging")
	if fa.callCount != 2 {
		t.Errorf("expected 2 calls, got %d", fa.callCount)
	}
}

func TestExpiryTriggersRefresh(t *testing.T) {
	// Token expires in 60s — within the 300s refresh margin
	fa := &fakeAssumer{expireAfter: 60 * time.Second}
	c := New(fa, zap.NewNop())
	_, _ = c.Get(context.Background(), "prod", "arn")
	_, _ = c.Get(context.Background(), "prod", "arn")
	if fa.callCount != 2 {
		t.Errorf("expected refresh on near-expiry, got %d calls", fa.callCount)
	}
}

func TestAssumeRoleErrorPropagates(t *testing.T) {
	fa := &fakeAssumer{failWith: errors.New("assume role failed")}
	c := New(fa, zap.NewNop())
	_, err := c.Get(context.Background(), "prod", "arn")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}