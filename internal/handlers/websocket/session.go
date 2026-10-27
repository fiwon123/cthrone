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
}

// newConnSession wraps conn and starts reading from it. Incoming messages
// reach the OnMessage callback; a read failure closes the session and reports
// the reason to OnClose.
func newConnSession(conn *websocket.Conn) *connSession {
	remote := conn.RemoteAddr().String()
	s := &connSession{
		Base: session.NewBase(remote, remote),
		conn: conn,
	}
	go s.readLoop()

	return s
}

func (s *connSession) readLoop() {
	defer s.conn.Close()

	for {
		_, payload, err := s.conn.ReadMessage()
		if err != nil {
			s.NotifyClose(err.Error())

			return
		}

		s.NotifyMessage(session.Message{
			From:    s.RemoteAddr(),
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
