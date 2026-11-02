//go:build gui

package ui

import (
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/session"
	"github.com/fiwon123/cthrone/internal/wire"
)

// recorder is a session with its own name, which is what a room keys a device
// by: two recorders built by newSendRecorder share the name "alice", and the hub
// would hold one of them.
func recorder(t *testing.T, name string) *sendRecorder {
	t.Helper()

	return &sendRecorder{
		Base: session.NewBase(name, name+":5555"),
		done: make(chan struct{}, 1),
	}
}

// sentCount is how many payloads this recorder was handed.
func (r *sendRecorder) sentCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()

	return len(r.sent)
}

// last is the most recent payload, or nil.
func (r *sendRecorder) last() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.sent) == 0 {
		return nil
	}

	return r.sent[len(r.sent)-1]
}

// A host is a room: its own message has to reach every device in it, not the one
// session the window happens to hold — which is the last device that joined
// (#320).
func TestAHostsMessageReachesEveryDeviceInTheRoom(t *testing.T) {
	g := newScreenGUI(t, false)

	hub := session.NewHub()
	laptop := recorder(t, "laptop")
	phone := recorder(t, "phone")

	hub.Register(laptop)
	hub.Register(phone)

	g.mu.Lock()
	g.hub = hub
	// The window also holds the session of the device that joined last, which is
	// exactly what the old code sent through.
	g.sess = phone
	g.mu.Unlock()

	g.send("hello room")

	laptop.wait(t)
	phone.wait(t)

	for name, recorder := range map[string]*sendRecorder{"laptop": laptop, "phone": phone} {
		if got := recorder.sentCount(); got != 1 {
			t.Errorf("%s got %d message(s), want 1", name, got)
		}

		if payload := recorder.last(); !strings.Contains(string(payload), "hello room") {
			t.Errorf("%s got %q, want the message text", name, payload)
		}
	}
}

// A client has one session, and that is where its messages go.
func TestAClientSendsToItsOneSession(t *testing.T) {
	g := newScreenGUI(t, false)

	recorder := newSendRecorder()

	g.mu.Lock()
	g.sess = recorder
	g.mu.Unlock()

	g.send("hello peer")
	recorder.wait(t)

	if got := recorder.sentCount(); got != 1 {
		t.Errorf("the peer got %d message(s), want 1", got)
	}
}

// A host with nobody in the room says so, and does not swallow the message: the
// row is not added and the field is not cleared, so the user still has the text.
func TestAHostWithAnEmptyRoomSaysSoAndKeepsTheText(t *testing.T) {
	g := newScreenGUI(t, false)

	g.mu.Lock()
	g.hub = session.NewHub()
	g.mu.Unlock()

	g.entry.SetText("wait for me")
	g.send("wait for me")

	if got := len(g.messages); got != 0 {
		t.Errorf("the transcript holds %d row(s), want none", got)
	}

	if !strings.Contains(g.status.Text(), "nobody has joined") {
		t.Errorf("the status line = %q, want it to say the room is empty", g.status.Text())
	}

	if got := g.entry.Text; got != "wait for me" {
		t.Errorf("the composer was cleared to %q; the message was not sent", got)
	}
}

// The file path uses the same sender: a host's file must reach the room too
// (#320). This checks the choice rather than a whole transfer — the chunk
// arithmetic has its own tests.
func TestAHostSendsFilesToTheRoom(t *testing.T) {
	g := newScreenGUI(t, false)

	hub := session.NewHub()
	hub.Register(newSendRecorder())

	g.mu.Lock()
	g.hub = hub
	g.mu.Unlock()

	sender, err := g.outgoing()
	if err != nil {
		t.Fatalf("outgoing() = %v, want the room", err)
	}

	if err := sender.Send([]byte("a chunk")); err != nil {
		t.Fatalf("the room refused a chunk: %v", err)
	}

	// A client still sends through its session.
	peer := newSendRecorder()
	g.mu.Lock()
	g.hub = nil
	g.sess = peer
	g.mu.Unlock()

	clientSender, err := g.outgoing()
	if err != nil {
		t.Fatalf("outgoing() for a client = %v", err)
	}

	if err := clientSender.Send([]byte("a chunk")); err != nil {
		t.Fatalf("the client refused a chunk: %v", err)
	}

	peer.wait(t)

	// The chunk is the payload the transfer code hands over; nothing wraps it.
	if payload := peer.last(); string(payload) != "a chunk" {
		t.Errorf("the client was handed %q", payload)
	}
}

// A text message a host sends is a frame or plain text, and the room must get
// exactly the bytes the sender produced — no re-encoding on the way.
func TestTheRoomGetsThePayloadUnchanged(t *testing.T) {
	g := newScreenGUI(t, false)

	hub := session.NewHub()
	first := newSendRecorder()
	hub.Register(first)

	g.mu.Lock()
	g.hub = hub
	g.mu.Unlock()

	sender, err := g.outgoing()
	if err != nil {
		t.Fatal(err)
	}

	payload, err := wire.Encode(wire.Header{Kind: wire.KindText}, []byte("wrapped"))
	if err != nil {
		t.Fatal(err)
	}

	if err := sender.Send(payload); err != nil {
		t.Fatal(err)
	}

	first.wait(t)

	if got := first.last(); string(got) != string(payload) {
		t.Errorf("the room got %q, want the %q that was sent", got, payload)
	}
}
