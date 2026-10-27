package websockethandler

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/gorilla/websocket"
)

// dial connects to the host. It waits a moment first: the host is often
// started right before the client connects to it. It is a variable so a
// test can replace it — the connection is the one step that needs a host to
// talk to, and a test that cannot reach it cannot check what is done with
// the error it returns.
var dial = func(url string, header http.Header, tlsConfig *tls.Config) (*websocket.Conn, *http.Response, error) {
	time.Sleep(1 * time.Second)

	return dialerFor(tlsConfig).Dial(url, header)
}

// dialerFor returns the dialer used to reach the host. The TLS
// configuration is the one that checks the certificate of the host, so it
// has to reach the dialer: a dialer without it would use the system roots,
// and a self-signed certificate is in none of them.
func dialerFor(tlsConfig *tls.Config) *websocket.Dialer {
	dialer := *websocket.DefaultDialer
	if tlsConfig != nil {
		dialer.TLSClientConfig = tlsConfig
	}

	return &dialer
}

// Connect to websocket host, then run the terminal chat loop. It returns when
// the peer disconnects or the user presses Ctrl-C.
func Connect(address string, port int, token string, useTLS bool, tlsConfig *tls.Config) error {
	conn, url, err := openConnectionDial(address, port, token, useTLS, tlsConfig)
	if err != nil {
		return err
	}

	fmt.Println("Connected to", url)

	s := newConnSession(conn)
	defer s.Close("goodbye")

	// The read loop may already have failed before OnClose was registered, so
	// close the channel from whichever side gets there first.
	var once sync.Once
	done := make(chan struct{})
	finished := func() { once.Do(func() { close(done) }) }

	s.OnClose(func(string) { finished() })
	if s.IsClosed() {
		finished()
	}

	currentInput := []rune{}
	go receiveMsgLoop(s, &currentInput)
	go sendMsgLoop(s, &currentInput)

	<-done

	return nil
}

// openConnectionDial dials the host and returns the connection with the URL
// it reached. The errors that name a missing secret or a missing --tls are
// produced here so every caller explains a refusal the same way.
func openConnectionDial(address string, port int, token string, useTLS bool, tlsConfig *tls.Config) (*websocket.Conn, string, error) {
	url := buildURL(address, port, useTLS)

	header := http.Header{}
	if token != "" {
		header.Set(auth.HeaderName, token)
	}

	conn, resp, err := dial(url, header, tlsConfig)
	if err != nil {
		if resp != nil {
			switch resp.StatusCode {
			case http.StatusUnauthorized:
				return nil, url, fmt.Errorf("the host refused the connection: it needs the shared secret")
			case http.StatusBadRequest:
				return nil, url, fmt.Errorf("the host refused the connection: it is serving TLS, add --tls")
			}
		}

		return nil, url, err
	}

	return conn, url, nil
}

// buildURL returns the URL of the host. An address that carries a port is
// used as it is, so that the port is not appended twice.
func buildURL(address string, port int, useTLS bool) string {
	scheme := "ws"
	if useTLS {
		scheme = "wss"
	}

	if _, _, err := net.SplitHostPort(address); err == nil {
		return fmt.Sprintf("%s://%s/ws", scheme, address)
	}

	return fmt.Sprintf("%s://%s:%d/ws", scheme, address, port)
}
