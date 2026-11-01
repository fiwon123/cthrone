package bluetooth

import (
	"bytes"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/session"
)

// fakeLink is one end of an in-memory BLE pipe: what the session writes lands
// in written, and what the test feeds to incoming arrives as a packet. It is
// the whole of the radio as far as the session is concerned, which is what
// makes a transport testable without one.
type fakeLink struct {
	name string
	mtu  int

	mu      sync.Mutex
	written [][]byte
	fail    error
	reason  string
	closed  bool

	incoming chan []byte
}

func newFakeLink(name string, mtu int) *fakeLink {
	return &fakeLink{name: name, mtu: mtu, incoming: make(chan []byte, 64)}
}

func (l *fakeLink) Name() string            { return l.name }
func (l *fakeLink) MTU() int                { return l.mtu }
func (l *fakeLink) Incoming() <-chan []byte { return l.incoming }
func (l *fakeLink) Reason() string          { l.mu.Lock(); defer l.mu.Unlock(); return l.reason }
func (l *fakeLink) ReasonWasSet() bool      { l.mu.Lock(); defer l.mu.Unlock(); return l.closed }
func (l *fakeLink) packets() [][]byte {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([][]byte(nil), l.written...)
}

func (l *fakeLink) Write(packet []byte) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.fail != nil {
		return l.fail
	}

	l.written = append(l.written, append([]byte(nil), packet...))

	return nil
}

func (l *fakeLink) Close(reason string) error {
	l.mu.Lock()
	l.reason = reason
	l.closed = true
	l.mu.Unlock()

	close(l.incoming)

	return nil
}

// end closes the incoming side the way a radio link does: the channel closes
// and the reason says why.
func (l *fakeLink) end(reason string) {
	l.mu.Lock()
	l.reason = reason
	l.closed = true
	l.mu.Unlock()

	close(l.incoming)
}

// deliver pushes a packet as the peer would.
func (l *fakeLink) deliver(packet []byte) { l.incoming <- packet }

// waitFor waits until cond is true, or fails the test.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()

	for i := 0; i < 400; i++ {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s", what)
}

// A message reaches the session as one message, however many packets it took,
// and what the session sends comes back out as packets that reassemble to it.
func TestSessionCarriesAMessageBothWays(t *testing.T) {
	link := newFakeLink("alice", 20)

	s := New(link)
	t.Cleanup(func() { s.Close("done") })

	received := make(chan session.Message, 1)
	s.OnMessage(func(m session.Message) { received <- m })

	text := "the network is down, so this is how we talk"

	packets, err := fragment([]byte(text), link.MTU(), 1)
	if err != nil {
		t.Fatalf("fragment() error = %v", err)
	}

	for _, packet := range packets {
		link.deliver(packet)
	}

	select {
	case got := <-received:
		if string(got.Payload) != text {
			t.Fatalf("received %q, want %q", got.Payload, text)
		}

		if got.From != "alice" {
			t.Errorf("From = %q, want the link's name", got.From)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the message never arrived")
	}

	if err := s.Send([]byte("and this is the answer")); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	sent := link.packets()
	if len(sent) < 2 {
		t.Fatalf("the answer went out as %d packets, want several", len(sent))
	}

	var r reassembler

	var message []byte

	for _, packet := range sent {
		if len(packet) > link.MTU() {
			t.Fatalf("a packet is %d bytes, the MTU is %d", len(packet), link.MTU())
		}

		got, err := r.add(packet)
		if err != nil {
			t.Fatalf("add() error = %v", err)
		}

		if got != nil {
			message = got
		}
	}

	if string(message) != "and this is the answer" {
		t.Fatalf("the packets reassemble to %q", message)
	}
}

// A link that ends closes the session with the link's reason, so the terminal
// says why rather than waiting for a message that will never come.
func TestSessionClosesWithTheLink(t *testing.T) {
	link := newFakeLink("alice", 20)
	s := New(link)

	closed := make(chan string, 1)
	s.OnClose(func(reason string) { closed <- reason })

	link.end("the device went out of range")

	select {
	case reason := <-closed:
		if reason != "the device went out of range" {
			t.Fatalf("close reason = %q", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the session did not close with the link")
	}

	if !s.IsClosed() {
		t.Error("the session is not marked closed")
	}
}

// A packet stream that lost its place ends the session: the bytes after a gap
// belong to a message nobody can complete, and carrying on would deliver
// something the peer never sent.
func TestSessionClosesOnADesynchronisedStream(t *testing.T) {
	link := newFakeLink("alice", 20)
	s := New(link)

	closed := make(chan string, 1)
	s.OnClose(func(reason string) { closed <- reason })

	packets, err := fragment(bytes.Repeat([]byte("m"), 100), link.MTU(), 1)
	if err != nil {
		t.Fatalf("fragment() error = %v", err)
	}

	link.deliver(packets[0])
	link.deliver(packets[2])

	select {
	case reason := <-closed:
		if reason == "" {
			t.Fatal("the session closed without a reason")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a gap did not end the session")
	}
}

// A write that fails is reported to the caller and closes nothing by itself:
// the caller is the one that decides whether the session is over.
func TestSessionSendReportsAFailedWrite(t *testing.T) {
	link := newFakeLink("alice", 20)
	link.fail = errors.New("the characteristic refused the write")

	s := New(link)
	t.Cleanup(func() { s.Close("done") })

	if err := s.Send([]byte("hello")); err == nil {
		t.Fatal("Send() succeeded on a failing link")
	}
}

// Send refuses to write to a session that is already closed, so a caller with a
// stale session gets an error instead of a write into nothing.
func TestSessionSendRefusesAClosedSession(t *testing.T) {
	link := newFakeLink("alice", 20)
	s := New(link)
	s.Close("bye")

	if err := s.Send([]byte("hello")); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("Send() error = %v, want ErrClosed", err)
	}
}

// Two sends at once must not interleave their packets: the receiver would
// reassemble one message out of two and lose both.
func TestSessionSerializesConcurrentSends(t *testing.T) {
	link := newFakeLink("alice", 20)
	s := New(link)
	t.Cleanup(func() { s.Close("done") })

	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)

		go func(n int) {
			defer wg.Done()

			if err := s.Send(bytes.Repeat([]byte{byte('a' + n)}, 200)); err != nil {
				t.Errorf("Send() error = %v", err)
			}
		}(i)
	}

	wg.Wait()

	var r reassembler

	messages := 0

	for _, packet := range link.packets() {
		message, err := r.add(packet)
		if err != nil {
			t.Fatalf("the packets of two sends interleaved: %v", err)
		}

		if message != nil {
			messages++

			if len(message) != 200 {
				t.Fatalf("a message of %d bytes, want 200", len(message))
			}
		}
	}

	if messages != 8 {
		t.Fatalf("reassembled %d messages, want 8", messages)
	}
}

// Close is safe to call twice: the first reason stands and the link is closed
// once.
func TestSessionCloseIsIdempotent(t *testing.T) {
	link := newFakeLink("alice", 20)
	s := New(link)

	s.Close("first")
	s.Close("second")

	if reason := s.Reason(); reason != "first" {
		t.Fatalf("reason = %q, want the first one", reason)
	}
}
