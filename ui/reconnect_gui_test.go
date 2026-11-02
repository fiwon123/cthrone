//go:build gui

package ui

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	websocketcore "github.com/fiwon123/cthrone/internal/core/websocket"
	"github.com/fiwon123/cthrone/internal/data/app"
	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/pairing"
	"github.com/fiwon123/cthrone/internal/session"
)

// These tests drive a dropped client and the window's answer to it (#326). No
// network is opened: the websocket dial is a variable, so a test hands the
// window a session it made and closes it, and what is being checked is that the
// window asks for the same link again, says so, and stops when it should.

// reconnectSoon runs the retry schedule in milliseconds. A test that waited a
// real second per attempt would be slow, not more convincing.
func reconnectSoon(t *testing.T) {
	t.Helper()

	original := reconnectBase
	reconnectBase = 2 * time.Millisecond
	t.Cleanup(func() { reconnectBase = original })
}

// dialStub is the websocket dial. It counts attempts and records the host each
// one asked for, so "asked again" and "not asked again" are assertions rather
// than a race with a timer.
type dialStub struct {
	mu    sync.Mutex
	dials int
	hosts []string
	next  func(attempt int) (session.Session, error)
}

func (d *dialStub) dial(args []string, _ *app.Data, _ pairing.Options, _ auth.TLS) (session.Session, error) {
	d.mu.Lock()
	d.dials++

	host := ""
	if len(args) > 0 {
		host = args[0]
	}

	attempt := d.dials
	d.hosts = append(d.hosts, host)
	next := d.next
	d.mu.Unlock()

	return next(attempt)
}

func (d *dialStub) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.dials
}

func (d *dialStub) host(attempt int) string {
	d.mu.Lock()
	defer d.mu.Unlock()

	if attempt < 1 || attempt > len(d.hosts) {
		return ""
	}

	return d.hosts[attempt-1]
}

func installDial(t *testing.T, stub *dialStub) {
	t.Helper()

	original := openWebsocketSession
	t.Cleanup(func() { openWebsocketSession = original })

	openWebsocketSession = stub.dial
}

// refusalSession is a session whose close was a refusal, the way a sealed
// session reports one when the peer would not accept it.
type refusalSession struct {
	*sendRecorder
	err error
}

func (r *refusalSession) Refusal() error { return r.err }

