package credentials

import (
	"sync"
)

// singleflight is a minimal implementation of golang.org/x/sync/singleflight
// (avoiding the external dep for the LRU-cache primitive). Collapses
// concurrent calls for the same key into one execution.
//
// API matches the parts of x/sync/singleflight we need:
//
//	Do(key string, fn func() (interface{}, error)) (v interface{}, err error, shared bool)
type singleflight struct {
	mu sync.Mutex
	m  map[string]*sfCall
}

type sfCall struct {
	wg  sync.WaitGroup
	val any
	err error
}

func (g *singleflight) Do(key string, fn func() (any, error)) (any, error, bool) {
	g.mu.Lock()
	if g.m == nil {
		g.m = make(map[string]*sfCall)
	}
	if c, ok := g.m[key]; ok {
		g.mu.Unlock()
		c.wg.Wait()
		return c.val, c.err, true
	}
	c := &sfCall{}
	c.wg.Add(1)
	g.m[key] = c
	g.mu.Unlock()

	go func() {
		defer func() {
			c.wg.Done()
		}()
		c.val, c.err = fn()
		g.mu.Lock()
		delete(g.m, key)
		g.mu.Unlock()
	}()
	c.wg.Wait()
	return c.val, c.err, false
}
