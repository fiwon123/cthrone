package chat

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/session"
)

// fakeSession is a session that records what was sent to it. The loop only
// needs the interface, so the test needs no socket and no broker.
type fakeSession struct {
	*session.Base

	mu   sync.Mutex
	sent [][]byte
}

func newFake(id string) *fakeSession {
	return &fakeSession{Base: session.NewBase(id, id+":8080")}
}

func (f *fakeSession) Send(payload []byte) error {
	if f.IsClosed() {
		return session.ErrClosed
	}

	f.mu.Lock()
	f.sent = append(f.sent, append([]byte(nil), payload...))
	f.mu.Unlock()

	return nil
}

func (f *fakeSession) Close(reason string) { f.NotifyClose(reason) }

func (f *fakeSession) outgoing() [][]byte {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([][]byte(nil), f.sent...)
}

// pipeTerminal gives a test the two ends of an input pipe and a buffer for the
// output. The write end is the test's, the read end is the loop's.
func pipeTerminal(t *testing.T) (*os.File, *os.File, *bytes.Buffer) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})

	return r, w, &bytes.Buffer{}
}

// A message that arrives is printed with the name of its sender, which is what
// tells two peers apart in one terminal.
func TestReceivePrintsTheSender(t *testing.T) {
	out := &bytes.Buffer{}
	s := newFake("alice")

	receive(s, &line{}, out)

	s.NotifyMessage(session.Message{From: "bob", Payload: []byte("hello")})

	if got := out.String(); !strings.Contains(got, "bob: hello") {
		t.Errorf("expected the sender and the message, got %q", got)
	}
}

// An arriving message is put above the input line and the line comes back
// intact: a message must not swallow what is being typed.
func TestNoticeKeepsTheInputLine(t *testing.T) {
	out := &bytes.Buffer{}
	input := &line{}

	input.append('h')
	input.append('i')

	notice(out, input, "bob: hello")

	if got := out.String(); !strings.HasSuffix(got, "> hi") {
		t.Errorf("expected the prompt and the typed text, got %q", got)
	}
}

// Enter sends what was typed and starts a new line.
func TestSendSendsAWholeLine(t *testing.T) {
	in, w, out := pipeTerminal(t)
	s := newFake("alice")
	input := &line{}

	done := make(chan struct{})
	go func() {
		send(s, input, in, out, &atomic.Bool{})
		close(done)
	}()

	fmt.Fprint(w, "hi\r")
	_ = w.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("send did not return when the input ended")
	}

	sent := s.outgoing()
	if len(sent) != 1 || string(sent[0]) != "hi" {
		t.Fatalf("sent %q, want one line", sent)
	}

	if got := input.get(); got != "" {
		t.Errorf("the line was not started again, got %q", got)
	}
}

// Backspace removes the last rune instead of sending it.
func TestSendHandlesBackspace(t *testing.T) {
	in, w, out := pipeTerminal(t)
	s := newFake("alice")
	input := &line{}

	done := make(chan struct{})
	go func() {
		send(s, input, in, out, &atomic.Bool{})
		close(done)
	}()

	fmt.Fprint(w, "ab\x7f\r")
	_ = w.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("send did not return when the input ended")
	}

	sent := s.outgoing()
	if len(sent) != 1 || string(sent[0]) != "a" {
		t.Fatalf("sent %q, want %q", sent, "a")
	}
}

// Ctrl-C closes the session and ends the loop.
func TestSendClosesTheSessionOnCtrlC(t *testing.T) {
	in, w, out := pipeTerminal(t)
	s := newFake("alice")

	done := make(chan struct{})
	go func() {
		send(s, &line{}, in, out, &atomic.Bool{})
		close(done)
	}()

	fmt.Fprint(w, "\x03")
	_ = w.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("send did not exit on ctrl-c")
	}

	if !s.IsClosed() {
		t.Error("expected ctrl-c to close the session")
	}
}

// Run returns when the session closes: a peer that hung up must not leave the
// loop waiting on a terminal nobody is typing at.
func TestRunReturnsWhenTheSessionCloses(t *testing.T) {
	in, _, out := pipeTerminal(t)
	s := newFake("alice")

	done := make(chan struct{})
	go func() {
		run(s, in, out)
		close(done)
	}()

	select {
	case <-done:
		t.Fatal("run returned before the session closed")
	case <-time.After(50 * time.Millisecond):
	}

	s.Close("peer said goodbye")

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after the session closed")
	}
}

