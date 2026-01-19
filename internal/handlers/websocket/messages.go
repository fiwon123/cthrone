package websockethandler

import (
	"fmt"
	"os"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

const (
	ctrlC     = 3
	enter     = 13
	backspace = 127
)

func receiveMsgLoop(conn *websocket.Conn, currentInput *[]rune) {
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			fmt.Println("Read error:", err)
			break
		}

		clearLine()
		fmt.Printf("%s: %s\n", conn.RemoteAddr(), string(msg))
		printCursor()
		fmt.Printf("%s", string(*currentInput))
	}
}

func sendMsgLoop(conn *websocket.Conn, currentInput *[]rune) {

	oldState, _ := term.MakeRaw(int(os.Stdin.Fd()))
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	buf := make([]byte, 1)
	printCursor()

	for {
		n, _ := os.Stdin.Read(buf)
		if n > 0 {
			b := buf[0]

			switch b {
			case ctrlC:
				return
			case enter:
				err := conn.WriteMessage(websocket.TextMessage, []byte(string(*currentInput)))
				if err != nil {
					return
				} else {
					fmt.Println("\rYou:", string(*currentInput))
					*currentInput = []rune{}
					printCursor()
				}
			case backspace:
				if len(*currentInput) > 0 {
					*currentInput = (*currentInput)[:len(*currentInput)-1]
					removeCharacterFromScreen()
				}
			default:
				*currentInput = append(*currentInput, rune(b))
				fmt.Printf("%c", b)
			}
		}

	}
}

func printCursor() {
	fmt.Print("\r> ")
}

func removeCharacterFromScreen() {
	fmt.Print("\b \b")
}

func clearLine() {
	fmt.Print("\r\033[2K")
}
