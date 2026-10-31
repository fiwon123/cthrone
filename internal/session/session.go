package session

import (
	"errors"
	"io"
	"sync"
	"time"

	"github.com/fiwon123/cthrone/internal/wire"
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

	// AddMessageHandler installs another consumer beside the one OnMessage
	// sets, and never replaces it. A host needs two: the terminal prints the
	// message, and the room relays it to the other devices. OnMessage is the
	// single consumer a caller expects to own; AddMessageHandler is for the
	// layers above it, so a relay cannot silently take the printer's place.
	AddMessageHandler(func(Message))

	OnClose(func(reason string))

	// AddCloseHandler is AddMessageHandler for a close: a host records a
	// departure in the room without displacing the consumer's own callback.
	AddCloseHandler(func(reason string))

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

	// extraMessages and extraCloses are the handlers installed beside the
	// consumer. A host's relay is one: the terminal prints a message and the
	// room forwards it, and neither may replace the other.
	extraMessages []func(Message)
	extraCloses   []func(string)

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

// messageHandlersLocked is every handler a message goes to, in the order it is
// delivered: the consumer first, then the ones added beside it. The caller
// holds the lock.
func (b *Base) messageHandlersLocked() []func(Message) {
	out := make([]func(Message), 0, 1+len(b.extraMessages))

	if b.onMessage != nil {
		out = append(out, b.onMessage)
	}

	return append(out, b.extraMessages...)
}

func (b *Base) hasHandlersLocked() bool {
	return b.onMessage != nil || len(b.extraMessages) > 0
}

// drainLocked starts a delivery if there is anything to deliver and no
// delivery is already running. It reports whether the caller must call drain.
func (b *Base) drainLocked() bool {
	if b.draining || !b.hasHandlersLocked() {
		return false
	}

	b.draining = true

	return true
}

func (b *Base) OnMessage(fn func(Message)) {
	b.mu.Lock()
	b.onMessage = fn

	if !b.drainLocked() {
		b.mu.Unlock()

		return
	}

	b.mu.Unlock()

	b.drain()
}

// AddMessageHandler installs a handler beside the consumer, and delivers
// anything that was held before it arrived. It is what lets a relay and a
// printer both see every frame: one OnMessage would have to choose.
func (b *Base) AddMessageHandler(fn func(Message)) {
	if fn == nil {
		return
	}

	b.mu.Lock()
	b.extraMessages = append(b.extraMessages, fn)

	if !b.drainLocked() {
		b.mu.Unlock()

		return
	}

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
		handlers := b.messageHandlersLocked()
		b.mu.Unlock()

		for _, fn := range handlers {
			fn(m)
		}
	}
}

func (b *Base) OnClose(fn func(string)) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.onClose = fn
}

