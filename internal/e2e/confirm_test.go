package e2e

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

// contactPair is two ends that have never met, each wrapped with its own
// identity, pin store and — on the side that asks — a confirmation gate.
type contactPair struct {
	a, b *Conn
	wa   *watch
	wb   *watch
}

// newContactPair wraps both ends. The first wrap records the first hello and
// the second is where the exchange runs, exactly as newIdentityPair does, so a
// test controls both configurations instead of only the shared ones.
func newContactPair(t *testing.T, cfgA, cfgB Config) *contactPair {
	t.Helper()

	sa, sb := newPipe("alice"), newPipe("bob")
	link(sa, sb)

	a, err := New(sa, cfgA)
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	p := &contactPair{a: a}
	p.wa = observe(a)

	b, err := New(sb, cfgB)
	if err != nil {
		t.Fatalf("wrapping bob: %v", err)
	}

	p.b = b
	p.wb = observe(b)

	return p
}

func testHandshakeKey(t *testing.T) []byte {
	t.Helper()

	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	return key.PublicKey().Bytes()
}

func testNonce(t *testing.T) []byte {
	t.Helper()

	buf := make([]byte, HandshakeNonce)
	if _, err := rand.Read(buf); err != nil {
		t.Fatal(err)
	}

	return buf
}

// size is how many peers this store has pinned.
func (m *memPins) size() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	return len(m.keys)
}

// The two screens agree on one code, and a splice cannot make them agree: each
// leg of a man in the middle has its own ephemeral key, so the code derived
// from it differs even when the name on both legs is the same.
func TestConfirmCodeMatchesOnBothSidesAndNotAcrossASplice(t *testing.T) {
	alice, bob := testIdentity(t), testIdentity(t)

	aliceHello := identityHello(t, alice, "alice", testHandshakeKey(t), testNonce(t))
	bobHello := identityHello(t, bob, "bob", testHandshakeKey(t), testNonce(t))

	// The same name and a different key: what the far leg of a middleman looks
	// like from this side.
	impostorHello := identityHello(t, testIdentity(t), "bob", testHandshakeKey(t), testNonce(t))

	ours, err := confirmCode(aliceHello, bobHello)
	if err != nil {
		t.Fatalf("deriving the code: %v", err)
	}

	theirs, err := confirmCode(bobHello, aliceHello)
	if err != nil {
		t.Fatalf("deriving the code from the other side: %v", err)
	}

	if ours != theirs {
		t.Errorf("the two sides disagree: %q and %q", ours, theirs)
	}

	if !regexp.MustCompile(`^\d{3} \d{3}$`).MatchString(ours) {
		t.Errorf("code %q is not six digits in two groups of three", ours)
	}

	spliced, err := confirmCode(aliceHello, impostorHello)
	if err != nil {
		t.Fatalf("deriving the spliced code: %v", err)
	}

	if spliced == ours {
		t.Errorf("a substituted key produced the same code %q", ours)
	}
}

// The code is read aloud, so it is two groups of three digits and never starts
// with a zero.
func TestFormatCode(t *testing.T) {
	tests := []struct {
		name string
		n    uint64
		want string
	}{
		{name: "the bottom of the range", n: 100000, want: "100 000"},
		{name: "a code with a zero group", n: 123000, want: "123 000"},
		{name: "a code with two zeroes", n: 120001, want: "120 001"},
		{name: "the top of the range", n: 999999, want: "999 999"},
		{name: "a short value is padded", n: 1, want: "000 001"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatCode(tt.n); got != tt.want {
				t.Errorf("formatCode(%d) = %q, want %q", tt.n, got, tt.want)
			}
		})
	}
}

// A host that says yes lets the device in, and both sides end up pinned.
func TestConfirmAcceptedPinsBothSides(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)
	pinsAlice, pinsBob := newPins(), newPins()

	var (
		mu     sync.Mutex
		asked  []FirstContact
		answer = true
	)

	confirm := func(c FirstContact) bool {
		mu.Lock()
		asked = append(asked, c)
		mu.Unlock()

		return answer
	}

	p := newContactPair(t,
		Config{Identity: idAlice, Pins: pinsAlice, Name: "alice", Timeout: time.Hour},
		Config{Identity: idBob, Pins: pinsBob, Name: "bob", Timeout: time.Hour, Confirm: confirm},
	)

	waitFor(t, func() bool { return len(p.b.Peers()) == 1 && len(p.a.Peers()) == 1 })

	mu.Lock()
	questions := append([]FirstContact(nil), asked...)
	mu.Unlock()

	if len(questions) != 1 {
		t.Fatalf("bob asked %d times, want once: %+v", len(questions), questions)
	}

	if questions[0].Name != "alice" {
		t.Errorf("the question named %q, want alice", questions[0].Name)
	}

	if want := testFingerprint(idAlice.Public()); questions[0].Fingerprint != want {
		t.Errorf("the question showed fingerprint %q, want %q", questions[0].Fingerprint, want)
	}

	if len(pinsAlice.keyFor("bob")) == 0 || len(pinsBob.keyFor("alice")) == 0 {
		t.Fatalf("pins after acceptance: alice=%d bob=%d entries, want one each",
			pinsAlice.size(), pinsBob.size())
	}

	if peers := p.a.Peers(); len(peers) != 1 || !peers[0].First {
		t.Fatalf("alice's contact = %+v, want one first contact", peers)
	}
}

