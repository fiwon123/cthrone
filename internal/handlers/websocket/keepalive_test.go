package websockethandler

import (
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/session"
)

// shrinkKeepalive makes the keepalive observable in a test: a real session
// would wait 25 seconds for a ping and 70 for an answer. The constants are the
// shipped ones; only these variables move.
func shrinkKeepalive(t *testing.T) {
	t.Helper()

	originalEvery, originalWithin := pingEvery, readWithin

	pingEvery, readWithin = 40*time.Millisecond, 300*time.Millisecond
	t.Cleanup(func() { pingEvery, readWithin = originalEvery, originalWithin })
}

// errMuted ends a proxied copy without closing the sockets underneath it.
var errMuted = errors.New("proxy muted")

// gatedConn stops carrying bytes once the proxy is muted, and returns an error
// so the copy loop gives up — but the socket itself stays open.
type gatedConn struct {
	net.Conn

	muted *atomic.Bool
}

func (c gatedConn) Read(p []byte) (int, error) {
	if c.muted.Load() {
		return 0, errMuted
	}

	return c.Conn.Read(p)
}

func (c gatedConn) Write(p []byte) (int, error) {
	if c.muted.Load() {
		return 0, errMuted
	}

	return c.Conn.Write(p)
}

// silentProxy sits between a client and a host and can stop forwarding without
// closing either end. That is the failure the keepalive exists for: a NAT or a
// firewall expiring a quiet flow, a phone whose radio slept — nothing on the
// wire says goodbye, so the transport below notices nothing.
type silentProxy struct {
	addr  string
	muted atomic.Bool
}

func newSilentProxy(t *testing.T, target string) *silentProxy {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listening for the proxy: %v", err)
	}

	t.Cleanup(func() { _ = listener.Close() })

	p := &silentProxy{addr: listener.Addr().String()}

	go func() {
		for {
			down, err := listener.Accept()
			if err != nil {
				return
			}

			go p.pipe(down, target)
		}
	}()

	return p
}

func (p *silentProxy) pipe(down net.Conn, target string) {
	up, err := net.Dial("tcp", target)
	if err != nil {
		_ = down.Close()

		return
	}

	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = down.Close()
			_ = up.Close()
		})
	}

	gated := func(c net.Conn) net.Conn { return gatedConn{Conn: c, muted: &p.muted} }

	// A copy that ends on errMuted leaves both sockets open, which is the
	// whole point: closing them would be a disconnect the transport can see.
	go func() {
		if _, err := io.Copy(gated(up), gated(down)); !errors.Is(err, errMuted) {
			closeBoth()
		}
	}()

	go func() {
		if _, err := io.Copy(gated(down), gated(up)); !errors.Is(err, errMuted) {
			closeBoth()
		}
	}()
}

// dialThroughProxy starts a host and returns a client session connected through
// the proxy, plus the host's end of the same session.
func dialThroughProxy(t *testing.T) (session.Session, session.Session, *silentProxy) {
	t.Helper()

	fastDial(t)

	hostSessions := make(chan session.Session, 1)

	server, err := OpenServer(0, "127.0.0.1", "", "", "", func(s session.Session) {
		hostSessions <- s
	})
	if err != nil {
		t.Fatalf("OpenServer: %v", err)
	}

	t.Cleanup(func() { _ = server.Stop() })

	proxy := newSilentProxy(t, server.Addr())

	host, portText, err := net.SplitHostPort(proxy.addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", proxy.addr, err)
	}

	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatalf("port %q: %v", portText, err)
	}

	client, err := Dial(host, port, "", false, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}

	t.Cleanup(func() { client.Close("test over") })

	var hostSession session.Session

	select {
	case hostSession = <-hostSessions:
	case <-time.After(2 * time.Second):
		t.Fatal("the host never handed out a session")
	}

	t.Cleanup(func() { hostSession.Close("test over") })

	return client, hostSession, proxy
}

// A link that goes silent in the middle is noticed, and both sides say so.
//
// This is the regression test for issue #193: with no ping and no read
// deadline, neither end ever learns that the NAT between them dropped the flow,
// so the session looks alive until the user types into a socket that cannot
// carry it.
func TestASilentLinkIsNoticed(t *testing.T) {
	shrinkKeepalive(t)

	client, hostSession, proxy := dialThroughProxy(t)

	// One message each way proves the session is past the handshake before the
	// link is cut, so what closes below is the keepalive noticing, not a
	// connection that never worked.
	exchange(t, client, hostSession)

	closed := make(chan string, 2)

	client.OnClose(func(reason string) { closed <- "client: " + reason })
	hostSession.OnClose(func(reason string) { closed <- "host: " + reason })

	proxy.muted.Store(true)

	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()

	for i := 0; i < 2; i++ {
		select {
		case who := <-closed:
			t.Logf("noticed the silent link: %s", who)

		case <-timer.C:
			t.Fatalf("only %d of 2 sides noticed a link that stopped carrying bytes: the session still believes it is connected", i)
		}
	}
}

// A quiet session that is still connected stays connected.
//
// The keepalive must answer and be answered: a pong has to move the read
// deadline, or the deadline would fire on a perfectly good connection — a bug
// that would drop a session for being idle, which is the report this fixes.
func TestAnIdleSessionStaysConnected(t *testing.T) {
	shrinkKeepalive(t)

	client, hostSession, _ := dialThroughProxy(t)

	exchange(t, client, hostSession)

	// Many ping intervals with nothing else to say.
	time.Sleep(6 * readWithin)

	if closed, ok := client.(interface{ IsClosed() bool }); ok && closed.IsClosed() {
		t.Fatal("the client closed an idle session that was still connected")
	}

	if closed, ok := hostSession.(interface{ IsClosed() bool }); ok && closed.IsClosed() {
		t.Fatal("the host closed an idle session that was still connected")
	}

	// And it still carries a message afterwards.
	exchange(t, client, hostSession)
}

// exchange sends one message each way and fails if either does not arrive.
func exchange(t *testing.T, client, hostSession session.Session) {
	t.Helper()

	fromClient := make(chan string, 1)
	hostSession.OnMessage(func(m session.Message) { fromClient <- string(m.Payload) })

	if err := client.Send([]byte("hello")); err != nil {
		t.Fatalf("client Send: %v", err)
	}

	select {
	case got := <-fromClient:
		if got != "hello" {
			t.Fatalf("the host received %q, want %q", got, "hello")
		}

	case <-time.After(2 * time.Second):
		t.Fatal("the host never received the message")
	}

	fromHost := make(chan string, 1)
	client.OnMessage(func(m session.Message) { fromHost <- string(m.Payload) })

	if err := hostSession.Send([]byte("hi")); err != nil {
		t.Fatalf("host Send: %v", err)
	}

	select {
	case got := <-fromHost:
		if got != "hi" {
			t.Fatalf("the client received %q, want %q", got, "hi")
		}

	case <-time.After(2 * time.Second):
		t.Fatal("the client never received the message")
	}
}
