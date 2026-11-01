package bluetooth

import (
	"sync"
	"time"

	"github.com/fiwon123/cthrone/internal/session"
)

// Link is one BLE pipe: packets out, packets in, and a way to end it.
//
// It is the seam between the radio and the session, and it is deliberately
// small: packets in, packets out, an MTU and a reason. Everything above it —
// fragmentation, reassembly, the session interface, the end-to-end layer — is
// portable code that a test can drive without a radio, and everything below it
// is the platform's GATT stack.
type Link interface {
	// MTU is the largest packet Write accepts.
	MTU() int

	// Write sends one packet — at most MTU() bytes — to the peer.
	Write(packet []byte) error

	// Incoming is the packets the peer sent. It is closed when the link ends,
	// and Reason then says why.
	Incoming() <-chan []byte

	// Reason is why the link ended. It is meaningful once Incoming is closed
	// and drained.
	Reason() string

	// Close ends the link.
	Close(reason string) error
}

// Session adapts a Link to session.Session.
//
// It fragments what Send is given, reassembles what arrives, and reports a
// broken packet stream as the session's end: a link that lost a packet cannot
// be repaired, because every byte after the gap belongs to a message the
// receiver will never complete.
type Session struct {
	*session.Base

	link Link

	// sendMu serializes whole messages. Two sends interleaved packet by packet
	// would produce two messages neither end can reassemble, so the lock is
	// held across the fragments of one message — the same rule the sealed
	// layer applies to its own frames (#192).
	sendMu sync.Mutex
	nextID uint16

	closeOnce sync.Once
}

// New wraps link as a session and starts reading from it.
//
// Incoming messages reach OnMessage; a link that ends closes the session with
// the link's reason.
func New(link Link) *Session {
	s := &Session{
		Base: session.NewBase(linkName(link), linkName(link)),
		link: link,
	}

	go s.readLoop()

	return s
}

// linkName is what a device in the room is called at the transport level: the
// name a BLE device advertises, or "bluetooth" when the link has none. The
// handshake replaces it with the authenticated device name as soon as the peer
// proves its key, so this is only what the first line of a session shows.
func linkName(link Link) string {
	if named, ok := link.(interface{ Name() string }); ok {
		if name := named.Name(); name != "" {
			return name
		}
	}

	return "bluetooth"
}

// readLoop turns packets into messages until the link ends or desynchronises.
func (s *Session) readLoop() {
	var r reassembler

	for packet := range s.link.Incoming() {
		message, err := r.add(packet)
		if err != nil {
			s.Close(err.Error())

			return
		}

		if message == nil {
			continue
		}

		s.NotifyMessage(session.Message{
			From:    s.RemoteAddr(),
			Addr:    s.RemoteAddr(),
			Payload: message,
			Time:    time.Now(),
		})
	}

	// The link ended. A Close of our own already reported a reason, and Base
	// keeps the first one.
	s.NotifyClose(s.link.Reason())
}

// Send writes payload to the peer as one fragmented message.
func (s *Session) Send(payload []byte) error {
	if s.IsClosed() {
		return session.ErrClosed
	}

	s.sendMu.Lock()
	defer s.sendMu.Unlock()

	packets, err := fragment(payload, s.link.MTU(), s.nextID)
	if err != nil {
		return err
	}

	s.nextID++

	for _, packet := range packets {
		if err := s.link.Write(packet); err != nil {
			return err
		}
	}

	return nil
}

// Close ends the link and reports why. It is safe to call more than once: Base
// keeps the first reason.
func (s *Session) Close(reason string) {
	s.closeOnce.Do(func() {
		_ = s.link.Close(reason)
		s.NotifyClose(reason)
	})
}
