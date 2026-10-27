package websockethandler

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/fiwon123/cthrone/internal/session"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

// Server is a running websocket host. Unlike Host, which blocks until the
// process stops, OpenServer returns a handle a GUI can start and stop.
type Server struct {
	httpSrv   *http.Server
	hub       *session.Hub
	addr      string
	onSession func(session.Session)
	done      chan struct{}
	serveErr  error
}

// OpenServer starts serving the chat endpoint and returns immediately.
//
// The listener is bound before it returns, so Addr reports the port that is
// actually open — a "hosting" status that named a port the server never bound
// would be a lie. onSession is called for every client that is let in.
func OpenServer(port int, bindAddress, token string, allowAnonymous bool, certFile, keyFile string, onSession func(session.Session)) (*Server, error) {
	mux := http.NewServeMux()

	s := &Server{
		hub:       session.NewHub(),
		onSession: onSession,
		done:      make(chan struct{}),
	}

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		s.handle(w, r, token, allowAnonymous)
	})

	listener, err := net.Listen("tcp", listenAddress(bindAddress, port))
	if err != nil {
		return nil, err
	}

	if certFile != "" {
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			_ = listener.Close()

			return nil, err
		}

		listener = tls.NewListener(listener, &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		})
	}

	s.addr = listener.Addr().String()
	s.httpSrv = &http.Server{Handler: mux}

	go func() {
		defer close(s.done)

		s.serveErr = s.httpSrv.Serve(listener)
	}()

	return s, nil
}

// Addr returns the address the server listens on.
func (s *Server) Addr() string { return s.addr }

// Hub returns the sessions currently connected to this host.
func (s *Server) Hub() *session.Hub { return s.hub }

// Wait blocks until the server stops, and reports why it stopped. A clean
// Stop is not an error.
func (s *Server) Wait() error {
	<-s.done

	if s.serveErr == nil || s.serveErr == http.ErrServerClosed {
		return nil
	}

	return s.serveErr
}

// Stop closes the listener and every connected session.
func (s *Server) Stop() error {
	err := s.httpSrv.Close()
	<-s.done

	return err
}

// handle upgrades the request when the client is allowed in and hands the
// session to the caller.
func (s *Server) handle(w http.ResponseWriter, r *http.Request, token string, allowAnonymous bool) {
	if !allowed(w, r, token, allowAnonymous) {
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)

		return
	}

	sess := newConnSession(conn)
	s.hub.Register(sess)
	sess.OnClose(func(string) { s.hub.Unregister(sess.ID()) })

	if s.onSession != nil {
		s.onSession(sess)
	}
}

// Host serves the chat endpoint with the terminal as its consumer.
//
// The bind address is empty by default, which means every interface: the tool
// is a chat between two devices on the same network, and a host that only
// listens on the loopback cannot be reached by the other device.
func Host(port int, bindAddress, token string, allowAnonymous bool, certFile, keyFile string) error {
	server, err := OpenServer(port, bindAddress, token, allowAnonymous, certFile, keyFile, runTerminalSession)
	if err != nil {
		return err
	}

	log.Println("WebSocket server listening on", server.Addr(), "-", describeBind(bindAddress))

	if allowAnonymous {
		log.Println("WARNING: anonymous connections are allowed, anyone who can reach this port can join the chat")
	}

	return server.Wait()
}

// runTerminalSession bridges a session to the terminal.
func runTerminalSession(s session.Session) {
	currentInput := []rune{}
	receiveMsgLoop(s, &currentInput)
	go sendMsgLoop(s, &currentInput)
}

// listenAddress returns the address the server listens on. An empty bind
// address means every interface, which is the default: the tool is a chat
// between two devices on the same network.
func listenAddress(bindAddress string, port int) string {
	return fmt.Sprintf("%s:%d", bindAddress, port)
}

func describeBind(bindAddress string) string {
	if bindAddress == "" {
		return "every interface — reachable from the local network"
	}
	if bindAddress == "127.0.0.1" || bindAddress == "localhost" {
		return "loopback only — this machine alone"
	}
	return bindAddress + " — a specific interface"
}

// allowed checks the shared secret. A host that allows anonymous connections
// lets every client in.
func allowed(w http.ResponseWriter, r *http.Request, token string, allowAnonymous bool) bool {
	if allowAnonymous {
		return true
	}

	if !auth.Equal(r.Header.Get(auth.HeaderName), token) {
		http.Error(w, "unauthorized: the shared secret is missing or wrong", http.StatusUnauthorized)

		return false
	}

	return true
}
