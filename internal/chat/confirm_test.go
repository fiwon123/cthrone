package chat

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/fiwon123/cthrone/internal/e2e"
)

// The question is the host's whole side of a first contact, so the answers it
// accepts have to be exactly the ones a person means: a "y" accepts, and
// everything else — including an empty line and a terminal that closed — does
// not. Silence is not a yes.
func TestConfirmFirstContact(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		want     bool
		wantText []string
	}{
		{name: "y accepts", input: "y\n", want: true, wantText: []string{"Accepted", "pinned"}},
		{name: "yes accepts", input: "yes\n", want: true, wantText: []string{"Accepted"}},
		{name: "YES accepts", input: "YES\n", want: true, wantText: []string{"Accepted"}},
		{name: "a carriage return ends the line", input: "y\r", want: true, wantText: []string{"Accepted"}},
		{name: "n refuses", input: "n\n", want: false, wantText: []string{"Refused", "nothing was pinned"}},
		{name: "an empty line refuses", input: "\n", want: false, wantText: []string{"Refused"}},
		{name: "an answer with nothing behind it refuses", input: "y", want: true, wantText: []string{"Accepted"}},
		{name: "a closed terminal refuses", input: "", want: false, wantText: []string{"No answer", "nothing was pinned"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer

			confirm := ConfirmFirstContact(strings.NewReader(tt.input), &out)

			if got := confirm(e2e.FirstContact{Name: "phone", Fingerprint: "SHA256:abc", Code: "123 456"}); got != tt.want {
				t.Errorf("ConfirmFirstContact(%q) = %v, want %v", tt.input, got, tt.want)
			}

			for _, want := range tt.wantText {
				if !strings.Contains(out.String(), want) {
					t.Errorf("the terminal output %q does not contain %q", out.String(), want)
				}
			}
		})
	}
}

// The question has to show what the two screens compare: the name, the code,
// and the fingerprint when the handshake carried one.
func TestConfirmQuestion(t *testing.T) {
	named := confirmQuestion(e2e.FirstContact{Name: "phone", Fingerprint: "SHA256:abc", Code: "123 456"})

	for _, want := range []string{"phone wants to connect", "SHA256:abc", "123 456", "must agree"} {
		if !strings.Contains(named, want) {
			t.Errorf("confirmQuestion() = %q, want it to contain %q", named, want)
		}
	}

	unnamed := confirmQuestion(e2e.FirstContact{Code: "123 456"})

	if !strings.Contains(unnamed, "A device wants to connect") {
		t.Errorf("confirmQuestion() = %q, want an unnamed device described as one", unnamed)
	}

	if strings.Contains(unnamed, "fingerprint") {
		t.Errorf("confirmQuestion() = %q, want no fingerprint line when there is none", unnamed)
	}
}

// A host with nobody to ask accepts, and says that it did: a pin nobody
// compared is worth knowing about.
func TestAcceptFirstContact(t *testing.T) {
	var out bytes.Buffer

	accept := AcceptFirstContact(&out)

	if !accept(e2e.FirstContact{Name: "phone", Code: "123 456"}) {
		t.Error("a host with no terminal refused a device")
	}

	for _, want := range []string{"No terminal", "phone", "accepted and pinned"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("the terminal output %q does not contain %q", out.String(), want)
		}
	}

	out.Reset()

	if !accept(e2e.FirstContact{Code: "123 456"}) {
		t.Error("a host with no terminal refused an unnamed device")
	}

	if !strings.Contains(out.String(), "an unnamed device") {
		t.Errorf("the terminal output %q does not name an unnamed device", out.String())
	}
}

// The notice is printed on both sides of a first contact: the side that is not
// asked anything has nothing else to show while it waits.
func TestFirstContactNotice(t *testing.T) {
	got := FirstContactNotice(e2e.FirstContact{Name: "phone", Code: "123 456"})

	for _, want := range []string{"phone", "123 456", "Both screens"} {
		if !strings.Contains(got, want) {
			t.Errorf("FirstContactNotice() = %q, want it to contain %q", got, want)
		}
	}

	unnamed := FirstContactNotice(e2e.FirstContact{Code: "123 456"})

	if !strings.Contains(unnamed, "an unnamed device") {
		t.Errorf("FirstContactNotice() = %q, want an unnamed device named as such", unnamed)
	}
}

// The prompt is asked once per connection, and a second question must not
// swallow the first answer: reading stops at the end of the line.
func TestConfirmFirstContactReadsOneLineAtATime(t *testing.T) {
	in := strings.NewReader("y\nn\n")
	var out bytes.Buffer

	confirm := ConfirmFirstContact(in, &out)

	if !confirm(e2e.FirstContact{Name: "one", Code: "111 111"}) {
		t.Fatal("the first question refused a yes")
	}

	if confirm(e2e.FirstContact{Name: "two", Code: "222 222"}) {
		t.Fatal("the second question accepted a no")
	}
}

// A reader that fails without producing anything is a terminal that went away,
// and that is a refusal rather than an error the caller has to handle.
func TestConfirmFirstContactOnABrokenReader(t *testing.T) {
	var out bytes.Buffer

	confirm := ConfirmFirstContact(errReader{err: errors.New("closed")}, &out)

	if confirm(e2e.FirstContact{Name: "phone", Code: "123 456"}) {
		t.Error("a broken reader accepted the device")
	}
}

// errReader fails on the first read, the way a terminal that was closed under
// the prompt does.
type errReader struct{ err error }

func (r errReader) Read([]byte) (int, error) { return 0, r.err }

var _ io.Reader = errReader{}
