package e2e

import (
	"sync"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/session"
)

// pipeSession is an in-memory session between ends that are in the same
// process: what one end sends arrives at every end linked to it, the way a
// websocket arrives at its peer and a NATS subject arrives at every
// subscriber.
//
// It buffers what arrives before a handler is installed, exactly as a socket
// does, so a test can start one side before the other and still see the frames
// that crossed.
type pipeSession struct {
	*session.Base

	mu      sync.Mutex
	links   []*pipeSession
	handler func(session.Message)
	inbox   []session.Message
	sent    [][]byte
	drop    func([]byte) bool
}

// newPipe makes one unlinked end.
func newPipe(id string) *pipeSession {
	return &pipeSession{Base: session.NewBase(id, id+".example:8080")}
}

// link joins two ends, so each sends to the other.
func link(a, b *pipeSession) {
	for _, pair := range [][2]*pipeSession{{a, b}, {b, a}} {
		pair[0].mu.Lock()
		pair[0].links = append(pair[0].links, pair[1])
		pair[0].mu.Unlock()
	}
}

func (p *pipeSession) Send(payload []byte) error {
	if p.IsClosed() {
		return session.ErrClosed
	}

	frame := append([]byte(nil), payload...)

	p.mu.Lock()
	p.sent = append(p.sent, frame)
	drop := p.drop
	links := append([]*pipeSession(nil), p.links...)
	p.mu.Unlock()

	if drop != nil && drop(frame) {
		return nil
	}

	for _, end := range links {
		// The address travels with the frame, the way a websocket reports it:
		// it is where the frame came from, which is this end, not the end it
		// is handed to.
		end.DeliverMessage(session.Message{
			From:    "peer",
			Addr:    p.RemoteAddr(),
			Payload: frame,
			Time:    time.Now(),
		})
	}

	return nil
}

// Deliver hands payload to whatever handler is installed, or holds it until one
// is, which is also how a test injects a frame the sender never produced.
func (p *pipeSession) Deliver(payload []byte) {
	p.DeliverMessage(session.Message{
		From:    "peer",
		Addr:    p.RemoteAddr(),
		Payload: append([]byte(nil), payload...),
		Time:    time.Now(),
	})
}

// DeliverMessage is Deliver with the message's fields left to the caller, so a
// test can say where a frame came from as well as what it carries.
func (p *pipeSession) DeliverMessage(message session.Message) {
	message.Payload = append([]byte(nil), message.Payload...)

	p.mu.Lock()
	handler := p.handler

	if handler == nil {
		p.inbox = append(p.inbox, message)
		p.mu.Unlock()

		return
	}

	p.mu.Unlock()
	handler(message)
}

func (p *pipeSession) OnMessage(fn func(session.Message)) {
	p.mu.Lock()
	p.handler = fn
	pending := p.inbox
	p.inbox = nil
	p.mu.Unlock()

	for _, message := range pending {
		fn(message)
	}
}

func (p *pipeSession) Close(reason string) {
	p.NotifyClose(reason)
}

// outgoing is everything this end sent, oldest first.
func (p *pipeSession) outgoing() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()

	return append([][]byte(nil), p.sent...)
}

// last is the frame this end sent most recently.
func (p *pipeSession) last(t *testing.T) []byte {
	t.Helper()

	out := p.outgoing()
	if len(out) == 0 {
		t.Fatalf("%s sent nothing", p.ID())
	}

	return out[len(out)-1]
}

// watch records what a Conn delivers, how it ended and what it refused.
type watch struct {
	mu     sync.Mutex
	msgs   [][]byte
	errs   []error
	reason string
	done   chan struct{}
	once   sync.Once
}

// observe hooks a Conn before a peer can make it do anything, so a refusal
// during the handshake is not missed.
func observe(c *Conn) *watch {
	w := &watch{done: make(chan struct{})}

	c.OnMessage(func(m session.Message) {
		w.mu.Lock()
		w.msgs = append(w.msgs, append([]byte(nil), m.Payload...))
		w.mu.Unlock()
	})

	c.OnClose(func(reason string) {
		w.mu.Lock()
		w.reason = reason
		w.mu.Unlock()
		w.once.Do(func() { close(w.done) })
	})

	c.OnError(func(err error) {
		w.mu.Lock()
		w.errs = append(w.errs, err)
		w.mu.Unlock()
	})

	return w
}

// received is the payloads that reached the session's consumer.
func (w *watch) received() [][]byte {
	w.mu.Lock()
	defer w.mu.Unlock()

	return append([][]byte(nil), w.msgs...)
}

// refused is the errors the session reported without closing.
func (w *watch) refused() []error {
	w.mu.Lock()
	defer w.mu.Unlock()

	return append([]error(nil), w.errs...)
}

// closeReason is why the session closed, or the empty string while it has not.
func (w *watch) closeReason() string {
	w.mu.Lock()
	defer w.mu.Unlock()

	return w.reason
}

// waitClosed waits for the session to close and fails the test if it does not.
func (w *watch) waitClosed(t *testing.T, within time.Duration) string {
	t.Helper()

	select {
	case <-w.done:
		return w.closeReason()
	case <-time.After(within):
		t.Fatalf("the session did not close within %s", within)

		return ""
	}
}