// AddCloseHandler installs a close callback beside the consumer's, so a host
// can record a departure without taking over OnClose.
func (b *Base) AddCloseHandler(fn func(string)) {
	if fn == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.extraCloses = append(b.extraCloses, fn)
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
	if !b.hasHandlersLocked() || b.draining {
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
	handlers := b.messageHandlersLocked()
	b.mu.Unlock()

	for _, fn := range handlers {
		fn(m)
	}

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

	closes := make([]func(string), 0, 1+len(b.extraCloses))
	if b.onClose != nil {
		closes = append(closes, b.onClose)
	}

	closes = append(closes, b.extraCloses...)
	b.mu.Unlock()

	for _, fn := range closes {
		fn(reason)
	}
}

// Label is how a message is attributed: the device name, with the address it
// came from when the transport knows one.
//
// A websocket session has both — the name is what the handshake authenticated,
// the address is which machine on the network it is — and a name alone does not
// tell two devices with the same hostname apart. On a bus there is no per-peer
// address, so the name stands alone; an unnamed peer is still labelled, because
// the one thing it must never render as is "You".
func (m Message) Label() string {
	from := m.From
	if from == "" {
		from = "peer"
	}

	if m.Addr == "" || m.Addr == from {
		return from
	}

	return from + " (" + m.Addr + ")"
}

// Hub is the room: every authenticated session a host holds, with the messages
// of each relayed to all the others.
//
// It is what makes a host a room instead of a set of two-party conversations.
// A session that was never sealed never reaches Register, so a relay can only
// write to a peer that proved its identity.
type Hub struct {
	mu       sync.RWMutex
	sessions map[string]Session

	// onMessage is the consumer of every message any session delivers — the
	// terminal console or the GUI. onJoin and onLeave report membership.
	onMessage func(Message)
	onJoin    func(Session)
	onLeave   func(Session, string)
}

func NewHub() *Hub {
	return &Hub{sessions: make(map[string]Session)}
}

// OnMessage installs the consumer every session's messages are delivered to.
func (h *Hub) OnMessage(fn func(Message)) {
	h.mu.Lock()
	h.onMessage = fn
	h.mu.Unlock()
}

// OnJoin installs the membership callback and replays the sessions already in
// the room, so a caller that installs it after the listener bound does not miss
// a device that joined in between.
func (h *Hub) OnJoin(fn func(Session)) {
	h.mu.Lock()
	h.onJoin = fn
	current := make([]Session, 0, len(h.sessions))

	for _, s := range h.sessions {
		current = append(current, s)
	}
	h.mu.Unlock()

	if fn == nil {
		return
	}

	for _, s := range current {
		fn(s)
	}
}

// OnLeave installs the callback for a session that closed.
func (h *Hub) OnLeave(fn func(Session, string)) {
	h.mu.Lock()
	h.onLeave = fn
	h.mu.Unlock()
}

// Register puts a sealed session in the room and wires the two things the room
// needs from it: its messages are relayed to the others, and its close takes it
// out of the room.
//
// Both are installed beside the consumer's own callbacks, not in place of them:
// a host's terminal prints a message and the room forwards it, and neither may
// displace the other.
func (h *Hub) Register(s Session) {
	h.mu.Lock()
	h.sessions[s.ID()] = s
	join := h.onJoin
	h.mu.Unlock()

	s.AddMessageHandler(func(m Message) {
		h.relay(s.ID(), m)
		h.deliver(m)
	})

	s.AddCloseHandler(func(reason string) {
		h.Unregister(s.ID())

		h.mu.RLock()
		leave := h.onLeave
		h.mu.RUnlock()

		if leave != nil {
			leave(s, reason)
		}
	})

	if join != nil {
		join(s)
	}
}

func (h *Hub) Unregister(id string) {
	h.mu.Lock()
	delete(h.sessions, id)
	h.mu.Unlock()
}

// deliver hands a message to the consumer, if there is one.
func (h *Hub) deliver(m Message) {
	h.mu.RLock()
	fn := h.onMessage
	h.mu.RUnlock()

	if fn != nil {
		fn(m)
	}
}

// relay forwards a message to every other session in the room, named.
func (h *Hub) relay(fromID string, m Message) {
	h.Broadcast(fromID, RelayedPayload(m))
}

// Broadcast sends payload to every session except fromID, and reports how many
// took it. A closed session is skipped rather than failing the broadcast: one
// device that left must not stop the message reaching the rest.
//
// The sessions are snapshotted before anything is sent: a send can close a
// session, and a close handler unregisters it, so holding the lock across the
// sends would deadlock the room on its own traffic.
func (h *Hub) Broadcast(fromID string, payload []byte) int {
	h.mu.RLock()
	targets := make([]Session, 0, len(h.sessions))

	for id, s := range h.sessions {
		if id == fromID {
			continue
		}

		targets = append(targets, s)
	}
	h.mu.RUnlock()

	sent := 0

	for _, s := range targets {
		if closed, ok := s.(interface{ IsClosed() bool }); ok && closed.IsClosed() {
			continue
		}

		if err := s.Send(payload); err != nil {
			continue
		}

		sent++
	}

	return sent
}

func (h *Hub) Count() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.sessions)
}

// RelayedPayload is what a relay forwards for a message: the payload, named.
//
// Text is prefixed with the sender's label, because the wire carries no
// per-message metadata for an unwrapped line — the frame format only wraps
// files, and wrapping text would make an older peer refuse it. A file frame is
// re-encoded with the sender in its header instead, which is additive: an older
// peer ignores the field and still assembles the file.
func RelayedPayload(m Message) []byte {
	label := m.Label()

	if wire.IsFrame(m.Payload) {
		frame, err := wire.Parse(m.Payload)
		if err == nil {
			// A text frame is a sentence the sender had to wrap because it
			// began with the magic. Unwrapping it here keeps the rule for
			// text — the label goes in front of the sentence — and an older
			// peer reads it as ordinary text.
			if frame.Header.Kind == wire.KindText {
				return []byte(label + ": " + string(frame.Payload))
			}

			frame.Header.Sender = label

			if encoded, err := wire.Encode(frame.Header, frame.Payload); err == nil {
				return encoded
			}
		}

		// A frame that cannot be re-encoded is forwarded as it is: the receiver
		// still gets the bytes, and losing the attribution is better than
		// losing the chunk.
		return m.Payload
	}

	return []byte(label + ": " + string(m.Payload))
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
