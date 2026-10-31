package chat

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/session"
)

// A room reads stdin once and sends the line to every device in it: two
// devices must never compete for the terminal.
func TestRoomSendsOneLineToEveryDevice(t *testing.T) {
	hub := session.NewHub()

	alice, bob := newFake("alice"), newFake("bob")
	hub.Register(alice)
	hub.Register(bob)

	in, w, out := pipeTerminal(t)
	room := newRoom(hub, in, out)

	go room.Run()

	waitForOutput(t, out, "> ")
	fmt.Fprint(w, "hello\r")

	waitForOutput(t, out, "You: hello")

	for _, s := range []*fakeSession{alice, bob} {
		got := s.outgoing()
		if len(got) != 1 || string(got[0]) != "hello" {
			t.Fatalf("%s received %q, want [hello]", s.ID(), got)
		}
	}
}

// What one device sends is printed once, with the sender named, and relayed to
// the other device — the two halves of a room.
func TestRoomRelaysAMessageToTheOtherDevices(t *testing.T) {
	hub := session.NewHub()

	alice, bob := newFake("alice"), newFake("bob")
	hub.Register(alice)
	hub.Register(bob)

	in, _, out := pipeTerminal(t)
	room := newRoom(hub, in, out)

	go room.Run()

	alice.NotifyMessage(session.Message{From: "alice", Addr: "192.168.0.5:1234", Payload: []byte("hello")})

	waitForOutput(t, out, "alice (192.168.0.5:1234): hello")

	got := bob.outgoing()
	if len(got) != 1 || string(got[0]) != "alice (192.168.0.5:1234): hello" {
		t.Fatalf("bob received %q, want the relayed message", got)
	}

	if sent := alice.outgoing(); len(sent) != 0 {
		t.Fatalf("the sender was sent its own message: %q", sent)
	}
}

// A room with nobody in it is not a crash: the text is kept and the console
// says why nothing went, so pressing Enter again after a device joins works.
func TestRoomSaysWhenNoDeviceIsConnected(t *testing.T) {
	hub := session.NewHub()

	in, w, out := pipeTerminal(t)
	room := newRoom(hub, in, out)

	go room.Run()

	waitForOutput(t, out, "> ")
	fmt.Fprint(w, "hello\r")

	waitForOutput(t, out, "Not sent: no device is connected")

	if !strings.Contains(out.String(), "hello") {
		t.Fatalf("the typed text was not kept: %q", out.String())
	}
}

// A device leaving does not end the room: the host keeps waiting for the next
// one, which is what hosting is.
func TestRoomKeepsRunningWhenAPeerLeaves(t *testing.T) {
	hub := session.NewHub()

	alice := newFake("alice")
	hub.Register(alice)

	in, _, out := pipeTerminal(t)
	room := newRoom(hub, in, out)

	quit := make(chan bool, 1)
	go func() { quit <- room.Run() }()

	waitForOutput(t, out, "Joined: alice:8080")

	alice.Close("peer said goodbye")

	waitForOutput(t, out, "Left: alice:8080 — peer said goodbye")

	select {
	case <-quit:
		t.Fatal("the room ended when a device left")
	case <-time.After(100 * time.Millisecond):
	}
}

// Ctrl-C is the user ending the console, and the host has to be told so the
// listening port can go back to the machine.
func TestRoomReportsCtrlC(t *testing.T) {
	hub := session.NewHub()

	in, w, out := pipeTerminal(t)
	room := newRoom(hub, in, out)

	quit := make(chan bool, 1)
	go func() { quit <- room.Run() }()

	waitForOutput(t, out, "> ")
	fmt.Fprint(w, "\x03")

	select {
	case got := <-quit:
		if !got {
			t.Error("the room did not report the Ctrl-C that ended it")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the room did not return after ctrl-c")
	}
}

// A host can run with no terminal attached, and there stdin reads as ended at
// once. That is not the user leaving: the room must keep serving instead of
// stopping the listener the moment it started.
func TestRoomKeepsServingWhenTheInputIsNotATerminal(t *testing.T) {
	hub := session.NewHub()

	in, w, out := pipeTerminal(t)

	// The input is closed before the room starts, the way /dev/null reads.
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}

	room := newRoom(hub, in, out)

	quit := make(chan bool, 1)
	go func() { quit <- room.Run() }()

	select {
	case got := <-quit:
		if got {
			t.Error("a host with no terminal reported a user quit")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the room did not return after the input ended")
	}
}
