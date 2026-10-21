package websockethandler

import (
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/gorilla/websocket"
)

// dial connects to the host. It waits a moment first: the host is often
// started right before the client connects to it. It is a variable so a
// test can replace it — the connection is the one step that needs a host to
// talk to, and a test that cannot reach it cannot check what is done with
// the error it returns.
var dial = func(url string, header http.Header) (*websocket.Conn, *http.Response, error) {
	time.Sleep(1 * time.Second)

	return websocket.DefaultDialer.Dial(url, header)
}

// Connect to websocket host
func Connect(address string, port int, token string) error {
	url := buildURL(address, port)

	header := http.Header{}
	if token != "" {
		header.Set(auth.HeaderName, token)
	}

	conn, resp, err := dial(url, header)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf("the host refused the connection: it needs the shared secret")
		}

		return err
	}
	defer conn.Close()

	fmt.Println("Connected to", url)

	currentInput := []rune{}
	go sendMsgLoop(conn, &currentInput)
	go receiveMsgLoop(conn, &currentInput)

	select {}
}

// buildURL returns the URL of the host. An address that carries a port is
// used as it is, so that the port is not appended twice.
func buildURL(address string, port int) string {
	if _, _, err := net.SplitHostPort(address); err == nil {
		return fmt.Sprintf("ws://%s/ws", address)
	}

	return fmt.Sprintf("ws://%s:%d/ws", address, port)
}
