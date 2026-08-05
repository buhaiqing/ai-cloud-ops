// Package credentials implements STS Token LRU cache (T1).
//
// Mirrors ai_cloud_ops.credentials.StsTokenCache in Python.
// Constants:
//   - TTL: 2700s (45 min, refresh 15 min before STS's 1-hour default expiry)
//   - Refresh margin: 300s
//
// Cache semantics:
//   - In-process map keyed by account alias
//   - Mutex protects the map; singleflight prevents thundering-herd on concurrent gets
//   - On expiry (within refresh margin), refresh via STS AssumeRole
package credentials

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"go.uber.org/zap"
)

const (
	DefaultTTLSeconds         = 2700
	DefaultRefreshMargin      = 300
	stsCacheRefreshSkewBuffer = 30 // small buffer for clock skew
)

// StsCreds holds temporary credentials for an Alibaba Cloud RAM role.
type StsCreds struct {
	AccessKeyID     string
	AccessKeySecret string
	SecurityToken   string
	Expiration      time.Time
}

// STSAssumer is the interface for the STS client (real impl wraps aliyun SDK).
type STSAssumer interface {
	AssumeRole(ctx context.Context, account, roleARN string, durationSeconds int) (*StsCreds, error)
}

var (
	cacheHits = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_cloud_ops_sts_cache_hits_total",
			Help: "Number of STS token cache hits",
		},
		[]string{"account"},
	)
	cacheMisses = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "ai_cloud_ops_sts_cache_misses_total",
			Help: "Number of STS token cache misses",
		},
		[]string{"account"},
	)
	cacheSize = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "ai_cloud_ops_sts_cache_size",
			Help: "Number of STS tokens currently cached",
		},
	)
)

func init() {
	prometheus.MustRegister(cacheHits, cacheMisses, cacheSize)
}

// StsTokenCache is an in-process LRU-style cache for STS tokens.
type StsTokenCache struct {
	assumer       STSAssumer
	ttl           time.Duration
	refreshMargin time.Duration
	clock         func() time.Time // injectable for tests
	logger        *zap.Logger

	mu    sync.Mutex
	store map[string]*stsEntry

	// singleflight keyed by account: collapse concurrent fetches for the same account
	sfGroup singleflight
}

type stsEntry struct {
	creds      *StsCreds
	expiration time.Time
}

// New constructs a cache. logger may be nil (uses nop).
func New(assumer STSAssumer, logger *zap.Logger) *StsTokenCache {
	if logger == nil {
		logger = zap.NewNop()
	}
	return &StsTokenCache{
		assumer:       assumer,
		ttl:           DefaultTTLSeconds * time.Second,
		refreshMargin: DefaultRefreshMargin * time.Second,
		clock:         time.Now,
		logger:        logger,
		store:         make(map[string]*stsEntry),
		sfGroup:       singleflight{},
	}
}

// Get returns STS credentials for an account, refreshing if expired.
func (c *StsTokenCache) Get(ctx context.Context, account, roleARN string) (*StsCreds, error) {
	// Fast path: hit (no lock needed for read with monotonic check)
	c.mu.Lock()
	entry, ok := c.store[account]
	now := c.clock()
	if ok && entry.expiration.After(now.Add(c.refreshMargin)) {
		c.mu.Unlock()
		cacheHits.WithLabelValues(account).Inc()
		return entry.creds, nil
	}
	c.mu.Unlock()
	cacheMisses.WithLabelValues(account).Inc()

	// Singleflight: collapse concurrent fetches
	creds, err, _ := c.sfGroup.Do(account, func() (any, error) {
		return c.assumer.AssumeRole(ctx, account, roleARN, int((c.ttl + c.refreshMargin).Seconds()))
	})
	if err != nil {
		return nil, fmt.Errorf("assume role: %w", err)
	}
	stCreds, ok := creds.(*StsCreds)
	if !ok {
		return nil, errors.New("assumer returned wrong type")
	}

	// Translate wall-clock expiration to monotonic: store the wall time,
	// check it against wall clock on read.
	c.mu.Lock()
	c.store[account] = &stsEntry{creds: stCreds, expiration: stCreds.Expiration}
	cacheSize.Set(float64(len(c.store)))
	c.mu.Unlock()
	return stCreds, nil
}

// Invalidate drops the cached creds for an account (e.g., after AssumeRole 403).
func (c *StsTokenCache) Invalidate(account string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.store, account)
	cacheSize.Set(float64(len(c.store)))
}

// Size returns the current number of cached accounts.
func (c *StsTokenCache) Size() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.store)
}
