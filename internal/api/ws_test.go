package api

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// --- Hub tests ---

func TestHub_PublishDeliversToAllClients(t *testing.T) {
	h := NewHub()
	ch1 := make(chan Event, 1)
	ch2 := make(chan Event, 1)
	h.mu.Lock()
	h.clients[&wsConn{}] = ch1
	h.clients[&wsConn{}] = ch2
	h.mu.Unlock()

	h.Publish(Event{Type: "alert.new"})

	e1 := <-ch1
	if e1.Type != "alert.new" {
		t.Errorf("ch1 did not receive event: %+v", e1)
	}
	e2 := <-ch2
	if e2.Type != "alert.new" {
		t.Errorf("ch2 did not receive event: %+v", e2)
	}
}

func TestHub_PublishDropsSlowConsumers(t *testing.T) {
	h := NewHub()
	ch := make(chan Event, 1) // capacity 1
	h.mu.Lock()
	h.clients[&wsConn{}] = ch
	h.mu.Unlock()

	// Fill the buffer then send another — should not block.
	h.Publish(Event{Type: "first"})
	h.Publish(Event{Type: "second"}) // dropped
	h.Publish(Event{Type: "third"})  // dropped

	if got := <-ch; got.Type != "first" {
		t.Fatalf("expected first, got %s", got.Type)
	}
}

func TestHub_ClientCountTracksRegistrations(t *testing.T) {
	h := NewHub()
	if h.ClientCount() != 0 {
		t.Fatalf("expected 0, got %d", h.ClientCount())
	}
	c := &wsConn{}
	ch := make(chan Event, 1)
	h.mu.Lock()
	h.clients[c] = ch
	h.mu.Unlock()
	if h.ClientCount() != 1 {
		t.Fatalf("expected 1, got %d", h.ClientCount())
	}
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	if h.ClientCount() != 0 {
		t.Fatalf("expected 0 after remove, got %d", h.ClientCount())
	}
}

// --- wsAcceptKey tests (RFC 6455 §4.2.2) ---

func TestWsAcceptKey_KnownVector(t *testing.T) {
	// RFC 6455 §1.3 example: key dGhlIHNhbXBsZSBub25jZQ== → accept s3pPLMBiTxaQ9kYGzzhZRbK+xOo=
	got := wsAcceptKey("dGhlIHNhbXBsZSBub25jZQ==")
	want := "s3pPLMBiTxaQ9kYGzzhZRbK+xOo="
	if got != want {
		t.Fatalf("wsAcceptKey mismatch:\n  got:  %s\n  want: %s", got, want)
	}
}

// --- writeText + readFrame roundtrip via bufio over a bytes.Buffer ---

