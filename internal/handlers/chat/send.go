package chat

import (
	"bufio"
	"fmt"
	"os"

	"github.com/gorilla/websocket"
)

func SendMsgLoop(conn *websocket.Conn) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("You: ")
		text, _ := reader.ReadString('\n')
		if err := conn.WriteMessage(websocket.TextMessage, []byte(text)); err != nil {
			return
		}
	}
}
