package e2e

import (
	"bytes"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/internal/wire"
)

// memPins is a pin store that lives in the test: the same rules as the real
// one, without a file.
type memPins struct {
	mu   sync.Mutex
	keys map[string][]byte

	// reject makes every Pin fail, so the "the pin could not be recorded"
	// path is reachable without an unwritable disk.
	reject error
}

func newPins() *memPins {
	return &memPins{keys: make(map[string][]byte)}
}

// pinID names the entry a peer belongs to: its device name, or its fingerprint
// when it has none.
func pinID(pub []byte, name string) string {
	if name != "" {
		return name
	}

	return testFingerprint(pub)
}

func (m *memPins) Check(pub []byte, name string) (string, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	fingerprint := testFingerprint(pub)
	id := pinID(pub, name)

	if pinned, ok := m.keys[id]; ok {
		if !bytes.Equal(pinned, pub) {
			return fingerprint, false, fmt.Errorf("the name %q is pinned to another key", id)
		}

		return fingerprint, false, nil
	}

	return fingerprint, true, nil
}

func (m *memPins) Pin(pub []byte, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.reject != nil {
		return m.reject
	}

	id := pinID(pub, name)
	if pinned, ok := m.keys[id]; ok && !bytes.Equal(pinned, pub) {
		return fmt.Errorf("the name %q is pinned to another key", id)
	}

	m.keys[id] = append([]byte(nil), pub...)

	return nil
}

// pin records a key for a name as if a previous session had seen it.
func (m *memPins) pin(name string, pub []byte) {
	m.mu.Lock()
	m.keys[name] = append([]byte(nil), pub...)
	m.mu.Unlock()
}

// keyFor returns the key pinned for a name, or nil.
func (m *memPins) keyFor(name string) []byte {
	m.mu.Lock()
	defer m.mu.Unlock()

	return append([]byte(nil), m.keys[name]...)
}

func testFingerprint(pub []byte) string {
	sum := sha256.Sum256(pub)

	return hex.EncodeToString(sum[:8])
}

func testIdentity(t *testing.T) *Identity {
	t.Helper()

	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}

	return &Identity{Key: priv}
}

// identityPair is two wrapped ends that have never met.
type identityPair struct {
	a, b *Conn
	wa   *watch
	wb   *watch
}

