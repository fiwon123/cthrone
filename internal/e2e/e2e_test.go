package e2e

import (
	"bytes"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/session"
	"github.com/fiwon123/cthrone/internal/wire"
)

// secret is what a test pair shares. Long enough for MinSecret, and the same in
// both directions because that is what the tool hands out.
var secret = []byte("a-shared-secret-for-the-tests-01")

// pair is two wrapped ends of an in-memory link.
type pair struct {
	a, b *Conn
	wa   *watch
	wb   *watch
	sa   *pipeSession
	sb   *pipeSession
}

// newPair wraps both ends. The second wrap is where the exchange finishes, so
// the watchers are attached to the first end before the second can talk to it.
func newPair(t *testing.T, secretA, secretB []byte, mode Mode) *pair {
	t.Helper()

	sa, sb := newPipe("alice"), newPipe("bob")
	link(sa, sb)

	a, err := New(sa, Config{Secret: secretA, Name: "alice", Mode: mode})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	p := &pair{a: a, sa: sa, sb: sb}
	p.wa = observe(a)

	b, err := New(sb, Config{Secret: secretB, Name: "bob", Mode: mode})
	if err != nil {
		t.Fatalf("wrapping bob: %v", err)
	}

	p.b = b
	p.wb = observe(b)

	return p
}

// bothVerified fails the test unless each side accepted the other.
func (p *pair) bothVerified(t *testing.T) {
	t.Helper()

	for name, c := range map[string]*Conn{"alice": p.a, "bob": p.b} {
		peers := c.Peers()

		if len(peers) != 1 {
			t.Fatalf("%s verified %d peers, want 1 (closed: %v)", name, len(peers), c.IsClosed())
		}
	}

	if peers := p.a.Peers(); peers[0].Device != "bob" {
		t.Fatalf("alice sees %q, want bob", peers[0].Device)
	}

	if peers := p.b.Peers(); peers[0].Device != "alice" {
		t.Fatalf("bob sees %q, want alice", peers[0].Device)
	}
}

func TestHandshakeCarriesMessages(t *testing.T) {
	p := newPair(t, secret, secret, Single)

	p.bothVerified(t)

	if got := p.wa.received(); len(got) != 0 {
		t.Fatalf("alice received %q before anything was sent", got)
	}

	if err := p.a.Send([]byte("morning, bob")); err != nil {
		t.Fatalf("alice sends: %v", err)
	}

	got := p.wb.received()
	if len(got) != 1 || string(got[0]) != "morning, bob" {
		t.Fatalf("bob received %q, want the message", got)
	}

	if err := p.b.Send([]byte("morning, alice")); err != nil {
		t.Fatalf("bob sends: %v", err)
	}

	got = p.wa.received()
	if len(got) != 1 || string(got[0]) != "morning, alice" {
		t.Fatalf("alice received %q, want the answer", got)
	}
}

func TestConnForwardsTheSessionIdentity(t *testing.T) {
	p := newPair(t, secret, secret, Single)

	if p.a.ID() != "alice" || p.a.RemoteAddr() != "alice.example:8080" {
		t.Fatalf("identity is %q/%q", p.a.ID(), p.a.RemoteAddr())
	}
}

func TestSealedPayloadHidesTheText(t *testing.T) {
	p := newPair(t, secret, secret, Single)

	text := []byte("the treasure is under the third flagstone")
	if err := p.a.Send(text); err != nil {
		t.Fatalf("alice sends: %v", err)
	}

	frame := p.sa.last(t)

	if bytes.Contains(frame, []byte("treasure")) {
		t.Fatalf("the frame carries the text in the clear: %q", frame)
	}

	if frame[0] != SealVersion {
		t.Fatalf("frame starts with 0x%02x, want the seal version", frame[0])
	}

	if want := 1 + TagLen + NonceSize + len(text) + TagSize; len(frame) != want {
		t.Fatalf("frame is %d bytes, want %d", len(frame), want)
	}

	// A second payload must not repeat the first frame: the counter moved on.
	if err := p.a.Send(text); err != nil {
		t.Fatalf("alice sends again: %v", err)
	}

	if second := p.sa.last(t); bytes.Equal(frame, second) {
		t.Fatal("the same payload produced the same frame twice")
	}
}

