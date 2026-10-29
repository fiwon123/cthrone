// Package chat is the terminal side of a session: it prints what arrives and
// sends what is typed, whatever carries the bytes.
//
// It exists because both transports need the same loop. The websocket path had
// it inside its handler and the NATS path did not have it at all, which is how
// "both transports meet at the same chat loop" stayed a claim rather than a
// fact. Above a websocket, above a broker or above the end-to-end wrapper, it is
// the same loop: it reads a session and a terminal and knows nothing else.
package chat

import (
	"fmt"
	"io"
	"os"
	"sync"

	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/session"
	"golang.org/x/term"
)

const (
	ctrlC     = 3
	enter     = 13
	backspace = 127
)

// Run prints the session's messages and sends what is typed until the session
// closes or the user presses Ctrl-C. It blocks, so a caller with nothing else
// to do calls it directly and a host calls it in a goroutine.
func Run(s session.Session) {
	run(s, os.Stdin, os.Stdout)
}

// run is Run with the terminal handed in, so a test needs no real tty.
func run(s session.Session, in *os.File, out io.Writer) {
	// What is being typed, shared with the printer: a message that arrives
	// must not swallow half a line, and two goroutines touch it.
	input := &line{}

	// The session may have closed before OnClose was installed — a peer that
	// hung up while the caller was still setting up — so close the channel
	// from whichever side gets there first.
	var once sync.Once
	done := make(chan struct{})
	finished := func() { once.Do(func() { close(done) }) }

	s.OnClose(func(string) { finished() })

	// A session that closed before the callback was installed will not report
	// it again, and waiting for a reason that already happened is a hang.
	if closed, ok := s.(interface{ IsClosed() bool }); ok && closed.IsClosed() {
		finished()
	}

	// A sealed session says when a peer authenticated and when it refused one;
	// both belong on the terminal, above the input line.
	if sealed, ok := s.(*e2e.Conn); ok {
		sealed.OnPeer(func(p e2e.Peer) { notice(out, input, "Encrypted with "+p.Name()) })
		sealed.OnError(func(err error) { notice(out, input, "Refused: "+err.Error()) })
	}

	// receive only installs the printer on the session, so it is done by the
	// time the sender wants to print; send blocks on the terminal and goes
	// aside.
	receive(s, input, out)
	go send(s, input, in, out)

	<-done
}

// receive prints the messages of a session to the terminal. The session does
// the reading; this only renders.
func receive(s session.Session, input *line, out io.Writer) {
	s.OnMessage(func(m session.Message) {
		notice(out, input, m.From+": "+string(m.Payload))
	})
}

// send reads raw-mode input and sends a line when Enter is pressed.
func send(s session.Session, input *line, in *os.File, out io.Writer) {
	if oldState, err := term.MakeRaw(int(in.Fd())); err == nil {
		defer term.Restore(int(in.Fd()), oldState)
	}

	buf := make([]byte, 1)
	printCursor(out)

	for {
		n, err := in.Read(buf)
		if n == 0 || err != nil {
			return
		}

		switch buf[0] {
		case ctrlC:
			fmt.Fprintf(out, "\r\nExiting...\n")
			s.Close("goodbye")

			return

		case enter:
			msg := input.take()

			if err := s.Send([]byte(msg)); err != nil {
				// The handshake may not have finished yet, and a send that
				// fails must not take the terminal with it: keep the text so
				// the user can press Enter again, and say why nothing went.
				input.put(msg)
				notice(out, input, "Not sent: "+err.Error())

				break
			}

			fmt.Fprintf(out, "\rYou: %s\n", msg)
			printCursor(out)

		case backspace:
			if input.backspace() {
				removeCharacterFromScreen(out)
			}

		default:
			input.append(rune(buf[0]))
			fmt.Fprintf(out, "%c", buf[0])
		}
	}
}

// notice prints one line above the input prompt and then puts the prompt, and
// whatever was being typed, back.
func notice(out io.Writer, input *line, text string) {
	clearLine(out)
	fmt.Fprintln(out, text)
	printCursor(out)
	fmt.Fprint(out, input.get())
}

// line is the text being typed. It is written by the reader and read by the
// printer, so every access holds the lock.
type line struct {
	mu   sync.Mutex
	text []rune
}

func (l *line) get() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return string(l.text)
}

// take returns what was typed and starts a new line.
func (l *line) take() string {
	l.mu.Lock()
	defer l.mu.Unlock()

	out := string(l.text)
	l.text = nil

	return out
}

// put restores text that was taken but not sent, so a failed send does not
// lose what was typed.
func (l *line) put(text string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.text = []rune(text)
}

func (l *line) append(r rune) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.text = append(l.text, r)
}

// backspace drops the last rune and reports whether there was one.
func (l *line) backspace() bool {
	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.text) == 0 {
		return false
	}

	l.text = l.text[:len(l.text)-1]

	return true
}

func printCursor(out io.Writer) {
	fmt.Fprint(out, "\r> ")
}

func removeCharacterFromScreen(out io.Writer) {
	fmt.Fprint(out, "\b \b")
}

func clearLine(out io.Writer) {
	fmt.Fprint(out, "\r\033[2K")
}
