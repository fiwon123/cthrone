package websockethandler

import (
	"crypto/tls"
	"sync"
	"time"

	"github.com/fiwon123/cthrone/internal/session"
	"github.com/gorilla/websocket"
)

// writeTimeout bounds a single write so a peer that stops reading cannot
// block the caller — a GUI button handler must not hang on a dead socket.
const writeTimeout = 10 * time.Second

// The keepalive: pingInterval is how often an idle side probes the link, and
// readTimeout is how long it waits for anything — a frame or a pong — before it
// declares the link gone.
//
// A TCP connection that is killed in the middle (a NAT or firewall idle
// timeout, a phone whose radio slept, a Wi-Fi reassociation) sends nothing, so
// without a probe the session only finds out on the next write — which for a
// quiet conversation may be minutes later, or never. The probe makes silence
// observable. readTimeout is several intervals, so a single lost ping is not a
// disconnect.
const (
	pingInterval = 25 * time.Second
	readTimeout  = 70 * time.Second
)

// The intervals are variables so a test can observe a keepalive without
// waiting a minute for it. Nothing else sets them.
var (
	pingEvery  = pingInterval
	readWithin = readTimeout
)

// connSession adapts a *websocket.Conn to session.Session.
//
// It is the seam that lets a caller other than the terminal loop (the GUI, a
// mobile app) use the websocket transport: nothing here touches stdin or
// stdout. The terminal loops in messages.go are a consumer of this interface,
// which is why the CLI and the GUI share one transport implementation.
type connSession struct {
	*session.Base

	conn    *websocket.Conn
	writeMu sync.Mutex

	// done ends the keepalive when the session does, however it ends: a read
	// failure and a Close both have to leave nothing writing to a closed conn.
	done     chan struct{}
	stopOnce sync.Once
}

// newConnSession wraps conn and starts reading from it. Incoming messages
// reach the OnMessage callback; a read failure closes the session and reports
// the reason to OnClose.
//
// It also starts the keepalive, on both sides of every connection: the host's
// accepted conn and the client's dialed one are both wrapped here, so each
// probes the other and each answers a probe.
func newConnSession(conn *websocket.Conn) *connSession {
	remote := conn.RemoteAddr().String()
	s := &connSession{
		Base: session.NewBase(remote, remote),
		conn: conn,
		done: make(chan struct{}),
	}

	// A pong is what the read deadline is waiting for, so the handler has to
	// extend it; installing one replaces gorilla's default handler.
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(readWithin))
	})

	go s.readLoop()
	go s.keepalive()

	return s
}

// keepalive pings the peer so a link that died while nobody was typing is
// noticed. It is not a chat message: nothing is delivered to OnMessage, and the
// peer answers through the transport.
func (s *connSession) keepalive() {
	ticker := time.NewTicker(pingEvery)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return

		case <-ticker.C:
			// WriteControl is safe alongside WriteMessage, but the deadline is
			// set under the lock a payload write uses so the two cannot
			// overwrite each other's.
			s.writeMu.Lock()
			err := s.conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(writeTimeout))
			s.writeMu.Unlock()

			if err != nil {
				// The transport cannot reach the peer any more. Closing the
				// connection makes the blocked read return, so the closure is
				// reported once, from the read side, with the socket's reason.
				_ = s.conn.Close()

				return
			}
		}
	}
}

// stop ends the keepalive. It is safe to call more than once because a Close
// and a read failure can both arrive.
func (s *connSession) stop() {
	s.stopOnce.Do(func() { close(s.done) })
}

func (s *connSession) readLoop() {
	defer s.conn.Close()

	for {
		// Every frame — and every pong, through the handler — moves the
		// deadline: the session is alive as long as either side is talking.
		_ = s.conn.SetReadDeadline(time.Now().Add(readWithin))

		_, payload, err := s.conn.ReadMessage()
		if err != nil {
			s.stop()
			s.NotifyClose(err.Error())

			return
		}

		s.NotifyMessage(session.Message{
			From:    s.RemoteAddr(),
			Addr:    s.RemoteAddr(),
			Payload: payload,
			Time:    time.Now(),
		})
	}
}

// Send writes a chat message to the peer.
func (s *connSession) Send(payload []byte) error {
	if s.IsClosed() {
		return session.ErrClosed
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	_ = s.conn.SetWriteDeadline(time.Now().Add(writeTimeout))

	return s.conn.WriteMessage(websocket.TextMessage, payload)
}

// Close says goodbye to the peer and closes the connection. It is safe to
// call more than once: Base.NotifyClose reports the first reason only.
func (s *connSession) Close(reason string) {
	s.stop()

	s.writeMu.Lock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(writeTimeout))
	_ = s.conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, reason))
	_ = s.conn.Close()
	s.writeMu.Unlock()

	s.NotifyClose(reason)
}

// Dial connects to a host and returns a live session.
//
// Unlike Connect it does not enter the terminal chat loop, so a GUI can own
// the session and decide when to send, stop and close it.
func Dial(address string, port int, token string, useTLS bool, tlsConfig *tls.Config) (session.Session, error) {
	conn, _, err := openConnectionDial(address, port, token, useTLS, tlsConfig)
	if err != nil {
		return nil, err
	}

	return newConnSession(conn), nil
}
