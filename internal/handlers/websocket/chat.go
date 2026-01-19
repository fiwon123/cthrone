package websockethandler

import (
	"fmt"
	"time"

	"github.com/gorilla/websocket"
)

func Connect(url string) {
	time.Sleep(1 * time.Second)
	conn, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		return
	}
	defer conn.Close()

	fmt.Println("Connected to", url)

	go SendMsgLoop(conn)

	select {}
}
