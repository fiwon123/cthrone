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
	"sync/atomic"

	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/session"
	"github.com/fiwon123/cthrone/internal/wire"
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

// lockedWriter serializes the loop's writes.
//
// receive prints a message from the transport's goroutine while send echoes a
// keystroke from the input goroutine, and the loop prints the disconnect line
// once the peer is gone. Two writers to one terminal must not interleave half a
// line, and a writer that is not an *os.File — a test's buffer — is not safe
// for concurrent writes at all.
type lockedWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	return l.w.Write(p)
}

// run is Run with the terminal handed in, so a test needs no real tty.
func run(s session.Session, in *os.File, out io.Writer) bool {
	out = &lockedWriter{w: out}

	// What is being typed, shared with the printer: a message that arrives
	// must not swallow half a line, and two goroutines touch it.
	input := &line{}

	// The session may have closed before OnClose was installed — a peer that
	// hung up while the caller was still setting up — so close the channel
	// from whichever side gets there first.
	var once sync.Once
	done := make(chan struct{})
	finished := func() { once.Do(func() { close(done) }) }

	// The reason is kept for the line printed when a peer leaves: the callback
	// is the only place it arrives, and by the time the loop returns the close
	// has already happened.
	var closeReason atomic.Value
	s.OnClose(func(reason string) {
		closeReason.Store(reason)
		finished()
	})

	// A session that closed before the callback was installed will not report
	// it again, and waiting for a reason that already happened is a hang.
	if closed, ok := s.(interface{ IsClosed() bool }); ok && closed.IsClosed() {
		finished()
	}

	// A sealed session says when a peer authenticated and when it refused one;
	// both belong on the terminal, above the input line.
	var own sync.Once
	announceSealed(s, out, input, &own)

	// receive only installs the printer on the session, so it is done by the
	// time the sender wants to print; send blocks on the terminal and goes
	// aside.
	receive(s, input, out)

	var quit atomic.Bool
	go send(s, input, in, out, &quit)

	<-done

	// "Exiting..." already says the user quit; anything else is a peer that
	// left, and the terminal has to say so or the host looks like it is still
	// talking to someone.
	if userQuit := quit.Load(); !userQuit {
		reason, _ := closeReason.Load().(string)
		if reason == "" {
			if closed, ok := s.(interface{ Reason() string }); ok {
				reason = closed.Reason()
			}
		}

		clearLine(out)
		fmt.Fprintln(out, disconnectNotice(reason))

		return false
	}

	return true
}

// disconnectNotice is what the terminal says when the peer ended the session.
// It is not printed for a Ctrl-C: that is the user asking to leave.
func disconnectNotice(reason string) string {
	if reason == "" {
		return "Disconnected"
	}

	return "Disconnected: " + reason
}

// peerNotice is what the terminal says when a peer completes the handshake.
//
// A first contact in the device-key model carries a fingerprint to compare
// with the other screen: the pin was just recorded, and this is the only moment
// at which the user can tell the right device from a device that got there
// first. Both values are printed, because each screen shows the other's under a
// different label — "its fingerprint" here is "this device" there.
func peerNotice(p e2e.Peer, own string) string {
	if !p.First || p.Fingerprint == "" {
		return "Encrypted with " + p.Name()
	}

	notice := "Encrypted with " + p.Name() + " — new device; its fingerprint is " + p.Fingerprint
	if own != "" {
		notice += ", this device's is " + own
	}

	return notice + ". Compare both with the other screen."
}

// receive prints the messages of a session to the terminal. The session does
// the reading; this only renders.
func receive(s session.Session, input *line, out io.Writer) {
	s.OnMessage(func(m session.Message) {
		notice(out, input, senderLabel(m)+": "+messageText(m.Payload))
	})
}

// messageText is what a message says.
//
// Text is the bytes that arrived, byte for byte. The one sentence a sender has
// to wrap is the one that begins with the wire magic — the receiver would read
// it as a frame — so a text frame is unwrapped here and printed as the line it
// carries. A file frame is not text and is left as it arrived: the terminal has
// no file support, and turning a transfer into a sentence would be worse than
// the noise it already is.
func messageText(payload []byte) string {
	if !wire.IsFrame(payload) {
		return string(payload)
	}

	frame, err := wire.Parse(payload)
	if err != nil || frame.Header.Kind != wire.KindText {
		return string(payload)
	}

	return string(frame.Payload)
}

// announceSealed prints what a sealed session has to say above the input line:
// this device's fingerprint once, then the peer's notices as they arrive.
//
// own is shared by every session a console holds: a room has several peers and
// one fingerprint, and printing it once per device would bury the line that
// matters — the one the other screen has to be compared with.
func announceSealed(s session.Session, out io.Writer, input *line, own *sync.Once) {
	sealed, ok := s.(*e2e.Conn)
	if !ok {
		return
	}

	// The two fingerprints are compared at first contact, so this device's own
	// must be on screen as well: the peer's arrives with the notice, and a
	// value nobody printed cannot be compared with anything.
	own.Do(func() {
		if fingerprint := sealed.Fingerprint(); fingerprint != "" {
			notice(out, input, "This device's fingerprint is "+fingerprint+" — the other screen shows the same value for this device")
		}
	})

	sealed.OnPeer(func(p e2e.Peer) { notice(out, input, peerNotice(p, sealed.Fingerprint())) })
	sealed.OnError(func(err error) { notice(out, input, "Refused: "+err.Error()) })

	// The code a first contact is compared by arrives before the peer is
	// accepted, and on both sides: the side that is waiting has nothing else to
	// go on.
	sealed.OnFirstContact(func(c e2e.FirstContact) { notice(out, input, FirstContactNotice(c)) })
}

// senderLabel is how a message is attributed. The rule lives on the message
// itself, because a relay names the sender the same way the terminal prints it.
func senderLabel(m session.Message) string {
	return m.Label()
}

// send reads raw-mode input and sends a line when Enter is pressed.
func send(s session.Session, input *line, in *os.File, out io.Writer, quit *atomic.Bool) {
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
			quit.Store(true)
			s.Close("goodbye")

			return

		case enter:
			msg := input.take()

			// The one line that cannot go out as it was typed is the one that
			// begins with the wire magic: the peer would read it as a frame,
			// and a damaged frame closes the session. Encode wraps it in a text
			// frame, which the peer unwraps, so the sentence arrives whole.
			payload, err := wire.Encode(wire.Header{Kind: wire.KindText}, []byte(msg))
			if err != nil {
				input.put(msg)
				notice(out, input, "Not sent: "+err.Error())

				break
			}

			if err := s.Send(payload); err != nil {
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
