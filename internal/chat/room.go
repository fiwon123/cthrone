package chat

import (
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"

	"github.com/fiwon123/cthrone/internal/e2e"
	"github.com/fiwon123/cthrone/internal/session"
	"golang.org/x/term"
)

// Room is the terminal side of a host: one console for every device in it.
//
// A host is a room, not a set of two-party conversations. One goroutine reads
// stdin, so two devices never compete for the terminal, and one line typed is
// sent to every device in the room. What arrives from one device is printed
// once, with the sender named, and the hub forwards it to the others.
type Room struct {
	hub   *session.Hub
	in    *os.File
	out   *lockedWriter
	input *line

	// own prints this device's fingerprint once, not once per device in the
	// room: it is one device, however many peers it holds.
	own sync.Once

	quit atomic.Bool
	done chan struct{}
	once sync.Once
}

// NewRoom builds the console for hub over the process's own terminal.
func NewRoom(hub *session.Hub) *Room {
	return newRoom(hub, os.Stdin, os.Stdout)
}

// newRoom is NewRoom with the terminal handed in, so a test needs no real tty.
func newRoom(hub *session.Hub, in *os.File, out io.Writer) *Room {
	r := &Room{
		hub:   hub,
		in:    in,
		out:   &lockedWriter{w: out},
		input: &line{},
		done:  make(chan struct{}),
	}

	// The hooks are installed here, not in Run, so a device that joins the
	// instant the listener binds is still reported: Run may not have started.
	hub.OnMessage(func(m session.Message) {
		notice(r.out, r.input, senderLabel(m)+": "+string(m.Payload))
	})

	hub.OnJoin(func(s session.Session) {
		announceSealed(s, r.out, r.input, &r.own)
		notice(r.out, r.input, "Joined: "+sessionLabel(s))
	})

	hub.OnLeave(func(s session.Session, reason string) {
		notice(r.out, r.input, leaveNotice(s, reason))
	})

	return r
}

// RunRoom is Run for a host: it reads stdin once and sends every line to every
// device in the room, and reports whether the user ended it by pressing Ctrl-C.
func RunRoom(hub *session.Hub) bool {
	return NewRoom(hub).Run()
}

// Run blocks until the user leaves or the input ends.
func (r *Room) Run() bool {
	go r.send()
	<-r.done

	return r.quit.Load()
}

func (r *Room) finish() {
	r.once.Do(func() { close(r.done) })
}

// send reads raw-mode input and sends a line to the room when Enter is pressed.
func (r *Room) send() {
	defer r.finish()

	// A host can run with no terminal attached — the first-contact gate says so
	// — and there stdin is /dev/null, which reads as ended at once. Ending the
	// host there would make it exit the moment it started, so only a terminal
	// that ends is the user leaving; without one, the room keeps serving and a
	// signal is what stops the process.
	tty := term.IsTerminal(int(r.in.Fd()))

	if oldState, err := term.MakeRaw(int(r.in.Fd())); err == nil {
		defer term.Restore(int(r.in.Fd()), oldState)
	}

	buf := make([]byte, 1)
	printCursor(r.out)

	for {
		n, err := r.in.Read(buf)
		if n == 0 || err != nil {
			if tty {
				r.quit.Store(true)
			}

			return
		}

		switch buf[0] {
		case ctrlC:
			fmt.Fprintf(r.out, "\r\nExiting...\n")
			r.quit.Store(true)

			return

		case enter:
			r.say()

		case backspace:
			if r.input.backspace() {
				removeCharacterFromScreen(r.out)
			}

		default:
			r.input.append(rune(buf[0]))
			fmt.Fprintf(r.out, "%c", buf[0])
		}
	}
}

// say sends what was typed to every device in the room.
//
// A room with nobody in it is not an error the user can fix by typing again, so
// the text is kept and the console says why nothing went — the same courtesy
// the one-to-one loop pays to a handshake that has not finished.
func (r *Room) say() {
	msg := r.input.take()
	if msg == "" {
		fmt.Fprintln(r.out)
		printCursor(r.out)

		return
	}

	if sent := r.hub.Broadcast("", []byte(msg)); sent == 0 {
		r.input.put(msg)
		notice(r.out, r.input, "Not sent: no device is connected")

		return
	}

	fmt.Fprintf(r.out, "\rYou: %s\n", msg)
	printCursor(r.out)
}

// sessionLabel names a session's peer: the name the handshake authenticated,
// with the address the transport knows, so two devices with one hostname are
// still two lines.
func sessionLabel(s session.Session) string {
	if sealed, ok := s.(*e2e.Conn); ok {
		for _, p := range sealed.Peers() {
			if name := p.Name(); name != "" {
				if addr := s.RemoteAddr(); addr != "" {
					return name + " (" + addr + ")"
				}

				return name
			}
		}
	}

	if addr := s.RemoteAddr(); addr != "" {
		return addr
	}

	return "peer"
}

// leaveNotice is what the console says when a device leaves the room.
func leaveNotice(s session.Session, reason string) string {
	out := "Left: " + sessionLabel(s)
	if reason != "" {
		out += " — " + reason
	}

	return out
}