func TestWriteText_SmallPayloadFraming(t *testing.T) {
	// Verify the frame header for a small payload (len < 126).
	var buf bytes.Buffer
	c := &wsConn{bw: bufio.NewWriter(&buf)}
	if err := c.writeText([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	out := buf.Bytes()
	if len(out) < 4 {
		t.Fatalf("frame too short: %x", out)
	}
	if out[0]&0x80 == 0 {
		t.Errorf("FIN bit not set: %x", out[0])
	}
	if out[0]&0x0F != 0x01 {
		t.Errorf("expected opcode 1 (text), got %x", out[0])
	}
	if int(out[1]&0x7F) != 2 {
		t.Errorf("expected length 2, got %d", out[1]&0x7F)
	}
	if string(out[2:]) != "hi" {
		t.Errorf("payload mismatch: %q", out[2:])
	}
}

func TestWriteText_MediumPayloadUses16BitLength(t *testing.T) {
	var buf bytes.Buffer
	c := &wsConn{bw: bufio.NewWriter(&buf)}
	payload := bytes.Repeat([]byte("x"), 200) // forces 16-bit length
	if err := c.writeText(payload); err != nil {
		t.Fatal(err)
	}
	out := buf.Bytes()
	if out[1]&0x7F != 126 {
		t.Fatalf("expected length code 126, got %d", out[1]&0x7F)
	}
	extLen := binary.BigEndian.Uint16(out[2:4])
	if int(extLen) != 200 {
		t.Fatalf("expected 16-bit length 200, got %d", extLen)
	}
	if !bytes.Equal(out[4:], payload) {
		t.Errorf("payload mismatch after 16-bit header")
	}
}

func TestReadFrame_UnmaskedTextPayload(t *testing.T) {
	// Build a minimal unmasked text frame: "hi"
	// header: 0x81 0x02 + "hi"
	frame := []byte{0x81, 0x02, 'h', 'i'}
	c := &wsConn{br: bufio.NewReader(bytes.NewReader(frame))}
	op, payload, err := c.readFrame()
	if err != nil {
		t.Fatal(err)
	}
	if op != 0x01 {
		t.Errorf("expected opcode 1, got %x", op)
	}
	if string(payload) != "hi" {
		t.Errorf("payload mismatch: %q", payload)
	}
}

// --- Concurrency tests (run with `go test -race`) ---
// These exercise the real race-prone paths: concurrent Publish to many
// clients while subscribers are coming and going. If any of these fail
// under -race, the data race is real and will bite in prod.

func TestHub_ConcurrentPublishAndSubscribe(t *testing.T) {
	h := NewHub()
	const N = 50
	const ITER = 200

	var wg sync.WaitGroup

	// Publishers: hammer Publish from many goroutines.
	for range 4 {
		wg.Go(func() {
			for range ITER {
				h.Publish(Event{Type: "alert.new"})
			}
		})
	}

	// Subscribers: rapidly add/remove clients.
	for range 4 {
		wg.Go(func() {
			for range ITER {
				c := &wsConn{}
				ch := make(chan Event, 4)
				h.mu.Lock()
				h.clients[c] = ch
				h.mu.Unlock()
				// Drain in background; if we miss, that's OK (drop policy).
				go func() {
					for range ch {
					}
				}()
				h.mu.Lock()
				delete(h.clients, c)
				h.mu.Unlock()
			}
		})
	}

	// Counter: track ClientCount stability (no negative, no goroutine leak).
	var maxSeen int64
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(1 * time.Millisecond):
				c := int64(h.ClientCount())
				if c < 0 {
					t.Errorf("negative ClientCount: %d", c)
				}
				atomic.StoreInt64(&maxSeen, max(atomic.LoadInt64(&maxSeen), c))
			}
		}
	}()

	wg.Wait()
	close(done)
	// Sanity: max concurrent clients never exceeded N.
	if atomic.LoadInt64(&maxSeen) > int64(N)+10 { // slack for in-flight adds
		t.Errorf("ClientCount grew unexpectedly: %d", maxSeen)
	}
}

func TestHub_PublishDoesNotBlockOnSlowConsumer(t *testing.T) {
	h := NewHub()
	// Slow consumer: channel capacity 0, never reads.
	slow := make(chan Event)
	fast := make(chan Event, 100)
	h.mu.Lock()
	h.clients[&wsConn{}] = slow
	h.clients[&wsConn{}] = fast
	h.mu.Unlock()

	// Publish many events; must not block even though slow is unread.
	done := make(chan struct{})
	go func() {
		for range 1000 {
			h.Publish(Event{Type: "flood"})
		}
		close(done)
	}()
	select {
	case <-done:
		// good
	case <-time.After(2 * time.Second):
		t.Fatal("Publish blocked on slow consumer — drop policy broken")
	}
	// Fast consumer got all events (capacity is 100, so first 100; rest dropped).
	count := 0
	for {
		select {
		case <-fast:
			count++
		default:
			goto check
		}
	}
check:
	if count == 0 {
		t.Error("fast consumer got nothing")
	}
}

func TestHub_ConcurrentClientCountNeverNegative(t *testing.T) {
	// Stress: 8 goroutines each adding 1000 clients then removing them.
	// ClientCount must always be >= 0 and eventually 0.
	h := NewHub()
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 1000 {
				c := &wsConn{}
				ch := make(chan Event, 1)
				h.mu.Lock()
				h.clients[c] = ch
				h.mu.Unlock()
				h.mu.Lock()
				delete(h.clients, c)
				h.mu.Unlock()
				if n := h.ClientCount(); n < 0 {
					t.Errorf("ClientCount went negative: %d", n)
				}
			}
		})
	}
	wg.Wait()
	if h.ClientCount() != 0 {
		t.Errorf("expected 0 after cleanup, got %d", h.ClientCount())
	}
}
