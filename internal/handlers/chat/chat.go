package chat

import (
	"bufio"
	"fmt"
	"os"
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

	go receiveMsgLoop(conn)
	go sendMsgLoop(conn)
}

func receiveMsgLoop(conn *websocket.Conn) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		fmt.Println("\nReceived:", string(msg))
		fmt.Print("You: ")
	}
}

func sendMsgLoop(conn *websocket.Conn) {
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("You: ")
		text, _ := reader.ReadString('\n')
		if err := conn.WriteMessage(websocket.TextMessage, []byte(text)); err != nil {
			return
		}
	}
}