func TestWrongSecretIsRefused(t *testing.T) {
	other := []byte("a-different-shared-secret-02")

	p := newPair(t, secret, other, Single)

	if !p.a.IsClosed() || !p.b.IsClosed() {
		t.Fatalf("a peer without the secret was not refused (alice closed: %v, bob closed: %v)",
			p.a.IsClosed(), p.b.IsClosed())
	}

	refused := p.wa.refused()
	if len(refused) == 0 || !errors.Is(refused[0], ErrMAC) {
		t.Fatalf("alice refused with %v, want a MAC failure", refused)
	}

	if !bytes.Contains([]byte(p.wa.closeReason()), []byte("did not prove")) {
		t.Fatalf("close reason is %q", p.wa.closeReason())
	}

	if got := p.wa.received(); len(got) != 0 {
		t.Fatalf("a peer with the wrong secret got %q through", got)
	}
}

func TestSecretTooShortIsRejected(t *testing.T) {
	_, err := New(newPipe("alice"), Config{Secret: []byte("short")})
	if !errors.Is(err, ErrSecret) {
		t.Fatalf("wrapping with a short secret returned %v, want ErrSecret", err)
	}

	if _, err := New(nil, Config{Secret: secret}); !errors.Is(err, ErrHandshake) {
		t.Fatalf("wrapping no session returned %v, want ErrHandshake", err)
	}
}

