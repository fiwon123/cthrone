package websockethandler

import (
	"fmt"
	"log"
	"net/http"

	"github.com/fiwon123/cthrone/internal/auth"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

// Host serves the chat endpoint. The bind address is empty by default,
// which means every interface: the tool is a chat between two devices on
// the same network, and a host that only listens on the loopback cannot be
// reached by the other device.
//
// With TLS the server serves https, and the websocket runs over it.
func Host(port int, bindAddress, token string, allowAnonymous bool, certFile, keyFile string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		openConnection(w, r, token, allowAnonymous)
	})

	address := listenAddress(bindAddress, port)

	log.Println("WebSocket server listening on", address)

	if allowAnonymous {
		log.Println("WARNING: anonymous connections are allowed, anyone who can reach this port can join the chat")
	}

	if certFile != "" {
		return http.ListenAndServeTLS(address, certFile, keyFile, mux)
	}

	return http.ListenAndServe(address, mux)
}

// listenAddress returns the address the server listens on. An empty bind
// address means every interface, which is the default: the tool is a chat
// between two devices on the same network.
func listenAddress(bindAddress string, port int) string {
	return fmt.Sprintf("%s:%d", bindAddress, port)
}

// openConnection upgrades the request when the client is allowed in.
func openConnection(w http.ResponseWriter, r *http.Request, token string, allowAnonymous bool) {
	if !allowed(w, r, token, allowAnonymous) {
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	defer conn.Close()

	currentInput := []rune{}
	go receiveMsgLoop(conn, &currentInput)
	go sendMsgLoop(conn, &currentInput)

	select {}
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