func waitForReconnecting(t *testing.T, g *gui, want bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		g.mu.Lock()
		got := g.reconnecting
		g.mu.Unlock()

		if got == want {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("reconnecting never became %v", want)
}

func waitForCondition(t *testing.T, what string, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("%s never happened", what)
}

// The wait doubles from the base and stops growing at a minute.
func TestReconnectDelayDoublesUpToAMinute(t *testing.T) {
	original := reconnectBase
	reconnectBase = time.Second
	t.Cleanup(func() { reconnectBase = original })

	want := []time.Duration{
		time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		time.Minute,
		time.Minute,
	}

	for i, expected := range want {
		if got := reconnectDelay(i + 1); got != expected {
			t.Errorf("attempt %d waits %s, want %s", i+1, got, expected)
		}
	}
}

// A client whose link goes away without a Stop is asked for again, with the
// host the session began with rather than whatever the form holds now.
func TestADroppedClientReconnectsOnItsOwn(t *testing.T) {
	reconnectSoon(t)
	g := newTestGUI(t)

	first, second := newSendRecorder(), newSendRecorder()
	stub := &dialStub{next: func(attempt int) (session.Session, error) {
		if attempt == 1 {
			return first, nil
		}

		return second, nil
	}}
	installDial(t, stub)

	g.hostEntry.SetText("192.168.1.20")
	g.connect()
	waitForSession(t, g, first)

	// The form is edited after the session began: a retry replays the link,
	// not the entry.
	g.hostEntry.SetText("10.0.0.9")

	first.Close("websocket: close 1006 (abnormal closure)")

	waitForSession(t, g, second)

	if got := stub.count(); got != 2 {
		t.Fatalf("%d dials, want 2: the dropped session was not asked for again", got)
	}

	if got := stub.host(2); got != "192.168.1.20" {
		t.Errorf("the retry dialled %q, want the host the session began with", got)
	}

	waitForCondition(t, "the window to say it reconnected", func() bool {
		return strings.Contains(g.status.Text(), "Reconnected")
	})

	waitForCondition(t, "the header to show the restored session", func() bool {
		return strings.Contains(g.status.Text(), "connected")
	})
}

// Stop is the way out of a retry loop: it cancels the wait at once, and no
// attempt is made after it.
func TestStopEndsTheReconnectLoop(t *testing.T) {
	original := reconnectBase
	reconnectBase = time.Hour
	t.Cleanup(func() { reconnectBase = original })

	g := newTestGUI(t)

	first := newSendRecorder()
	stub := &dialStub{next: func(int) (session.Session, error) { return first, nil }}
	installDial(t, stub)

	g.hostEntry.SetText("192.168.1.20")
	g.connect()
	waitForSession(t, g, first)

	first.Close("EOF")
	waitForReconnecting(t, g, true)

	waitForCondition(t, "the retry to say it is waiting", func() bool {
		return strings.Contains(g.status.Text(), "reconnecting in")
	})

	waitForCondition(t, "the header to say reconnecting", func() bool {
		return strings.Contains(g.status.Text(), "reconnecting")
	})

	waitForCondition(t, "Stop to be offered while retrying", func() bool {
		return !g.stopBtn.Disabled()
	})

	g.stop()
	waitForReconnecting(t, g, false)

	if got := stub.count(); got != 1 {
		t.Errorf("%d dials, want 1: Stop let the retry dial again", got)
	}
}

// A refusal is an answer, not a drop: a session that ends because the peer said
// no is not asked for again.
func TestARefusedSessionIsNotRetried(t *testing.T) {
	reconnectSoon(t)
	g := newTestGUI(t)

	sess := &refusalSession{sendRecorder: newSendRecorder(), err: e2e.ErrConfirm}
	stub := &dialStub{next: func(int) (session.Session, error) { return sess, nil }}
	installDial(t, stub)

	g.hostEntry.SetText("192.168.1.20")
	g.connect()
	waitForSession(t, g, sess)

	sess.Close("e2e: first contact not confirmed")

	// Long enough for several attempts at the test schedule.
	time.Sleep(50 * time.Millisecond)

	if got := stub.count(); got != 1 {
		t.Errorf("%d dials, want 1: a refused peer was asked again", got)
	}

	waitForReconnecting(t, g, false)

	if got := g.status.Text(); !strings.Contains(got, "Disconnected") {
		t.Errorf("the status reads %q, want the close named", got)
	}
}

// A retry that meets a host refusal stops too: dialling again would ask the same
// question and get the same answer.
func TestARefusedRetryStopsTheQuestion(t *testing.T) {
	reconnectSoon(t)
	g := newTestGUI(t)

	first := newSendRecorder()
	stub := &dialStub{next: func(attempt int) (session.Session, error) {
		if attempt == 1 {
			return first, nil
		}

		return nil, fmt.Errorf("%w: it needs the shared secret", websocketcore.ErrRefused)
	}}
	installDial(t, stub)

	g.hostEntry.SetText("192.168.1.20")
	g.connect()
	waitForSession(t, g, first)

	first.Close("EOF")

	waitForCondition(t, "the refusal to be shown", func() bool {
		return strings.Contains(g.status.Text(), "refused")
	})

	// Long enough for several attempts at the test schedule.
	time.Sleep(50 * time.Millisecond)

	if got := stub.count(); got != 2 {
		t.Errorf("%d dials, want 2: a refused retry kept asking", got)
	}

	waitForReconnecting(t, g, false)
}

// A host is what others reconnect to: its own session ending leaves nothing to
// dial.
func TestAHostDoesNotReconnect(t *testing.T) {
	reconnectSoon(t)
	g := newTestGUI(t)

	stub := &dialStub{next: func(int) (session.Session, error) { return newSendRecorder(), nil }}
	installDial(t, stub)

	// The session a host's callback attaches: there is no link behind it.
	sess := newSendRecorder()

	g.mu.Lock()
	g.server = stopServer{}
	g.mu.Unlock()

	g.attach(sess)
	sess.Close("closed from the app")

	time.Sleep(30 * time.Millisecond)

	if got := stub.count(); got != 0 {
		t.Errorf("a host dialled %d times after its session closed", got)
	}

	waitForReconnecting(t, g, false)
}

// A Connect pressed while a retry waits ends the old loop and dials the new
// address: two loops must not race to become the live session.
func TestANewConnectEndsTheOldRetry(t *testing.T) {
	original := reconnectBase
	reconnectBase = time.Hour
	t.Cleanup(func() { reconnectBase = original })

	g := newTestGUI(t)

	first, second := newSendRecorder(), newSendRecorder()
	stub := &dialStub{next: func(attempt int) (session.Session, error) {
		if attempt == 1 {
			return first, nil
		}

		return second, nil
	}}
	installDial(t, stub)

	g.hostEntry.SetText("192.168.1.20")
	g.connect()
	waitForSession(t, g, first)

	first.Close("EOF")
	waitForReconnecting(t, g, true)

	waitForCondition(t, "the retry to say it is waiting", func() bool {
		return strings.Contains(g.status.Text(), "reconnecting in")
	})

	g.hostEntry.SetText("192.168.1.21")
	g.connect()
	waitForSession(t, g, second)
	waitForReconnecting(t, g, false)

	if got := stub.host(2); got != "192.168.1.21" {
		t.Errorf("the new Connect dialled %q, want the address it was given", got)
	}
}
