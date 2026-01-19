package websockethandler

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

// host to websocket connection
func Host(port int) {
	http.HandleFunc("/ws", openConnection)

	log.Println("WebSocket server listening on port", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}

func openConnection(w http.ResponseWriter, r *http.Request) {
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
