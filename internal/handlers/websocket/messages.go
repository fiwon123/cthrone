package websockethandler

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

var (
	stdin  *os.File  = os.Stdin
	stdout io.Writer = os.Stdout
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
			fmt.Fprintln(stdout, "Read error:", err)
			break
		}

		clearLine()
		fmt.Fprintf(stdout, "%s: %s\n", conn.RemoteAddr(), string(msg))
		printCursor()
		fmt.Fprintf(stdout, "%s", string(*currentInput))
	}
}

func sendMsgLoop(conn *websocket.Conn, currentInput *[]rune) {
	var mu sync.Mutex
	var oldState *term.State
	var err error
	oldState, err = term.MakeRaw(int(stdin.Fd()))
	if err == nil {
		defer term.Restore(int(stdin.Fd()), oldState)
	}

	buf := make([]byte, 1)
	printCursor()

	for {
		n, err := stdin.Read(buf)
		if n == 0 || err != nil {
			return
		}
		b := buf[0]

		switch b {
		case ctrlC:
			fmt.Fprintf(stdout, "\r\nExiting...\n")
			_ = conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "goodbye"))
			_ = conn.Close()
			return
		case enter:
			mu.Lock()
			msg := string(*currentInput)
			*currentInput = []rune{}
			mu.Unlock()
			err := conn.WriteMessage(websocket.TextMessage, []byte(msg))
			if err != nil {
				return
			}
			fmt.Fprintf(stdout, "\rYou: %s\n", msg)
			printCursor()
		case backspace:
			mu.Lock()
			if len(*currentInput) > 0 {
				*currentInput = (*currentInput)[:len(*currentInput)-1]
				mu.Unlock()
				removeCharacterFromScreen()
			} else {
				mu.Unlock()
			}
		default:
			mu.Lock()
			*currentInput = append(*currentInput, rune(b))
			mu.Unlock()
			fmt.Fprintf(stdout, "%c", b)
		}

	}
}

func printCursor() {
	fmt.Fprint(stdout, "\r> ")
}

func removeCharacterFromScreen() {
	fmt.Fprint(stdout, "\b \b")
}

func clearLine() {
	fmt.Fprint(stdout, "\r\033[2K")
}
