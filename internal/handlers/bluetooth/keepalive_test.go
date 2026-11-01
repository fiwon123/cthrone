package bluetooth

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/session"
)

// The probe runs on milliseconds here rather than the minutes it uses on a
// radio, so a test watches the mechanism instead of waiting for it.
const (
	testPing    = 15 * time.Millisecond
	testTimeout = 120 * time.Millisecond
)

// controlPacket builds the packet a peer's probe arrives as.
func controlPacket(kind byte) []byte {
	packet := make([]byte, controlLen)
	binary.BigEndian.PutUint16(packet[0:], controlID)
	binary.BigEndian.PutUint16(packet[2:], 0)
	packet[4] = kind

	return packet
}

// hasControl reports whether the link wrote a control packet of that kind.
func hasControl(packets [][]byte, kind byte) bool {
	for _, packet := range packets {
		if len(packet) == controlLen &&
			binary.BigEndian.Uint16(packet[0:]) == controlID &&
			packet[4] == kind {
			return true
		}
	}

	return false
}

// A side that has nothing to say still probes: that is what makes the peer's
// silence mean something.
func TestKeepaliveProbesAnIdleLink(t *testing.T) {
	link := newFakeLink("alice", 20)

	k := newKeepalive(link, testPing, time.Second)
	t.Cleanup(func() { k.Close("done") })

	waitFor(t, "a probe on the link", func() bool {
		return hasControl(link.packets(), controlPing)
	})
}

// A probe is answered and swallowed: it is the link's business, not the
// conversation's, so the session never sees it.
func TestKeepaliveAnswersAProbeAndHidesIt(t *testing.T) {
	link := newFakeLink("alice", 20)
	k := newKeepalive(link, time.Hour, time.Second)

	s := New(k)
	t.Cleanup(func() { s.Close("done") })

	received := make(chan session.Message, 1)
	s.OnMessage(func(m session.Message) { received <- m })

	link.deliver(controlPacket(controlPing))

	waitFor(t, "the answer to the peer's probe", func() bool {
		return hasControl(link.packets(), controlPong)
	})

	select {
	case m := <-received:
		t.Fatalf("the probe reached the session as %q", m.Payload)
	case <-time.After(50 * time.Millisecond):
	}
}

// Data passes through the probe untouched, in both directions.
func TestKeepaliveCarriesData(t *testing.T) {
	link := newFakeLink("alice", 20)
	k := newKeepalive(link, time.Hour, time.Second)

	s := New(k)
	t.Cleanup(func() { s.Close("done") })

	received := make(chan session.Message, 1)
	s.OnMessage(func(m session.Message) { received <- m })

	packets, err := fragment([]byte("still here"), link.MTU(), 1)
	if err != nil {
		t.Fatalf("fragment() error = %v", err)
	}

	for _, packet := range packets {
		link.deliver(packet)
	}

	select {
	case m := <-received:
		if string(m.Payload) != "still here" {
			t.Fatalf("received %q", m.Payload)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the message never arrived through the probe")
	}

	if err := s.Send([]byte("and back")); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	sent := link.packets()

	// The probe may have written its own packets first; the data is what the
	// reassembler can turn back into a message.
	var r reassembler

	var got []byte

	for _, packet := range sent {
		if len(packet) == controlLen && binary.BigEndian.Uint16(packet[0:]) == controlID {
			continue
		}

		message, err := r.add(packet)
		if err != nil {
			t.Fatalf("add() error = %v", err)
		}

		if message != nil {
			got = message
		}
	}

	if string(got) != "and back" {
		t.Fatalf("the link carried %q, want %q", got, "and back")
	}
}

// A link that has gone quiet is ended with a reason, so the host frees the slot
// for the next device instead of waiting forever.
func TestKeepaliveEndsAQuietLink(t *testing.T) {
	link := newFakeLink("alice", 20)
	k := newKeepalive(link, testPing, testTimeout)

	s := New(k)

	closed := make(chan string, 1)
	s.OnClose(func(reason string) { closed <- reason })

	select {
	case reason := <-closed:
		if !strings.Contains(reason, "nothing has arrived") {
			t.Fatalf("close reason = %q", reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a quiet link was never ended")
	}

	if !link.ReasonWasSet() {
		t.Error("the link underneath was not closed")
	}
}

// Traffic keeps the link open: a device that keeps talking is not timed out,
// however quiet the conversation is.
func TestKeepaliveSurvivesTraffic(t *testing.T) {
	link := newFakeLink("alice", 20)
	k := newKeepalive(link, testPing, testTimeout)

	s := New(k)
	t.Cleanup(func() { s.Close("done") })

	closed := make(chan string, 1)
	s.OnClose(func(reason string) { closed <- reason })

	deadline := time.After(testTimeout * 3)

	for {
		select {
		case reason := <-closed:
			t.Fatalf("a talking link was ended: %q", reason)
		case <-deadline:
			if s.IsClosed() {
				t.Fatal("the session closed while traffic was flowing")
			}

			return
		case <-time.After(testPing / 2):
			packets, err := fragment([]byte("tick"), link.MTU(), 1)
			if err != nil {
				t.Fatalf("fragment() error = %v", err)
			}

			for _, packet := range packets {
				link.deliver(packet)
			}
		}
	}
}

// Closing through the probe closes what is underneath, and the reason the
// session reports is the one it was given.
func TestKeepaliveCloseReachesTheLink(t *testing.T) {
	link := newFakeLink("alice", 20)
	k := newKeepalive(link, time.Hour, time.Second)

	s := New(k)

	closed := make(chan string, 1)
	s.OnClose(func(reason string) { closed <- reason })

	s.Close("the user quit")

	select {
	case reason := <-closed:
		if reason != "the user quit" {
			t.Fatalf("close reason = %q", reason)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the session did not close")
	}

	if !link.ReasonWasSet() {
		t.Error("the link underneath was not closed")
	}
}

// A probe larger than the link's packet size is not sent: there is nothing to
// write it into, and a link that small still carries the conversation.
func TestKeepaliveSkipsAProbeThatCannotFit(t *testing.T) {
	link := newFakeLink("alice", controlLen-1)
	k := newKeepalive(link, testPing, time.Second)
	t.Cleanup(func() { k.Close("done") })

	time.Sleep(testPing * 3)

	if bytes.Contains(bytes.Join(link.packets(), nil), []byte{0xFF, 0xFF}) {
		t.Fatal("a probe was written into a link too small to carry it")
	}
}
