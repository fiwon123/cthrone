package natshandler

import (
	"sync"
	"time"

	"github.com/fiwon123/cthrone/internal/session"
	"github.com/nats-io/nats.go"
)

// natsSession adapts a broker connection to session.Session. Send publishes to
// the subject; incoming messages on the same subject arrive through the
// subscription. It never touches stdin or stdout.
type natsSession struct {
	*session.Base

	pub     publisher
	subject string
	sub     subscription
	closeFn func()
	writeMu sync.Mutex

	// pending counts the payloads this session has just published. A broker
	// delivers a message to every subscription on the subject, including the
	// publisher's own, so without this counter the sender would render its
	// own message twice: once as "You:" and once as the echo.
	pendingMu sync.Mutex
	pending   map[string]int
}

// NewSession subscribes to subject on nc and returns a live session.
//
// The subscription stays active until Close: a subscription that unsubscribes
// itself right after subscribing never receives anything, which is why the
// GUI could not use the old SubscribeMessages path.
func NewSession(nc *nats.Conn, subject string) (session.Session, error) {
	return newSession(nc, subs(nc), nc.Close, subject)
}

func newSession(p publisher, s subscriber, closeFn func(), subject string) (*natsSession, error) {
	sess := &natsSession{
		Base:    session.NewBase(subject, ""),
		pub:     p,
		subject: subject,
		closeFn: closeFn,
		pending: make(map[string]int),
	}

	sub, err := s.Subscribe(subject, func(m *nats.Msg) {
		payload := append([]byte(nil), m.Data...)
		if sess.consumePending(string(payload)) {
			return
		}

		sess.NotifyMessage(session.Message{
			From: m.Subject,
			// No Addr: a subject is the same for every peer on a bus, so it
			// identifies no sender. Leaving it empty is what keeps a caller
			// from printing a subject as if it were an address.
			Payload: payload,
			Time:    time.Now(),
		})
	})
	if err != nil {
		return nil, err
	}

	sess.sub = sub

	return sess, nil
}

// Send publishes a chat message to the subject.
func (s *natsSession) Send(payload []byte) error {
	if s.IsClosed() {
		return session.ErrClosed
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	text := string(payload)
	s.addPending(text)

	if err := publishMessages(s.pub, text, s.subject); err != nil {
		s.removePending(text)

		return err
	}

	return nil
}

// addPending records a payload that is about to be published, so the echo of
// it is recognised and dropped.
func (s *natsSession) addPending(payload string) {
	s.pendingMu.Lock()
	s.pending[payload]++
	s.pendingMu.Unlock()
}

// removePending forgets a payload whose publish failed: no echo is coming.
func (s *natsSession) removePending(payload string) {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	if n := s.pending[payload]; n <= 1 {
		delete(s.pending, payload)
	} else {
		s.pending[payload] = n - 1
	}
}

// consumePending reports whether payload is the echo of a message this session
// published. It consumes one pending count per echo.
func (s *natsSession) consumePending(payload string) bool {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()

	n := s.pending[payload]
	if n == 0 {
		return false
	}

	if n == 1 {
		delete(s.pending, payload)
	} else {
		s.pending[payload] = n - 1
	}

	return true
}

// Close unsubscribes, closes the connection and reports the reason.
func (s *natsSession) Close(reason string) {
	s.writeMu.Lock()

	if s.sub != nil {
		_ = s.sub.Unsubscribe()
	}

	if s.closeFn != nil {
		s.closeFn()
	}

	s.writeMu.Unlock()

	s.NotifyClose(reason)
}