// A send that fails — the handshake has not finished, so the wrapper refuses to
// put anything in the clear — must not swallow the line or end the input loop.
// The text stays so Enter can be pressed again once the peer is verified.
func TestSendKeepsTheLineWhenTheSessionRefuses(t *testing.T) {
	in, w, out := pipeTerminal(t)
	f := &flakySession{fakeSession: newFake("alice"), fails: 1, attempted: make(chan struct{}, 1)}

	done := make(chan struct{})
	go func() {
		send(f, &line{}, in, out, &atomic.Bool{})
		close(done)
	}()

	fmt.Fprint(w, "hi\r")

	select {
	case <-f.attempted:
	case <-time.After(2 * time.Second):
		t.Fatal("the first send was not attempted")
	}

	// The text is still there, so a second Enter sends it now that it is allowed.
	fmt.Fprint(w, "\r")
	_ = w.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("send did not return when the input ended")
	}

	sent := f.outgoing()
	if len(sent) != 1 || string(sent[0]) != "hi" {
		t.Fatalf("sent %q, want the line sent once after the refusal", sent)
	}

	if got := out.String(); !strings.Contains(got, "Not sent:") {
		t.Errorf("expected the refusal on the terminal, got %q", got)
	}
}

// errNotYet stands in for the wrapper's refusal before the handshake finishes.
var errNotYet = errors.New("e2e: handshake refused")

// flakySession fails a number of sends before behaving, so a test can hold the
// session in the state a handshake creates.
type flakySession struct {
	*fakeSession

	mu        sync.Mutex
	fails     int
	attempted chan struct{}
}

func (f *flakySession) Send(payload []byte) error {
	f.mu.Lock()
	fails := f.fails
	f.fails--
	f.mu.Unlock()

	if fails > 0 {
		f.attempted <- struct{}{}

		return errNotYet
	}

	return f.fakeSession.Send(payload)
}

// The line is written by the reader and read by the printer, so it has to be
// safe from both: this is the case that used to race.
func TestLineIsSafeFromBothSides(t *testing.T) {
	input := &line{}
	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()

		for i := 0; i < 1000; i++ {
			input.append('x')
		}
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < 1000; i++ {
			_ = input.get()
		}
	}()

	wg.Wait()

	if got := len(input.get()); got != 1000 {
		t.Errorf("line holds %d runes, want 1000", got)
	}
}

// A first contact in the device-key model says which fingerprint to compare:
// the pin was recorded just now, and after this notice the two devices are
// trusted on the strength of that comparison.
func TestPeerNoticeNamesBothFingerprintsOnFirstContact(t *testing.T) {
	first := peerNotice(e2e.Peer{Device: "bob", Fingerprint: "aaaa-bbbb-cccc-dddd", First: true}, "1111-2222-3333-4444")
	for _, want := range []string{"bob", "aaaa-bbbb-cccc-dddd", "1111-2222-3333-4444"} {
		if !strings.Contains(first, want) {
			t.Fatalf("first-contact notice = %q, want %q in it", first, want)
		}
	}

	again := peerNotice(e2e.Peer{Device: "bob", Fingerprint: "aaaa-bbbb-cccc-dddd"}, "1111-2222-3333-4444")
	if strings.Contains(again, "aaaa-bbbb-cccc-dddd") {
		t.Fatalf("a returning peer's notice repeats the fingerprint: %q", again)
	}

	secret := peerNotice(e2e.Peer{Device: "bob"}, "")
	if secret != "Encrypted with bob" {
		t.Fatalf("a shared-secret peer's notice = %q", secret)
	}
}

// Ctrl-C is the user ending the loop, and a host has to be able to tell that
// apart from a peer that left: the first releases the listening port, the
// second leaves the host waiting for the next device.
func TestRunHostReportsAUserQuit(t *testing.T) {
	in, w, out := pipeTerminal(t)
	s := newFake("alice")

	quit := make(chan bool, 1)
	go func() { quit <- run(s, in, out) }()

	fmt.Fprint(w, "\x03")

	select {
	case got := <-quit:
		if !got {
			t.Error("RunHost did not report the Ctrl-C that ended the loop")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after ctrl-c")
	}
}

// A peer that leaves is not the user quitting: the host keeps waiting.
func TestRunHostDoesNotReportAPeerLeaving(t *testing.T) {
	in, _, out := pipeTerminal(t)
	s := newFake("alice")

	quit := make(chan bool, 1)
	go func() { quit <- run(s, in, out) }()

	s.Close("peer said goodbye")

	select {
	case got := <-quit:
		if got {
			t.Error("a peer leaving was reported as the user quitting")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not return after the session closed")
	}
}
