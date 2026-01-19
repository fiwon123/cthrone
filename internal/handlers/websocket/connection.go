package websockethandler

import (
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

func Connect(ip string, port int) {
	url := fmt.Sprintf("ws://%s:%d/ws", ip, port)

	time.Sleep(1 * time.Second)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	fmt.Println("Connected to", url)

	currentInput := []rune{}
	go sendMsgLoop(conn, &currentInput)
	go receiveMsgLoop(conn, &currentInput)

	select {}
}