// A host that says no leaves nothing behind: no pin on either side, and no
// session on the side that was asked. The client is never authenticated,
// because the confirm it would have to verify is the one being withheld.
func TestConfirmRefusedPinsNothingAndAttachesNeitherSide(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)
	pinsAlice, pinsBob := newPins(), newPins()

	p := newContactPair(t,
		Config{Identity: idAlice, Pins: pinsAlice, Name: "alice", Timeout: time.Hour},
		Config{
			Identity: idBob, Pins: pinsBob, Name: "bob", Timeout: time.Hour,
			Confirm: func(FirstContact) bool { return false },
		},
	)

	reason := p.wb.waitClosed(t, time.Second)
	if !strings.Contains(reason, "not confirmed") {
		t.Errorf("bob closed with %q, want a confirmation refusal", reason)
	}

	if refused := p.wb.refused(); len(refused) == 0 || !errors.Is(refused[0], ErrConfirm) {
		t.Errorf("bob's refusal = %v, want ErrConfirm", refused)
	}

	if got := pinsBob.size(); got != 0 {
		t.Errorf("bob pinned %d devices after refusing, want none", got)
	}

	if got := len(p.b.Peers()); got != 0 {
		t.Errorf("bob tracks %d peers after refusing, want none", got)
	}

	// The other side: the confirm that would have authenticated the host was
	// never sent, so the client has no peer and wrote no pin either.
	if got := pinsAlice.size(); got != 0 {
		t.Errorf("alice pinned %d devices, want none", got)
	}

	if got := len(p.a.Peers()); got != 0 {
		t.Errorf("alice attached %d peers, want none", got)
	}

	if got := p.wa.received(); len(got) != 0 {
		t.Errorf("alice received %q, want nothing", got)
	}

	p.a.Close("done")
}

// Nothing reaches the session before the question is answered: the confirm is
// held back, so the other side is still unauthenticated while the person
// decides.
func TestConfirmWithholdsTheConfirmUntilAnswered(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)

	answer := make(chan bool, 1)
	asked := make(chan FirstContact, 1)

	p := newContactPair(t,
		Config{Identity: idAlice, Pins: newPins(), Name: "alice", Timeout: time.Hour},
		Config{
			Identity: idBob, Pins: newPins(), Name: "bob", Timeout: time.Hour,
			Confirm: func(c FirstContact) bool {
				asked <- c

				return <-answer
			},
		},
	)

	question := <-asked

	if got := len(p.a.Peers()); got != 0 {
		t.Fatalf("alice attached %d peers before the answer, want none", got)
	}

	answer <- true
	waitFor(t, func() bool { return len(p.a.Peers()) == 1 })

	// The client derives the same code the host is being shown.
	code := p.wa.received()
	if len(code) != 0 {
		t.Errorf("alice received %q before the peer authenticated", code)
	}

	contact := make(chan FirstContact, 1)
	p.a.OnFirstContact(func(c FirstContact) { contact <- c })

	select {
	case c := <-contact:
		if c.Code != question.Code {
			t.Errorf("the two screens show %q and %q", c.Code, question.Code)
		}
	case <-time.After(time.Second):
		t.Fatal("alice was never told about the first contact")
	}

	p.a.Close("done")
	p.b.Close("done")
}

