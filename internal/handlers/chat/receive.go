package chat

import (
	"fmt"

	"github.com/gorilla/websocket"
)

func ReceiveMsgLoop(conn *websocket.Conn) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("Read error:", err)
			break
		}
		fmt.Printf("%s: %s", conn.RemoteAddr(), string(msg))
	}
}
