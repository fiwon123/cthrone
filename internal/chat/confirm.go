package chat

import (
	"fmt"
	"io"
	"strings"

	"github.com/fiwon123/cthrone/internal/e2e"
)

// ConfirmFirstContact asks the terminal whether a device met for the first time
// may join.
//
// The question prints what the person at the other screen is looking at — the
// device's name, its fingerprint and the six-digit code — and reads one line.
// "y" or "yes" accepts; anything else, an empty line, or a terminal that closes
// before answering refuses. Silence is not a yes: the whole point of the
// question is that a device nobody vouched for is not let in.
//
// The reader is read one byte at a time, because a second device can connect
// while the chat loop already has the same terminal in raw mode, where a line
// ends with a carriage return rather than a newline.
func ConfirmFirstContact(in io.Reader, out io.Writer) func(e2e.FirstContact) bool {
	return func(c e2e.FirstContact) bool {
		fmt.Fprintln(out)
		fmt.Fprintln(out, confirmQuestion(c))
		fmt.Fprint(out, "Accept this device? [y/N] ")

		accept, answered := readAnswer(in)
		if !answered {
			fmt.Fprintln(out, "No answer — the device was refused and nothing was pinned.")

			return false
		}

		if accept {
			fmt.Fprintln(out, "Accepted — this device is pinned.")

			return true
		}

		fmt.Fprintln(out, "Refused — nothing was pinned.")

		return false
	}
}

// AcceptFirstContact is the answer for a host that has nobody to ask: it says
// so when a device arrives, and accepts and pins it.
//
// A headless host is still a host. Refusing every device it meets would make it
// unusable, and there is no person at one to compare a code with — but the
// acceptance is not silent, because a pin with no comparison is weaker than one
// that had a person behind it.
func AcceptFirstContact(out io.Writer) func(e2e.FirstContact) bool {
	return func(c e2e.FirstContact) bool {
		fmt.Fprintln(out, "No terminal to confirm "+contactLabel(c)+" — it is accepted and pinned without a comparison. Run the host in a terminal to compare the six-digit code instead.")

		return true
	}
}

// FirstContactNotice is what the terminal says when a device is met for the
// first time: the code the two screens have to agree on.
//
// It is printed on both sides of a first contact, because the screen that is
// not asked anything is the one that has to show what it is waiting for.
func FirstContactNotice(c e2e.FirstContact) string {
	return "First contact with " + contactLabel(c) + " — code " + c.Code + ". Both screens must show the same code."
}

// confirmQuestion is the text of the terminal's question: everything a person
// needs to decide, and nothing they have to be told separately.
func confirmQuestion(c e2e.FirstContact) string {
	question := "A device wants to connect."
	if c.Name != "" {
		question = c.Name + " wants to connect."
	}

	if c.Fingerprint != "" {
		question += "\nIts fingerprint is " + c.Fingerprint + "."
	}

	return question + "\nThe code both screens show is " + c.Code + " — they must agree."
}

// contactLabel names a device in a sentence, where an empty name would leave a
// hole.
func contactLabel(c e2e.FirstContact) string {
	if c.Name == "" {
		return "an unnamed device"
	}

	return c.Name
}

// readAnswer reads one line and reports whether it was a yes, and whether there
// was a line at all.
func readAnswer(in io.Reader) (bool, bool) {
	var line []byte

	buf := make([]byte, 1)

	for {
		n, err := in.Read(buf)
		if n > 0 {
			if buf[0] == '\r' || buf[0] == '\n' {
				return isYes(string(line)), true
			}

			line = append(line, buf[0])
		}

		if err != nil {
			if len(line) > 0 {
				return isYes(string(line)), true
			}

			return false, false
		}
	}
}

// isYes reports whether an answer is an acceptance.
func isYes(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}