// A device that is already pinned is not asked about again: the pin is the
// answer to that question, given once.
func TestConfirmIsNotAskedAboutAReturningDevice(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)
	pinsAlice, pinsBob := newPins(), newPins()
	pinsAlice.pin("bob", idBob.Public())
	pinsBob.pin("alice", idAlice.Public())

	var asked int

	var mu sync.Mutex

	p := newContactPair(t,
		Config{Identity: idAlice, Pins: pinsAlice, Name: "alice", Timeout: time.Hour},
		Config{
			Identity: idBob, Pins: pinsBob, Name: "bob", Timeout: time.Hour,
			Confirm: func(FirstContact) bool {
				mu.Lock()
				asked++
				mu.Unlock()

				return false
			},
		},
	)

	waitFor(t, func() bool { return len(p.a.Peers()) == 1 && len(p.b.Peers()) == 1 })

	mu.Lock()
	defer mu.Unlock()

	if asked != 0 {
		t.Errorf("bob asked %d times about a pinned device, want never", asked)
	}

	if peers := p.b.Peers(); len(peers) != 1 || peers[0].First {
		t.Errorf("bob's contact = %+v, want one peer that is not new", peers)
	}
}

// A question nobody answers is a refusal, not a session left half open: the
// window is bounded and the peer is dropped when it closes.
func TestConfirmExpires(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)
	pinsBob := newPins()

	block := make(chan struct{})
	defer close(block)

	p := newContactPair(t,
		Config{Identity: idAlice, Pins: newPins(), Name: "alice", Timeout: time.Hour},
		Config{
			Identity: idBob, Pins: pinsBob, Name: "bob", Timeout: time.Hour,
			ConfirmTimeout: 40 * time.Millisecond,
			Confirm: func(FirstContact) bool {
				<-block

				return true
			},
		},
	)

	reason := p.wb.waitClosed(t, 2*time.Second)
	if !strings.Contains(reason, "not confirmed") {
		t.Errorf("bob closed with %q, want a confirmation refusal", reason)
	}

	if got := pinsBob.size(); got != 0 {
		t.Errorf("bob pinned %d devices after the question expired, want none", got)
	}

	p.a.Close("done")
}

// A hello alone opens no dialog and gets no answer. The question waits until
// the peer has proved it holds the session keys, so a frame someone replayed
// cannot put a name on the screen, and the confirm that would authenticate this
// side is withheld until a person answers.
func TestConfirmIsNotAskedOnAHelloAlone(t *testing.T) {
	hostID, phoneID := testIdentity(t), testIdentity(t)
	asked := make(chan FirstContact, 1)

	pipe := newPipe("host")
	hostConn, err := New(pipe, Config{
		Identity: hostID, Pins: newPins(), Name: "host", Timeout: time.Hour,
		Confirm: func(c FirstContact) bool {
			asked <- c

			return false
		},
	})
	if err != nil {
		t.Fatalf("wrapping the host: %v", err)
	}
	defer hostConn.Close("done")

	// The host's own hello is the only frame it has sent so far.
	before := len(pipe.outgoing())

	hello := handshake{
		V: VersionIdentity, Step: stepHello,
		Key: testHandshakeKey(t), Nonce: testNonce(t), Device: "phone", Id: phoneID.Public(),
	}
	hello.Sig = ed25519.Sign(phoneID.Key, helloClaim(hello))

	frame, err := encodeHandshake(hello)
	if err != nil {
		t.Fatalf("encoding the hello: %v", err)
	}

	pipe.Deliver(frame)

	select {
	case c := <-asked:
		t.Fatalf("the host asked about %q on a hello alone", c.Name)
	case <-time.After(100 * time.Millisecond):
	}

	if got := len(pipe.outgoing()); got != before {
		t.Errorf("the host sent %d frames after an unproven hello, want %d", got, before)
	}

	if peers := hostConn.Peers(); len(peers) != 0 {
		t.Errorf("the host attached %+v on a hello alone", peers)
	}
}

// waitFor polls until cond holds, so a test does not depend on how many frames
// the exchange needs or on how the scheduler interleaves the two ends.
func waitFor(t *testing.T, cond func() bool) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)

	for time.Now().Before(deadline) {
		if cond() {
			return
		}

		time.Sleep(time.Millisecond)
	}

	t.Fatal("the condition did not hold within 2s")
}

// The answer to a question that arrived after the session closed is ignored:
// the handshake must not be kept alive by a dialog nobody can see.
func TestConfirmAfterCloseIsIgnored(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)

	answer := make(chan bool, 1)
	asked := make(chan struct{}, 1)

	p := newContactPair(t,
		Config{Identity: idAlice, Pins: newPins(), Name: "alice", Timeout: time.Hour},
		Config{
			Identity: idBob, Pins: newPins(), Name: "bob", Timeout: time.Hour,
			Confirm: func(FirstContact) bool {
				asked <- struct{}{}

				return <-answer
			},
		},
	)

	<-asked
	p.b.Close("done")
	answer <- true

	if peers := p.b.Peers(); len(peers) != 0 {
		t.Errorf("bob attached %+v to a closed session", peers)
	}

	p.a.Close("done")
}
