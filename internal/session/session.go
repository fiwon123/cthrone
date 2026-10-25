package session

import (
	"errors"
	"io"
	"sync"
	"time"
)

// Message represents a chat message exchanged through a session.
type Message struct {
	From    string
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

// Base provides common functionality for session implementations.
type Base struct {
	mu        sync.RWMutex
	onMessage func(Message)
	onClose   func(string)
	closed    bool
	reason    string
	id        string
	remote    string
}

func NewBase(id, remote string) *Base {
	return &Base{id: id, remote: remote}
}

func (b *Base) ID() string         { return b.id }
func (b *Base) RemoteAddr() string { return b.remote }

func (b *Base) OnMessage(fn func(Message)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onMessage = fn
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
	b.mu.RLock()
	fn := b.onMessage
	b.mu.RUnlock()
	if fn != nil {
		fn(m)
	}
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
