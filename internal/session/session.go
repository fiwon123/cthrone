package session

import (
	"errors"
	"io"
	"sync"
	"time"
)

// Message represents a chat message exchanged through a session.
type Message struct {
	// From is who sent it: the device name the handshake authenticated where
	// the session has one, and the transport's own sender string otherwise.
	From string

	// Addr is where the frame came from at the transport — the peer's address
	// on a websocket. It is empty when the transport has no per-peer address: a
	// NATS subject is the same for every peer, so it tells one sender from
	// another no better than nothing at all. It sits beside From rather than
	// inside it because From is a name, and the address is what distinguishes
	// two devices that happen to carry the same name.
	Addr    string
	Payload []byte
	Time    time.Time
}

// Session is the transport-agnostic interface for a chat session.
type Session interface {
	Send(payload []byte) error
	OnMessage(func(Message))
	OnClose(func(reason string))
	Close(reason string)
	ID() string
	RemoteAddr() string
}

// ErrClosed is returned when writing to a closed session.
var ErrClosed = errors.New("session closed")

// maxPending bounds the messages a session holds before a message handler is
// installed. The window is short — a transport delivers a frame the moment it
// is open, while the wrapper around it is still being installed — but the peer
// decides what it sends in that window, so the buffer has a ceiling rather than
// growing without one.
const maxPending = 256

// Base provides common functionality for session implementations.
type Base struct {
	mu        sync.RWMutex
	onMessage func(Message)
	onClose   func(string)
	closed    bool
	reason    string
	id        string
	remote    string

	// pending holds messages that arrived before a handler was installed, and
	// draining says one goroutine is delivering them. A frame can be read by
	// the transport before the layer above it has hooked OnMessage — the
	// handshake hello is exactly that frame — and dropping it would lose the
	// start of the exchange. Holding it until the handler arrives is what makes
	// that gap harmless.
	pending  []Message
	draining bool
}

func NewBase(id, remote string) *Base {
	return &Base{id: id, remote: remote}
}

func (b *Base) ID() string         { return b.id }
func (b *Base) RemoteAddr() string { return b.remote }

func (b *Base) OnMessage(fn func(Message)) {
	b.mu.Lock()
	b.onMessage = fn

	// Nothing held, or a delivery is already running (it will pick this up):
	// there is nothing for this call to do.
	if fn == nil || b.draining {
		b.mu.Unlock()

		return
	}

	b.draining = true
	b.mu.Unlock()

	b.drain()
}

// drain delivers held messages in arrival order, and anything that arrives while
// it runs, until nothing is left. It is the only place a handler is called for
// a held message, so a handler that sends on its own session cannot start a
// second delivery.
func (b *Base) drain() {
	for {
		b.mu.Lock()

		if len(b.pending) == 0 {
			b.pending = nil
			b.draining = false

			b.mu.Unlock()

			return
		}

		m := b.pending[0]
		b.pending = b.pending[1:]
		fn := b.onMessage
		b.mu.Unlock()

		if fn == nil {
			continue
		}

		fn(m)
	}
}

func (b *Base) OnClose(fn func(string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onClose = fn
}

func (b *Base) IsClosed() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.closed
}

func (b *Base) Reason() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.reason
}

func (b *Base) NotifyMessage(m Message) {
	b.mu.Lock()

	// No handler yet, or one is busy delivering: hold the message so the
	// handler sees it in order once it is ready. A handler is never called
	// here while another delivery is running.
	if b.onMessage == nil || b.draining {
		// The oldest are what is kept: the first frames of a session are its
		// handshake, and a peer that floods the window before a handler exists
		// has nothing in the overflow that is worth holding.
		if len(b.pending) < maxPending {
			b.pending = append(b.pending, m)
		}

		b.mu.Unlock()

		return
	}

	b.draining = true
	fn := b.onMessage
	b.mu.Unlock()

	fn(m)
	b.drain()
}

func (b *Base) NotifyClose(reason string) {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return
	}
	b.closed = true
	b.reason = reason
	fn := b.onClose
	b.mu.Unlock()
	if fn != nil {
		fn(reason)
	}
}

// Hub manages multiple sessions and relays messages between them.
type Hub struct {
	mu       sync.RWMutex
	sessions map[string]Session
}

func NewHub() *Hub {
	return &Hub{sessions: make(map[string]Session)}
}

func (h *Hub) Register(s Session) {
	h.mu.Lock()
	h.sessions[s.ID()] = s
	h.mu.Unlock()
}

func (h *Hub) Unregister(id string) {
	h.mu.Lock()
	delete(h.sessions, id)
	h.mu.Unlock()
}

func (h *Hub) Broadcast(fromID string, payload []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	msg := Message{From: fromID, Payload: payload, Time: time.Now()}
	for id, s := range h.sessions {
		if id == fromID {
			continue
		}
		if err := s.Send(payload); err != nil {
			continue
		}
		_ = msg
	}
}

func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

// Writer is a simple writer that sends to a session.
type Writer struct {
	s Session
}

func (w *Writer) Write(p []byte) (n int, err error) {
	if err := w.s.Send(append([]byte(nil), p...)); err != nil {
		if errors.Is(err, ErrClosed) {
			return 0, io.ErrClosedPipe
		}
		return 0, err
	}
	return len(p), nil
}
