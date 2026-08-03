package api

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"testing"
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