func TestSendBeforeTheHandshakeIsRefused(t *testing.T) {
	c, err := New(newPipe("alice"), Config{Secret: secret, Name: "alice", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping: %v", err)
	}

	defer c.Close("done")

	if err := c.Send([]byte("anyone there?")); !errors.Is(err, ErrHandshake) {
		t.Fatalf("Send before the handshake returned %v, want ErrHandshake", err)
	}
}

func TestSendAfterCloseIsRefused(t *testing.T) {
	p := newPair(t, secret, secret, Single)

	p.a.Close("bye")

	if err := p.a.Send([]byte("still here")); !errors.Is(err, session.ErrClosed) {
		t.Fatalf("Send after Close returned %v, want ErrClosed", err)
	}
}

func TestReplayedFrameIsRefused(t *testing.T) {
	p := newPair(t, secret, secret, Single)

	if err := p.a.Send([]byte("once")); err != nil {
		t.Fatalf("alice sends: %v", err)
	}

	frame := p.sa.last(t)

	if len(p.wb.received()) != 1 {
		t.Fatalf("the first delivery did not arrive")
	}

	p.sb.Deliver(frame)

	refused := p.wb.refused()
	if len(refused) == 0 || !errors.Is(refused[0], ErrSequence) {
		t.Fatalf("the replay was refused with %v, want ErrSequence", refused)
	}

	if len(p.wb.received()) != 1 {
		t.Fatal("the replay was delivered")
	}
}

func TestOutOfOrderFrameIsRefused(t *testing.T) {
	p := newPair(t, secret, secret, Single)

	p.sa.silence()

	for _, text := range []string{"one", "two"} {
		if err := p.a.Send([]byte(text)); err != nil {
			t.Fatalf("alice sends %q: %v", text, err)
		}
	}

	frames := p.sa.outgoing()
	if len(frames) != 4 {
		t.Fatalf("alice sent %d frames, want her hello, her confirm and two payloads", len(frames))
	}

	// The second payload arrives where the first was due.
	p.sb.Deliver(frames[len(frames)-1])

	refused := p.wb.refused()
	if len(refused) == 0 || !errors.Is(refused[0], ErrSequence) {
		t.Fatalf("the reordering was refused with %v, want ErrSequence", refused)
	}

	if got := p.wb.received(); len(got) != 0 {
		t.Fatalf("an out-of-order frame was delivered: %q", got)
	}
}

func TestPlaintextIsRefused(t *testing.T) {
	p := newPair(t, secret, secret, Single)

	p.sb.Deliver([]byte("a sentence from a peer that does not encrypt"))

	refused := p.wb.refused()
	if len(refused) == 0 || !errors.Is(refused[0], ErrPlaintext) {
		t.Fatalf("plaintext was refused with %v, want ErrPlaintext", refused)
	}

	if got := p.wb.received(); len(got) != 0 {
		t.Fatalf("plaintext reached the consumer: %q", got)
	}

	if !p.b.IsClosed() {
		t.Fatal("a single correspondent that does not encrypt did not end the session")
	}
}

func TestUnsealedFrameIsRefused(t *testing.T) {
	p := newPair(t, secret, secret, Single)

	frame, err := wire.Encode(wire.Header{
		Kind:   wire.KindFileEnd,
		ID:     "f1",
		Name:   "notes.txt",
		SHA256: wire.SumHex([32]byte{}),
	}, nil)
	if err != nil {
		t.Fatalf("building an unsealed frame: %v", err)
	}

	p.sb.Deliver(frame)

	refused := p.wb.refused()
	if len(refused) == 0 || !errors.Is(refused[0], ErrPlaintext) {
		t.Fatalf("an unsealed frame was refused with %v, want ErrPlaintext", refused)
	}
}

func TestTruncatedSealedFrameIsRefused(t *testing.T) {
	p := newPair(t, secret, secret, Single)

	p.sb.Deliver([]byte{SealVersion, 0, 0, 0})

	refused := p.wb.refused()
	if len(refused) == 0 || !errors.Is(refused[0], ErrSeal) {
		t.Fatalf("a truncated frame was refused with %v, want ErrSeal", refused)
	}

	if got := p.wb.received(); len(got) != 0 {
		t.Fatalf("a truncated frame was delivered: %q", got)
	}
}

func TestASecondHelloIsIgnored(t *testing.T) {
	p := newPair(t, secret, secret, Single)

	p.bothVerified(t)

	hello := p.sa.outgoing()[0]

	before := len(p.sb.outgoing())
	p.sb.Deliver(hello)

	if after := len(p.sb.outgoing()); after != before {
		t.Fatalf("a repeated hello drew %d answers, want none", after-before)
	}

	if p.b.IsClosed() {
		t.Fatal("a repeated hello closed the session")
	}

	p.bothVerified(t)
}

func TestConfirmArrivingBeforeItsHello(t *testing.T) {
	sa, sb := newPipe("alice"), newPipe("bob")
	link(sa, sb)

	// Bob's hello never reaches alice. Bob's confirm does, and it has to be
	// enough: it proves the secret and carries the same fields, so holding it
	// back for a hello that will not come would leave both sides waiting.
	sb.drop = func(payload []byte) bool { return isStep(payload, stepHello) }

	a, err := New(sa, Config{Secret: secret, Name: "alice", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	wa := observe(a)

	b, err := New(sb, Config{Secret: secret, Name: "bob", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping bob: %v", err)
	}

	wb := observe(b)

	defer a.Close("done")
	defer b.Close("done")

	if got := len(a.Peers()); got != 1 {
		t.Fatalf("alice verified %d peers without bob's hello, want 1", got)
	}

	if got := len(b.Peers()); got != 1 {
		t.Fatalf("bob verified %d peers, want 1", got)
	}

	if err := a.Send([]byte("we crossed")); err != nil {
		t.Fatalf("alice sends: %v", err)
	}

	if got := wb.received(); len(got) != 1 || string(got[0]) != "we crossed" {
		t.Fatalf("bob received %q", got)
	}

	if len(wa.received()) != 0 {
		t.Fatalf("alice received %q, she only sent", wa.received())
	}
}

func TestSinglePeerHandshakeTimeoutCloses(t *testing.T) {
	c, err := New(newPipe("alice"), Config{Secret: secret, Name: "alice", Timeout: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("wrapping: %v", err)
	}

	w := observe(c)

	reason := w.waitClosed(t, 5*time.Second)
	if !bytes.Contains([]byte(reason), []byte("no encrypted handshake")) {
		t.Fatalf("close reason is %q", reason)
	}
}

func TestBusKeepsTheRoomWhenAStrangerFails(t *testing.T) {
	const (
		alice = iota
		bob
		mallory
	)

	ends := []*pipeSession{newPipe("alice"), newPipe("bob"), newPipe("mallory")}
	for i := range ends {
		for j := i + 1; j < len(ends); j++ {
			link(ends[i], ends[j])
		}
	}

	secrets := [][]byte{secret, secret, []byte("a-different-shared-secret-02")}
	names := []string{"alice", "bob", "mallory"}

	conns := make([]*Conn, len(ends))
	watches := make([]*watch, len(ends))

	for i, end := range ends {
		c, err := New(end, Config{Secret: secrets[i], Name: names[i], Mode: Bus})
		if err != nil {
			t.Fatalf("wrapping %s: %v", names[i], err)
		}

		conns[i] = c
		watches[i] = observe(c)
	}

	for i, want := range []int{1, 1, 0} {
		if got := len(conns[i].Peers()); got != want {
			t.Fatalf("%s verified %d peers, want %d", names[i], got, want)
		}

		if conns[i].IsClosed() {
			t.Fatalf("%s was closed, a bus must keep the room", names[i])
		}
	}

	// Mallory's own refusals happen while it is being wrapped, before anything
	// can hook it; what the room saw is what matters, and both peers report the
	// stranger without closing.
	for _, i := range []int{alice, bob} {
		if refused := watches[i].refused(); len(refused) == 0 || !errors.Is(refused[0], ErrMAC) {
			t.Fatalf("%s refused with %v, want a MAC failure", names[i], refused)
		}
	}

	if err := conns[alice].Send([]byte("still here, bob")); err != nil {
		t.Fatalf("alice sends in a room that refused a stranger: %v", err)
	}

	if got := watches[bob].received(); len(got) != 1 || string(got[0]) != "still here, bob" {
		t.Fatalf("bob received %q", got)
	}

	if got := watches[mallory].received(); len(got) != 0 {
		t.Fatalf("mallory received %q", got)
	}

	if err := conns[mallory].Send([]byte("let me in")); !errors.Is(err, ErrHandshake) {
		t.Fatalf("mallory sends with no verified peer and gets %v, want ErrHandshake", err)
	}
}

func TestBusKeepsTheRoomOnPlaintext(t *testing.T) {
	a, b := newPipe("alice"), newPipe("bob")
	link(a, b)

	ca, err := New(a, Config{Secret: secret, Name: "alice", Mode: Bus})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	wa := observe(ca)

	cb, err := New(b, Config{Secret: secret, Name: "bob", Mode: Bus})
	if err != nil {
		t.Fatalf("wrapping bob: %v", err)
	}

	a.Deliver([]byte("plaintext on a bus"))

	refused := wa.refused()
	if len(refused) == 0 || !errors.Is(refused[0], ErrPlaintext) {
		t.Fatalf("plaintext was refused with %v, want ErrPlaintext", refused)
	}

	if ca.IsClosed() {
		t.Fatal("a bus closed on one plaintext frame")
	}

	if err := cb.Send([]byte("carrying on")); err != nil {
		t.Fatalf("bob sends after the refusal: %v", err)
	}

	if got := wa.received(); len(got) != 1 || string(got[0]) != "carrying on" {
		t.Fatalf("alice received %q", got)
	}
}

func TestPeersAreSealedForEachOfThem(t *testing.T) {
	// On a bus, one payload becomes one frame per peer: the keys are pairwise,
	// so a frame sealed for one peer must not open for another — and a peer
	// that sees it must skip it rather than call it a forgery.
	ends := []*pipeSession{newPipe("alice"), newPipe("bob"), newPipe("carol")}
	for i := range ends {
		for j := i + 1; j < len(ends); j++ {
			link(ends[i], ends[j])
		}
	}

	conns := make([]*Conn, len(ends))
	watches := make([]*watch, len(ends))
	names := []string{"alice", "bob", "carol"}

	for i, end := range ends {
		c, err := New(end, Config{Secret: secret, Name: names[i], Mode: Bus})
		if err != nil {
			t.Fatalf("wrapping %s: %v", names[i], err)
		}

		conns[i] = c
		watches[i] = observe(c)
	}

	if got := len(conns[0].Peers()); got != 2 {
		t.Fatalf("alice verified %d peers, want 2", got)
	}

	if err := conns[0].Send([]byte("to both")); err != nil {
		t.Fatalf("alice sends: %v", err)
	}

	frames := ends[0].outgoing()
	payloads := frames[len(frames)-2:]

	if len(payloads) != 2 {
		t.Fatalf("alice sent %d frames for two peers, want 2", len(payloads))
	}

	if bytes.Equal(payloads[0], payloads[1]) {
		t.Fatal("the same frame went to two peers")
	}

	for i, name := range names[1:] {
		if got := watches[i+1].received(); len(got) != 1 || string(got[0]) != "to both" {
			t.Fatalf("%s received %q, want the payload once", name, got)
		}

		if refused := watches[i+1].refused(); len(refused) != 0 {
			t.Fatalf("%s refused a frame meant for a third peer: %v", name, refused)
		}
	}
}

func TestPeersListOnlyWhatCompleted(t *testing.T) {
	c, err := New(newPipe("alice"), Config{Secret: secret, Name: "alice", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping: %v", err)
	}

	defer c.Close("done")

	// A hello from a peer that never confirms is recorded but not accepted.
	c.receiveHello(handshake{V: Version, Step: stepHello, Key: bytes.Repeat([]byte{7}, KeySize), Nonce: bytes.Repeat([]byte{8}, HandshakeNonce), Device: "stranger"})

	if got := len(c.Peers()); got != 0 {
		t.Fatalf("%d peers are listed before a confirm arrived, want none", got)
	}
}

func TestAFullRoomEvictsAStrangerAndKeepsThePeer(t *testing.T) {
	p := newPair(t, secret, secret, Bus)
	p.bothVerified(t)

	// Fill the room with peers that only ever said hello. The peer that
	// completed the handshake must survive them, and the room must stay open.
	for _, key := range strangerKeys(t, maxPeers) {
		p.a.receiveHello(handshake{
			V:      Version,
			Step:   stepHello,
			Key:    key,
			Nonce:  bytes.Repeat([]byte{9}, HandshakeNonce),
			Device: "stranger",
		})
	}

	if got := len(p.a.Peers()); got != 1 {
		t.Fatalf("alice kept %d verified peers, want bob", got)
	}

	if p.a.IsClosed() {
		t.Fatal("a crowd of strangers closed the room")
	}

	// One more stranger makes room by evicting the oldest one, so a peer that
	// is new to the room is never locked out by the ones that went quiet.
	newcomer := strangerKeys(t, 1)[0]
	p.a.receiveHello(handshake{
		V:      Version,
		Step:   stepHello,
		Key:    newcomer,
		Nonce:  bytes.Repeat([]byte{9}, HandshakeNonce),
		Device: "newcomer",
	})

	p.a.mu.Lock()
	_, recorded := p.a.peers[string(newcomer)]
	p.a.mu.Unlock()

	if !recorded {
		t.Fatal("a newcomer was locked out of a full room of strangers")
	}

	if err := p.b.Send([]byte("still here")); err != nil {
		t.Fatalf("bob sends: %v", err)
	}

	if got := p.wa.received(); len(got) != 1 || string(got[0]) != "still here" {
		t.Fatalf("alice received %q", got)
	}
}

// strangerKeys makes n public keys that belong to nobody, so a test can fill a
// room without a second end.
func strangerKeys(t *testing.T, n int) [][]byte {
	t.Helper()

	keys := make([][]byte, n)

	for i := range keys {
		key, err := ecdh.X25519().GenerateKey(rand.Reader)
		if err != nil {
			t.Fatalf("generating a stranger's key: %v", err)
		}

		keys[i] = key.PublicKey().Bytes()
	}

	return keys
}

// silence drops everything this end sends from now on, so a test can deliver a
// captured frame by hand.
func (p *pipeSession) silence() {
	p.mu.Lock()
	p.drop = func([]byte) bool { return true }
	p.mu.Unlock()
}

// isStep reports whether payload is a handshake step named step.
func isStep(payload []byte, step string) bool {
	frame, err := wire.Parse(payload)
	if err != nil || frame.Header.Kind != wire.KindHandshake {
		return false
	}

	var h handshake
	if json.Unmarshal(frame.Payload, &h) != nil {
		return false
	}

	return h.Step == step
}
