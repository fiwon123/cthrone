package ws

import (
	"fmt"
	"log"
	"net/http"

	"github.com/fiwon123/cthrone/internal/handlers/chat"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{}

func StartServer(port int) {
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Println("Upgrade error:", err)
			return
		}
		defer conn.Close()

		go chat.ReceiveMsgLoop(conn)

		select {}
	})

	log.Println("WebSocket server listening on port", port)
	if err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil); err != nil {
		log.Fatal("ListenAndServe:", err)
	}
}
