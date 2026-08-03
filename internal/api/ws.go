// M2-8: WebSocket realtime push.
//
// Minimal RFC 6455 implementation — text frames only, no fragmentation, no
// compression. Sufficient for server-to-client push of small JSON events.
// Ponytail: avoid adding gorilla/websocket for ~50 lines of code.
//
// Browser-side: `new WebSocket('ws://host/api/v1/ws')`.

package api

import (
	"bufio"
	"crypto/sha1"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
)

const wsGUID = "258EAFA5-E914-47DA-95CA-C5AB0DC85B11"

// Event is the JSON shape pushed to dashboards.
type Event struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

// Hub is a tiny in-process pub/sub.
type Hub struct {
	mu      sync.RWMutex
	clients map[*wsConn]chan Event
}

func NewHub() *Hub { return &Hub{clients: map[*wsConn]chan Event{}} }

func (h *Hub) Publish(ev Event) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, ch := range h.clients {
		select {
		case ch <- ev:
		default:
		}
	}
}

func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

// wsConn is a server-side WebSocket connection over a hijacked TCP conn.
type wsConn struct {
	br  *bufio.Reader
	bw  *bufio.Writer
	mu  sync.Mutex
	cls chan struct{}
}

// wsHub extracts the hub from deps or returns nil.
func wsHub(deps *Deps) *Hub {
	if deps == nil {
		return nil
	}
	if deps.Hub == nil {
		deps.Hub = NewHub()
	}
	return deps.Hub
}

// wsHandler upgrades to WS and pumps events to the client.
func wsHandler(deps *Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hub := wsHub(deps)
		if hub == nil {
			http.Error(w, "ws hub unavailable", http.StatusServiceUnavailable)
			return
		}
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "expected websocket upgrade", http.StatusBadRequest)
			return
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "hijacker not supported", http.StatusInternalServerError)
			return
		}
		conn, rw, err := hijacker.Hijack()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		defer conn.Close()

		key := r.Header.Get("Sec-WebSocket-Key")
		if key == "" {
			return
		}
		accept := wsAcceptKey(key)
		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n" +
			"Sec-WebSocket-Accept: " + accept + "\r\n\r\n"
		if _, err := rw.WriteString(resp); err != nil {
			return
		}
		if err := rw.Flush(); err != nil {
			return
		}

		wsc := &wsConn{br: bufio.NewReader(rw), bw: bufio.NewWriter(rw), cls: make(chan struct{})}
		ch := make(chan Event, 16)
		hub.mu.Lock()
		hub.clients[wsc] = ch
		hub.mu.Unlock()
		defer func() {
			hub.mu.Lock()
			delete(hub.clients, wsc)
			hub.mu.Unlock()
			close(ch)
			_ = conn.Close()
		}()
		go func() {
			for {
				if _, _, err := wsc.readFrame(); err != nil {
					close(wsc.cls)
					return
				}
			}
		}()
		for {
			select {
			case <-wsc.cls:
				return
			case ev := <-ch:
				b, err := json.Marshal(ev)
				if err != nil {
					continue
				}
				if err := wsc.writeText(b); err != nil {
					return
				}
			}
		}
	}
}

// wsAcceptKey computes the Sec-WebSocket-Accept value per RFC 6455 §4.2.2.
func wsAcceptKey(clientKey string) string {
	h := sha1.New()
	h.Write([]byte(clientKey + wsGUID))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// readFrame reads one frame and returns opcode + payload. We ignore control
// frames other than close.
func (c *wsConn) readFrame() (byte, []byte, error) {
	var hdr [2]byte
	if _, err := io.ReadFull(c.br, hdr[:]); err != nil {
		return 0, nil, err
	}
	opcode := hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	length := int(hdr[1] & 0x7F)
	switch length {
	case 126:
		var ext [2]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint16(ext[:]))
	case 127:
		var ext [8]byte
		if _, err := io.ReadFull(c.br, ext[:]); err != nil {
			return 0, nil, err
		}
		length = int(binary.BigEndian.Uint64(ext[:]))
	}
	var mask [4]byte
	if masked {
		if _, err := io.ReadFull(c.br, mask[:]); err != nil {
			return 0, nil, err
		}
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(c.br, payload); err != nil {
		return 0, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= mask[i%4]
		}
	}
	return opcode, payload, nil
}

// writeText writes a single text frame (server→client, no mask).
func (c *wsConn) writeText(b []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	var hdr [2]byte
	hdr[0] = 0x81 // FIN + text
	if len(b) < 126 {
		hdr[1] = byte(len(b))
	} else if len(b) < 65536 {
		hdr[1] = 126
	} else {
		hdr[1] = 127
	}
	if _, err := c.bw.Write(hdr[:]); err != nil {
		return err
	}
	if len(b) >= 126 && len(b) < 65536 {
		if err := binary.Write(c.bw, binary.BigEndian, uint16(len(b))); err != nil {
			return err
		}
	} else if len(b) >= 65536 {
		if err := binary.Write(c.bw, binary.BigEndian, uint64(len(b))); err != nil {
			return err
		}
	}
	if _, err := c.bw.Write(b); err != nil {
		return err
	}
	return c.bw.Flush()
}

// ErrClientClosed is returned when a client disconnects mid-read.
var ErrClientClosed = errors.New("ws client closed")