// newIdentityPair wraps both ends with their own identity and pin store. The
// first wrap records the first hello, the second is where the exchange runs,
// exactly as newPair does for the shared secret.
func newIdentityPair(t *testing.T, idA, idB *Identity, pinsA, pinsB Pins, mode Mode) *identityPair {
	t.Helper()

	sa, sb := newPipe("alice"), newPipe("bob")
	link(sa, sb)

	a, err := New(sa, Config{Identity: idA, Pins: pinsA, Name: "alice", Mode: mode, Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	p := &identityPair{a: a}
	p.wa = observe(a)

	b, err := New(sb, Config{Identity: idB, Pins: pinsB, Name: "bob", Mode: mode, Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping bob: %v", err)
	}

	p.b = b
	p.wb = observe(b)

	return p
}

// The device-key exchange finishes without a secret on either side, and both
// sides pin the other's key on the way.
func TestIdentityPairPinsBothSides(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)
	pinsAlice, pinsBob := newPins(), newPins()

	p := newIdentityPair(t, idAlice, idBob, pinsAlice, pinsBob, Single)

	for name, c := range map[string]*Conn{"alice": p.a, "bob": p.b} {
		if peers := c.Peers(); len(peers) != 1 {
			t.Fatalf("%s verified %d peers, want 1 (closed: %v)", name, len(peers), c.IsClosed())
		}
	}

	bobAtAlice := p.a.Peers()[0]
	if bobAtAlice.Device != "bob" {
		t.Errorf("alice sees %q, want bob", bobAtAlice.Device)
	}

	if want := testFingerprint(idBob.Public()); bobAtAlice.Fingerprint != want {
		t.Errorf("alice pinned %q, want %q", bobAtAlice.Fingerprint, want)
	}

	if !bobAtAlice.First {
		t.Error("alice did not report this as the first contact")
	}

	aliceAtBob := p.b.Peers()[0]
	if want := testFingerprint(idAlice.Public()); aliceAtBob.Fingerprint != want {
		t.Errorf("bob pinned %q, want %q", aliceAtBob.Fingerprint, want)
	}

	if !aliceAtBob.First {
		t.Error("bob did not report this as the first contact")
	}

	if err := p.a.Send([]byte("morning, bob")); err != nil {
		t.Fatalf("alice sends: %v", err)
	}

	if got := p.wb.received(); len(got) != 1 || string(got[0]) != "morning, bob" {
		t.Fatalf("bob received %q, want the message", got)
	}
}

// The second session with the same keys is the same two devices, not two new
// ones: the pins are found, and neither side claims a first contact.
func TestIdentitySecondContactIsNotNew(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)
	pinsAlice, pinsBob := newPins(), newPins()

	first := newIdentityPair(t, idAlice, idBob, pinsAlice, pinsBob, Single)
	defer first.a.Close("done")
	defer first.b.Close("done")

	second := newIdentityPair(t, idAlice, idBob, pinsAlice, pinsBob, Single)

	if peers := second.a.Peers(); len(peers) != 1 || peers[0].First {
		t.Fatalf("alice's second contact = %+v, want one peer, not first", peers)
	}

	if peers := second.b.Peers(); len(peers) != 1 || peers[0].First {
		t.Fatalf("bob's second contact = %+v, want one peer, not first", peers)
	}
}

// A name that is already pinned to another key is refused, not re-pinned: that
// is the whole point of pinning.
func TestIdentityRefusesAChangedKeyUnderAPinnedName(t *testing.T) {
	idAlice := testIdentity(t)
	pinsAlice := newPins()
	pinsAlice.pin("bob", testIdentity(t).Public())

	p := newIdentityPair(t, idAlice, testIdentity(t), pinsAlice, newPins(), Single)

	if !p.a.IsClosed() {
		t.Fatal("alice kept a session with a device that came back with a new key")
	}

	if refused := p.wa.refused(); len(refused) == 0 || !errors.Is(refused[0], ErrPin) {
		t.Fatalf("refusal = %v, want ErrPin", refused)
	}
}

// The identity is only as good as the signature: a hello that names a key it
// cannot prove it holds is refused.
func TestIdentityRefusesAForgedSignature(t *testing.T) {
	idAlice := testIdentity(t)
	idBob := testIdentity(t)

	pins := newPins()
	alice, err := New(newPipe("alice"), Config{Identity: idAlice, Pins: pins, Name: "alice", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	w := observe(alice)

	bob := handshake{
		V:      VersionIdentity,
		Step:   stepHello,
		Key:    strangerKeys(t, 1)[0],
		Nonce:  bytes.Repeat([]byte{0x21}, HandshakeNonce),
		Device: "bob",
		Id:     idBob.Public(),
		Sig:    ed25519.Sign(idBob.Key, []byte("not the hello")),
	}

	alice.receiveHello(bob)

	if !alice.IsClosed() {
		t.Fatal("alice kept a session with a peer that could not prove its key")
	}

	if refused := w.refused(); len(refused) == 0 || !errors.Is(refused[0], ErrIdentity) {
		t.Fatalf("refusal = %v, want ErrIdentity", refused)
	}
}

// A hello that is never answered pins nothing. A stranger can name any device
// and sign it with a key of its own, so the pin belongs to the exchange that
// proved the key, not to the first frame that claims it — otherwise one hello
// would let anyone take a name and lock its real owner out.
func TestIdentityHelloAlonePinsNothing(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)
	pins := newPins()

	alice, err := New(newPipe("alice"), Config{Identity: idAlice, Pins: pins, Name: "alice", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	w := observe(alice)

	alice.receiveHello(identityHello(t, idBob, "bob", strangerKeys(t, 1)[0], bytes.Repeat([]byte{0x71}, HandshakeNonce)))

	if alice.IsClosed() {
		t.Fatalf("alice closed on a valid hello: %v", w.refused())
	}

	if peers := alice.Peers(); len(peers) != 0 {
		t.Errorf("an unanswered hello was reported as a peer: %+v", peers)
	}

	if got := pins.keyFor("bob"); got != nil {
		t.Errorf("an unanswered hello was pinned as %x", got)
	}
}

// A confirm whose MAC does not match pins nothing either: the pin is the
// fingerprint the user will compare, so it belongs to a peer that proved the
// key, not to one that merely repeated the frame.
func TestIdentityBadConfirmPinsNothing(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)
	pins := newPins()

	alice, err := New(newPipe("alice"), Config{Identity: idAlice, Pins: pins, Name: "alice", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	w := observe(alice)

	hello := identityHello(t, idBob, "bob", strangerKeys(t, 1)[0], bytes.Repeat([]byte{0x72}, HandshakeNonce))
	confirm := confirmFor(t, alice, idBob, hello)
	confirm.MAC[0] ^= 0xff

	alice.receiveHello(hello)
	alice.receiveConfirm(confirm)

	if !alice.IsClosed() {
		t.Fatal("alice kept a session with a peer whose confirm did not match")
	}

	if refused := w.refused(); len(refused) == 0 || !errors.Is(refused[0], ErrMAC) {
		t.Fatalf("refusal = %v, want ErrMAC", refused)
	}

	if got := pins.keyFor("bob"); got != nil {
		t.Errorf("a confirm with a bad MAC pinned %x", got)
	}
}

// A confirm that answers under a different name than the hello introduced is
// refused: the name that is pinned and shown must be the one the transcript
// authenticated, or the display would credit a key to the wrong device.
func TestIdentityRefusesAConfirmThatRenamesThePeer(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)
	pins := newPins()

	alice, err := New(newPipe("alice"), Config{Identity: idAlice, Pins: pins, Name: "alice", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	w := observe(alice)

	hello := identityHello(t, idBob, "bob", strangerKeys(t, 1)[0], bytes.Repeat([]byte{0x73}, HandshakeNonce))

	renamed := hello
	renamed.Device = "mallory"
	confirm := confirmFor(t, alice, idBob, renamed)

	alice.receiveHello(hello)
	alice.receiveConfirm(confirm)

	if !alice.IsClosed() {
		t.Fatal("alice accepted a confirm that renamed the peer")
	}

	if refused := w.refused(); len(refused) == 0 || !errors.Is(refused[0], ErrMAC) {
		t.Fatalf("refusal = %v, want ErrMAC", refused)
	}

	if got := pins.keyFor("bob"); got != nil {
		t.Errorf("a renamed confirm pinned bob as %x", got)
	}

	if got := pins.keyFor("mallory"); got != nil {
		t.Errorf("a renamed confirm pinned mallory as %x", got)
	}
}

// A pin that cannot be recorded refuses the peer: the trust would be memory
// only, and the next run would ask the user to compare the device again.
func TestIdentityRefusesAPeerWhosePinCannotBeRecorded(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)
	pins := newPins()
	pins.reject = errors.New("the disk is full")

	alice, err := New(newPipe("alice"), Config{Identity: idAlice, Pins: pins, Name: "alice", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	w := observe(alice)

	hello := identityHello(t, idBob, "bob", strangerKeys(t, 1)[0], bytes.Repeat([]byte{0x74}, HandshakeNonce))

	alice.receiveHello(hello)
	alice.receiveConfirm(confirmFor(t, alice, idBob, hello))

	if !alice.IsClosed() {
		t.Fatal("alice accepted a peer whose pin could not be written")
	}

	if refused := w.refused(); len(refused) == 0 || !errors.Is(refused[0], ErrPin) {
		t.Fatalf("refusal = %v, want ErrPin", refused)
	}

	if peers := alice.Peers(); len(peers) != 0 {
		t.Errorf("a peer with no pin was accepted: %+v", peers)
	}
}

// The terminal prints this device's own fingerprint beside the peer's, because
// the comparison at first contact needs both values on one screen.
func TestConnFingerprintIsTheDeviceKey(t *testing.T) {
	idAlice := testIdentity(t)

	alice, err := New(newPipe("alice"), Config{Identity: idAlice, Pins: newPins(), Name: "alice", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	if want := discovery.Fingerprint(idAlice.Public()); alice.Fingerprint() != want {
		t.Errorf("alice's fingerprint = %q, want %q", alice.Fingerprint(), want)
	}

	// A shared-secret session has no per-device identity to show.
	bob, err := New(newPipe("bob"), Config{Secret: secret, Name: "bob", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping bob: %v", err)
	}

	if got := bob.Fingerprint(); got != "" {
		t.Errorf("a shared-secret session reported the fingerprint %q", got)
	}
}

// identityHello is a valid device-key hello from id, for the ephemeral key and
// nonce given.
func identityHello(t *testing.T, id *Identity, name string, key, nonce []byte) handshake {
	t.Helper()

	h := handshake{V: VersionIdentity, Step: stepHello, Key: key, Nonce: nonce, Device: name, Id: id.Public()}
	h.Sig = ed25519.Sign(id.Key, helloClaim(h))

	return h
}

// confirmFor is the confirm id sends for h, computed the way that peer would
// compute it: the MAC over both identities, addressed to alice. It lets a test
// drive receiveConfirm without a second Conn.
func confirmFor(t *testing.T, alice *Conn, id *Identity, h handshake) handshake {
	t.Helper()

	pub, err := ecdh.X25519().NewPublicKey(h.Key)
	if err != nil {
		t.Fatal(err)
	}

	shared, err := alice.priv.ECDH(pub)
	if err != nil {
		t.Fatal(err)
	}

	key, err := deriveConfirmKey(shared, handshakeSalt(alice.ours(), h), alice.ours(), h)
	if err != nil {
		t.Fatal(err)
	}

	out := h
	out.Step = stepConfirm
	out.To = alice.key

	// The signature covers the name, so a confirm answering under another one
	// is signed again — otherwise the test would exercise the signature check
	// rather than the name binding.
	out.Sig = ed25519.Sign(id.Key, helloClaim(out))
	out.MAC = confirmMAC(key, alice.transcriptFor(out), alice.key)

	return out
}

// A confirm is enough on its own in the device-key version: it repeats the
// sender's identity and signature, so a peer that joined a bus after the hello
// was broadcast can still authenticate the answer and pin the device.
func TestIdentityAcceptsAConfirmThatCarriesItsIdentity(t *testing.T) {
	idAlice, idBob := testIdentity(t), testIdentity(t)
	pinsAlice := newPins()

	alice, err := New(newPipe("alice"), Config{
		Identity: idAlice,
		Pins:     pinsAlice,
		Name:     "alice",
		Timeout:  time.Hour,
	})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	w := observe(alice)

	// Bob's confirm as a peer that never saw alice's hello would send it: his
	// hello fields, his identity and signature, addressed to alice.
	bob := handshake{
		V:      VersionIdentity,
		Step:   stepConfirm,
		Key:    strangerKeys(t, 1)[0],
		Nonce:  bytes.Repeat([]byte{0x31}, HandshakeNonce),
		Device: "bob",
		To:     alice.key,
	}
	bob.Id = idBob.Public()
	bob.Sig = ed25519.Sign(idBob.Key, helloClaim(bob))

	// The MAC is the one a real bob computes, so only the accept-and-pin path
	// is under test here.
	pub, err := ecdh.X25519().NewPublicKey(bob.Key)
	if err != nil {
		t.Fatal(err)
	}

	shared, err := alice.priv.ECDH(pub)
	if err != nil {
		t.Fatal(err)
	}

	confirmKey, err := deriveConfirmKey(shared, handshakeSalt(alice.ours(), bob), alice.ours(), bob)
	if err != nil {
		t.Fatal(err)
	}

	bob.MAC = confirmMAC(confirmKey, alice.transcriptFor(bob), alice.key)

	alice.receiveConfirm(bob)

	if alice.IsClosed() {
		t.Fatalf("alice closed instead of accepting the confirm: %v", w.refused())
	}

	peers := alice.Peers()
	if len(peers) != 1 || peers[0].Device != "bob" {
		t.Fatalf("alice's peers = %+v, want bob", peers)
	}

	if want := testFingerprint(idBob.Public()); peers[0].Fingerprint != want {
		t.Errorf("alice pinned %q, want %q", peers[0].Fingerprint, want)
	}

	if !peers[0].First {
		t.Error("the confirm alone was not a first contact")
	}

	if got := pinsAlice.keyFor("bob"); !bytes.Equal(got, idBob.Public()) {
		t.Errorf("alice pinned %x for bob, want his key", got)
	}

	// The answer goes back, so bob can verify alice in turn.
	if len(w.received()) != 0 {
		t.Error("a handshake step reached the consumer as a message")
	}
}

// A device-key peer and a shared-secret peer are two versions: a hello of the
// other one is refused, not downgraded into the one this side speaks.
func TestIdentityAndSecretPeersDoNotMix(t *testing.T) {
	sa, sb := newPipe("alice"), newPipe("bob")
	link(sa, sb)

	alice, err := New(sa, Config{Identity: testIdentity(t), Pins: newPins(), Name: "alice", Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	observe(alice)

	if _, err := New(sb, Config{Secret: secret, Name: "bob", Timeout: time.Hour}); err != nil {
		t.Fatalf("wrapping bob: %v", err)
	}

	if !alice.IsClosed() {
		t.Fatal("a shared-secret peer was accepted by a device-key session")
	}
}

// A device-key bus pins each peer the first time it speaks, and a peer that
// comes back with a different key is dropped without ending the room: on a bus
// one stranger must not be able to silence everyone.
func TestIdentityBusPinsEachPeerAndKeepsTheRoom(t *testing.T) {
	idAlice, idBob, idMallory := testIdentity(t), testIdentity(t), testIdentity(t)

	pinsAlice, pinsBob := newPins(), newPins()

	sa, sb := newPipe("alice"), newPipe("bob")
	link(sa, sb)

	alice, err := New(sa, Config{Identity: idAlice, Pins: pinsAlice, Name: "alice", Mode: Bus, Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping alice: %v", err)
	}

	wa := observe(alice)

	bob, err := New(sb, Config{Identity: idBob, Pins: pinsBob, Name: "bob", Mode: Bus, Timeout: time.Hour})
	if err != nil {
		t.Fatalf("wrapping bob: %v", err)
	}

	wb := observe(bob)

	// The first exchange pinned each side's long-term key under its name.
	if got := pinsAlice.keyFor("bob"); !bytes.Equal(got, idBob.Public()) {
		t.Fatalf("alice pinned %x for bob, want his key", got)
	}

	if got := pinsBob.keyFor("alice"); !bytes.Equal(got, idAlice.Public()) {
		t.Fatalf("bob pinned %x for alice, want her key", got)
	}

	// Mallory claims to be bob with a key of her own. Alice has bob pinned, so
	// the hello is refused before any traffic is accepted.
	mallory := handshake{
		V:      VersionIdentity,
		Step:   stepHello,
		Key:    strangerKeys(t, 1)[0],
		Nonce:  bytes.Repeat([]byte{0x51}, HandshakeNonce),
		Device: "bob",
		Id:     idMallory.Public(),
	}
	mallory.Sig = ed25519.Sign(idMallory.Key, helloClaim(mallory))

	alice.receiveHello(mallory)

	if alice.IsClosed() {
		t.Fatal("a bus closed the room because one peer presented a changed key")
	}

	if refused := wa.refused(); len(refused) == 0 || !errors.Is(refused[0], ErrPin) {
		t.Fatalf("alice refused with %v, want ErrPin", refused)
	}

	if peers := alice.Peers(); len(peers) != 1 || peers[0].Device != "bob" {
		t.Fatalf("alice's peers after the refusal = %+v, want only the real bob", peers)
	}

	// The pin was not overwritten: bob is still the key alice met first.
	if got := pinsAlice.keyFor("bob"); !bytes.Equal(got, idBob.Public()) {
		t.Fatalf("alice re-pinned bob to %x", got)
	}

	if err := alice.Send([]byte("still here, bob")); err != nil {
		t.Fatalf("alice sends in a room that refused a stranger: %v", err)
	}

	if got := wb.received(); len(got) != 1 || string(got[0]) != "still here, bob" {
		t.Fatalf("bob received %q, want the message", got)
	}
}

// The configuration is one credential or the other, and each has to be usable.
func TestCredentialIsExactlyOneUsableCredential(t *testing.T) {
	id := testIdentity(t)

	tests := []struct {
		name    string
		cfg     Config
		wantErr error
	}{
		{"no credential", Config{Name: "alice"}, ErrSecret},
		{"short secret", Config{Secret: []byte("short")}, ErrSecret},
		{"both", Config{Secret: secret, Identity: id, Pins: newPins()}, ErrSecret},
		{"identity without pins", Config{Identity: id}, ErrIdentity},
		{"short identity", Config{Identity: &Identity{Key: []byte("short")}, Pins: newPins()}, ErrIdentity},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := New(newPipe("alice"), tt.cfg)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("New(%s) = %v, want %v", tt.name, err, tt.wantErr)
			}
		})
	}
}

// The device-key transcript covers both identities: two peers that agree on
// every ephemeral field but not on who they are do not compute the same bytes.
func TestIdentityTranscriptCoversBothIdentities(t *testing.T) {
	a, b := hellos()
	a.V, b.V = VersionIdentity, VersionIdentity
	a.Id, b.Id = testIdentity(t).Public(), testIdentity(t).Public()

	if !bytes.Equal(transcriptIdentity(a, b), transcriptIdentity(b, a)) {
		t.Fatal("the two sides compute different device-key transcripts")
	}

	other := b
	other.Id = testIdentity(t).Public()

	if bytes.Equal(transcriptIdentity(a, b), transcriptIdentity(a, other)) {
		t.Fatal("a different device key produced the same transcript")
	}
}

// The encoding carries the identity fields on every device-key step, and decode
// refuses a payload that claims one version while carrying the other's fields,
// or a device-key step that carries no identity at all.
func TestDecodeHandshakeIdentityFields(t *testing.T) {
	id := testIdentity(t)
	key := strangerKeys(t, 1)[0]
	nonce := bytes.Repeat([]byte{0x41}, HandshakeNonce)

	valid := handshake{V: VersionIdentity, Step: stepHello, Key: key, Nonce: nonce, Device: "bob",
		Id: id.Public(), Sig: ed25519.Sign(id.Key, []byte("anything"))}

	payload, err := encodeHandshake(valid)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	parsed, err := wire.Parse(payload)
	if err != nil {
		t.Fatalf("parsing the frame: %v", err)
	}

	if _, err := decodeHandshake(parsed.Payload, VersionIdentity); err != nil {
		t.Fatalf("decoding a valid device-key hello: %v", err)
	}

	// A confirm carries the identity too, because on a bus it is often the only
	// thing a peer that joined late ever sees.
	confirm := handshake{V: VersionIdentity, Step: stepConfirm, Key: key, Nonce: nonce,
		To: key, MAC: bytes.Repeat([]byte{0x01}, 32), Id: id.Public(),
		Sig: ed25519.Sign(id.Key, []byte("anything"))}

	out, err := encodeHandshake(confirm)
	if err != nil {
		t.Fatalf("encoding: %v", err)
	}

	frame, err := wire.Parse(out)
	if err != nil {
		t.Fatalf("parsing the frame: %v", err)
	}

	if _, err := decodeHandshake(frame.Payload, VersionIdentity); err != nil {
		t.Fatalf("decoding a valid device-key confirm: %v", err)
	}

	tests := []struct {
		name string
		h    handshake
	}{
		{"hello without an identity", handshake{V: VersionIdentity, Step: stepHello, Key: key, Nonce: nonce}},
		{"hello without a signature", handshake{V: VersionIdentity, Step: stepHello, Key: key, Nonce: nonce, Id: id.Public()}},
		{"v1 hello with an identity", handshake{V: Version, Step: stepHello, Key: key, Nonce: nonce, Id: id.Public()}},
		{"confirm without an identity", handshake{V: VersionIdentity, Step: stepConfirm, Key: key, Nonce: nonce,
			To: key, MAC: bytes.Repeat([]byte{0x01}, 32)}},
		{"v1 confirm with an identity", handshake{V: Version, Step: stepConfirm, Key: key, Nonce: nonce,
			To: key, MAC: bytes.Repeat([]byte{0x01}, 32), Id: id.Public()}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out, err := encodeHandshake(tt.h)
			if err != nil {
				t.Fatalf("encoding: %v", err)
			}

			frame, err := wire.Parse(out)
			if err != nil {
				t.Fatalf("parsing the frame: %v", err)
			}

			if _, err := decodeHandshake(frame.Payload, tt.h.V); !errors.Is(err, ErrHandshake) {
				t.Fatalf("decode = %v, want ErrHandshake", err)
			}
		})
	}
}

// A device name is a label on a terminal and in the app, and the peer chooses
// it: a control character could forge a line or move the cursor. It is refused
// at decode, where the other side's text first enters, while an ordinary name
// with a non-ASCII letter is still a name.
func TestDecodeHandshakeRefusesAControlCharacterInTheDeviceName(t *testing.T) {
	id := testIdentity(t)
	key := strangerKeys(t, 1)[0]
	nonce := bytes.Repeat([]byte{0x62}, HandshakeNonce)

	decode := func(t *testing.T, device string) error {
		t.Helper()

		h := handshake{V: VersionIdentity, Step: stepHello, Key: key, Nonce: nonce, Device: device, Id: id.Public()}
		h.Sig = ed25519.Sign(id.Key, helloClaim(h))

		out, err := encodeHandshake(h)
		if err != nil {
			t.Fatalf("encoding: %v", err)
		}

		frame, err := wire.Parse(out)
		if err != nil {
			t.Fatalf("parsing the frame: %v", err)
		}

		_, err = decodeHandshake(frame.Payload, VersionIdentity)

		return err
	}

	tests := []struct {
		name    string
		device  string
		wantErr bool
	}{
		{"a plain name", "bob", false},
		{"a name with a letter outside ASCII", "böb", false},
		{"a carriage return", "bob\rmallory: hello", true},
		{"an escape sequence", "\x1b[2Jbob", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := decode(t, tt.device)

			if tt.wantErr {
				if !errors.Is(err, ErrHandshake) {
					t.Fatalf("decode(%q) = %v, want ErrHandshake", tt.device, err)
				}

				return
			}

			if err != nil {
				t.Fatalf("decode(%q) = %v, want no error", tt.device, err)
			}
		})
	}
}
