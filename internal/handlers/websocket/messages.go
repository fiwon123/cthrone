package websockethandler

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/fiwon123/cthrone/internal/session"
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

// receiveMsgLoop prints the messages of a session to the terminal. The
// session does the reading; this only renders.
func receiveMsgLoop(s session.Session, currentInput *[]rune) {
	s.OnMessage(func(m session.Message) {
		clearLine()
		fmt.Fprintf(stdout, "%s: %s\n", m.From, string(m.Payload))
		printCursor()
		fmt.Fprintf(stdout, "%s", string(*currentInput))
	})
}

// sendMsgLoop reads raw-mode stdin and sends a line when Enter is pressed.
func sendMsgLoop(s session.Session, currentInput *[]rune) {
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
			s.Close("goodbye")

			return
		case enter:
			mu.Lock()
			msg := string(*currentInput)
			*currentInput = []rune{}
			mu.Unlock()
			if err := s.Send([]byte(msg)); err != nil {
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
