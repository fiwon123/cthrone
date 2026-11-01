// Package e2e seals what two cthrone peers exchange, so the thing in the middle
// — a NATS broker, a websocket host that is not your correspondent, anyone
// recording the wire — carries ciphertext it cannot read.
//
// It is a wrapper over session.Session, not over a transport: the handshake has
// to travel as ordinary messages before any user traffic can, and both the
// terminal loops and the GUI already attach through that one interface. Nothing
// here imports a transport or a GUI, so it is testable against an in-memory
// pair.
//
//	peer A                         peer B
//	  hello  ──────────────────────▶
//	         ◀────────────────────── hello
//	  confirm ─────────────────────▶   (MAC over both hellos, addressed to B)
//	         ◀────────────────────── confirm
//	  sealed ──────────────────────▶   (AES-256-GCM, sequence number as AAD)
//
// # What it does not do
//
// It does not hide that a conversation happened. Frame sizes, timing and the
// subject or address are all still visible to whatever carries them; only the
// payload is sealed.
//
// # Two credentials
//
// A shared secret keeps the original model: every device that holds it is the
// same identity, which is fine for a pair and weak for a set. It is what a
// configured secret speaks, so an older peer still meets a newer one on the
// secret.
//
// With no secret configured a session speaks the device-key model instead: the
// hello carries this device's public key and its signature, the peer pins the
// key on first contact, and a key that changes under a pinned name is refused
// rather than learned. Forgetting a pin is the way to revoke that trust.
//
// The secret is a key. An eavesdropper who recorded the ciphertext and
// later learned the secret could read what they kept, so it belongs in the same
// category as the token: never a flag, never a screenshot, never a log. A
// device key is the same kind of material without the sharing.
//
// # There is no plaintext mode
//
// A Conn always handshakes, and there is no option that turns it off. A peer
// that cannot produce the confirm MAC is refused — never downgraded to
// plaintext — which is what makes "it is encrypted" mean something. Send fails
// until a peer has completed the handshake; nothing typed in the meantime goes
// out in the clear.
package e2e

import (
	"crypto/cipher"
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fiwon123/cthrone/internal/discovery"
	"github.com/fiwon123/cthrone/internal/session"
	"github.com/fiwon123/cthrone/internal/wire"
)

// Protocol names the scheme and its version. It is the first thing in every
// MAC transcript and every key derivation, so a value computed for one version
// is meaningless to another.
const Protocol = "cthrone-e2e/v1"

// DefaultTimeout is how long a single-peer session waits for the handshake to
// finish before it closes. A bus does not use it: a peer that has not spoken on
// a shared subject is not the same as one that stalled, and one stranger must
// not be able to end a room.
const DefaultTimeout = 15 * time.Second

// DefaultConfirmTimeout is how long a pending first-contact confirmation may
// hold a single-peer session open. It is longer than DefaultTimeout because the
// person answering has to read a code off two screens, and because the side
// that is waiting for an answer must not time out while the other side's human
// is still deciding.
const DefaultConfirmTimeout = 60 * time.Second

// MinSecret is the shortest secret this package will derive keys from. The token
// the tool generates is 32 random bytes; a short secret makes the confirm MAC —
// the only thing standing between a stranger and the session — brute-forceable.
const MinSecret = 16

// maxPeers bounds how many peers one Conn will track. A bus is a room, not a
// crowd, and the bound is what keeps a peer from filling the map.
const maxPeers = 64

// Mode says how many peers the session underneath has.
type Mode int

const (
	// Single is one correspondent: a websocket connection. A refusal is fatal,
	// because there is no one else to talk to, and a handshake that does not
	// finish closes the session.
	Single Mode = iota

	// Bus is a shared subject with several peers: NATS. A refusal names the
	// peer and drops it, so one stranger cannot take down the room.
	Bus
)

// Config is what a Conn needs besides the session.
//
// There are two credentials and exactly one is used. A secret is the original
// model — one key shared by everyone who may talk — and it keeps the v1
// handshake. An Identity with a Pins store is the per-device model: each device
// signs its hello with its own key and the peer pins it on first contact, and
// that is what a session with no configured secret runs.
type Config struct {
	// Secret is the shared secret. It is the identity of every device that
	// holds it, and nothing else is needed to be one. Leave it empty to use
	// Identity instead.
	Secret []byte

	// Identity is this device's long-term signing key. It must come with
	// Pins: without somewhere to record a first contact there is no point at
	// which a returning device could be recognized.
	Identity *Identity

	// Pins decides whether a peer's identity is accepted and records it on
	// first contact. It is required with Identity and ignored without it.
	Pins Pins

	// Name is this device's name. It is part of the MAC transcript, so a peer
	// cannot rename itself mid-handshake, and it is what the peer shows. It is
	// also what the pin store keys a device by.
	Name string

	// Mode is Single (the zero value) or Bus.
	Mode Mode

	// Timeout is how long a single-peer session waits for the confirm. Zero
	// means DefaultTimeout; it is ignored in Bus mode.
	Timeout time.Duration

	// Confirm decides whether a device met for the first time may join. It is
	// asked at most once per peer, only on a single-peer session, only in the
	// device-key model, and only after the peer has proved it holds the key it
	// presented — never for a device that is already pinned.
	//
	// While the answer is pending this side withholds its confirm, so the peer
	// is not authenticated and its session is not attached until the answer is
	// yes. The question is put on a goroutine of its own, so a caller may block
	// on a terminal or a dialog without stopping the transport.
	//
	// A nil Confirm accepts the peer and pins it, which is what every caller
	// did before there was a code to compare.
	Confirm func(FirstContact) bool

	// ConfirmTimeout bounds how long a pending confirmation may hold a session
	// open. Zero means DefaultConfirmTimeout. It replaces Timeout while the
	// question is pending: a person reading a code aloud needs longer than a
	// stalled handshake is allowed.
	ConfirmTimeout time.Duration
}

// FirstContact is a device met for the first time: what the other screen shows,
// and the short code both sides derived without sending it.
type FirstContact struct {
	Name        string
	Fingerprint string
	Code        string
}

// Errors. A message says what happened; the error says what kind of wrong it
// was, so a caller can refuse without reading English.
var (
	// ErrSecret is a secret this package will not derive keys from.
	ErrSecret = errors.New("e2e: unusable shared secret")

	// ErrHandshake is a handshake step that is malformed, of another version,
	// or from a peer that cannot be in the exchange.
	ErrHandshake = errors.New("e2e: handshake refused")

	// ErrMAC is a confirm that did not authenticate: the peer does not hold
	// the shared secret, or it is not the peer the confirm was addressed to.
	ErrMAC = errors.New("e2e: handshake MAC mismatch")

	// ErrPlaintext is a payload that arrived unsealed. The peer is not
	// encrypting, and its bytes are refused rather than shown.
	ErrPlaintext = errors.New("e2e: unencrypted payload")

	// ErrSeal is a sealed payload that is malformed before any key is tried.
	ErrSeal = errors.New("e2e: malformed sealed payload")

	// ErrAuth is a frame no peer could open, or one that failed to
	// authenticate.
	ErrAuth = errors.New("e2e: frame did not authenticate")

	// ErrSequence is a frame whose sequence number is not the one due: a
	// replay, a dropped frame or a reordering.
	ErrSequence = errors.New("e2e: frame out of sequence")

	// ErrPeer is a peer this Conn will not track: the room is full.
	ErrPeer = errors.New("e2e: peer refused")

	// ErrConfirm is a first contact that nobody answered: the person at the
	// other end refused it, or the question ran out of time. Nothing is pinned
	// in either case.
	ErrConfirm = errors.New("e2e: first contact not confirmed")
)

// peer is one correspondent: what it told us about itself and the two ciphers
// derived from the exchange.
type peer struct {
	device string
	key    []byte
	nonce  []byte
	tag    []byte
	send   cipher.AEAD
	recv   cipher.AEAD

	// confirm is the key this peer's confirm MAC is computed with: the shared
	// secret in the v1 handshake, the pairwise derived key in v2.
	confirm []byte

	// id is the peer's long-term device key in the v2 handshake, empty in v1.
	// fingerprint and first are what the pin store said when it was seen.
	id          []byte
	fingerprint string
	first       bool

	// step is the last handshake step this peer sent, kept because the answer
	// to it may be withheld until a person confirms the contact.
	step handshake

	// code is the six-digit comparison value for this contact, empty in the
	// shared-secret model and on a bus. announced records that the contact was
	// reported to the caller, and asks are the confirmation in flight:
	// asking is set while the question is pending, and oweConfirm records that
	// this side still has to send its confirm once the answer is yes.
	code       string
	announced  bool
	asking     bool
	oweConfirm bool

	// sendSeq is the next sequence number used to seal a payload for this
	// peer; expect is the one the next payload from it must carry.
	sendSeq uint64
	expect  uint64

	// verified is set once this peer produced the confirm MAC. Until then its
	// sealed payloads are refused.
	verified bool
}

// Conn is a session.Session that seals everything it carries.
//
// It takes the callbacks of the session it wraps: the transport delivers to
// Conn, and Conn delivers to whoever called OnMessage on it. So a caller swaps
// its session for this one and keeps the loop it already has.
type Conn struct {
	*session.Base

	inner  session.Session
	secret []byte
	name   string
	mode   Mode

	// version is the handshake this session speaks, and identity and pins are
	// the device-key model's half of it. They are empty in a secret session
	// and never change after New.
	version  int
	identity *Identity
	pins     Pins

	priv  *ecdh.PrivateKey
	key   []byte
	tag   []byte
	nonce []byte

	// confirm is the human gate for a first contact, nil when this side has
	// nobody to ask. confirmTimeout bounds the question, and extended records
	// that the waiting side already used the one confirmation window it gets.
	confirm        func(FirstContact) bool
	confirmTimeout time.Duration
	extended       bool

	// onFirst and firsts are the first-contact reports: a contact recorded
	// before the callback was installed is replayed, the way OnPeer replays a
	// peer that finished early.
	onFirst func(FirstContact)
	firsts  []FirstContact

	mu      sync.Mutex
	peers   map[string]*peer
	order   []string
	timer   *time.Timer
	onError func(error)
	onPeer  func(Peer)

	// sendMu makes one payload's whole send atomic: the sequence number is
	// assigned to the frame before the frame is written, so two sends that
	// overlapped would be two numbers that can reach the wire in the other
	// order. It covers sealing *and* the write for that reason, and is held
	// across a transport write — a slow peer delays the next send rather than
	// letting it jump ahead of this one.
	sendMu sync.Mutex
}

// credential decides which handshake the session speaks and checks that the
// configured credential can actually run it.
//
// The two credentials are alternatives, not layers: a session that carries a
// secret speaks v1, so an older peer still meets it on the secret, and a
// session with no secret speaks v2 with this device's key. A session with
// neither is an error rather than one that talks in the clear.
func credential(cfg Config) (int, error) {
	hasSecret := len(cfg.Secret) > 0
	hasIdentity := cfg.Identity != nil

	if hasSecret && hasIdentity {
		return 0, fmt.Errorf("%w: a secret and a device key are two identities, configure one", ErrSecret)
	}

	if hasSecret {
		if len(cfg.Secret) < MinSecret {
			return 0, fmt.Errorf("%w: %d bytes of secret, at least %d are needed",
				ErrSecret, len(cfg.Secret), MinSecret)
		}

		return Version, nil
	}

	if !hasIdentity {
		return 0, fmt.Errorf("%w: no shared secret and no device key; set a secret to pair the old way, or leave it unset to pair with a device key", ErrSecret)
	}

	if len(cfg.Identity.Key) != ed25519.PrivateKeySize {
		return 0, fmt.Errorf("%w: the device key is %d bytes, want %d", ErrIdentity, len(cfg.Identity.Key), ed25519.PrivateKeySize)
	}

	if cfg.Pins == nil {
		return 0, fmt.Errorf("%w: a device key needs somewhere to pin peers", ErrIdentity)
	}

	return VersionIdentity, nil
}

// New wraps s and starts the handshake.
//
// The first thing it does is send this side's hello, so a session that is
// already open starts speaking the moment it is wrapped; the second is to take
// over the session's message callback. Nothing else is exchanged in the clear.
func New(s session.Session, cfg Config) (*Conn, error) {
	if s == nil {
		return nil, fmt.Errorf("%w: no session to wrap", ErrHandshake)
	}

	version, err := credential(cfg)
	if err != nil {
		return nil, err
	}

	priv, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("%w: generating this side's key: %v", ErrHandshake, err)
	}

	nonce := make([]byte, HandshakeNonce)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("%w: generating this side's nonce: %v", ErrHandshake, err)
	}

	c := &Conn{
		Base:           session.NewBase(s.ID(), s.RemoteAddr()),
		inner:          s,
		secret:         append([]byte(nil), cfg.Secret...),
		name:           cfg.Name,
		mode:           cfg.Mode,
		version:        version,
		identity:       cfg.Identity,
		pins:           cfg.Pins,
		confirm:        cfg.Confirm,
		confirmTimeout: cfg.ConfirmTimeout,
		priv:           priv,
		key:            priv.PublicKey().Bytes(),
		nonce:          nonce,
		peers:          make(map[string]*peer),
	}
	c.tag = recipientTag(c.key)

	s.OnClose(func(reason string) { c.shutdown(reason) })

	// The hello goes out before the callback is installed, and that order is
	// harmless because a transport session holds a frame that arrives before
	// its OnMessage handler is set (session.Base) and replays it on install.
	// So a peer's hello that crosses this side's own hello is still delivered,
	// and a confirm for a peer this side has not heard from is treated as one
	// that carries its own hello.
	if err := c.sendHello(); err != nil {
		return nil, err
	}

	s.OnMessage(c.receive)

	if c.mode == Single {
		timeout := cfg.Timeout
		if timeout <= 0 {
			timeout = DefaultTimeout
		}

		// Only while the handshake is still outstanding: a peer that answered
		// from the buffer above has already stopped the clock that never
		// started.
		c.mu.Lock()
		if c.verifiedLocked() == 0 {
			c.timer = time.AfterFunc(timeout, func() { c.handshakeTimedOut(timeout) })
		}
		c.mu.Unlock()
	}

	return c, nil
}

// OnError reports a refusal that did not end the session: on a bus, the peer
// that was dropped and why. A single-peer refusal closes the session instead,
// and the reason arrives through OnClose.
func (c *Conn) OnError(fn func(error)) {
	c.mu.Lock()
	c.onError = fn
	c.mu.Unlock()
}

// OnPeer reports every peer that has completed the handshake: the ones already
// verified when it is installed, and each new one as it authenticates, once per
// peer. It is how a caller says "encrypted with X" without polling Peers and
// guessing when to look.
//
// The replay matters because a fast peer can answer before the caller has had
// the wrapper back — installing the callback and reading the list are two steps,
// and a peer that finished in between would be seen by neither.
func (c *Conn) OnPeer(fn func(Peer)) {
	c.mu.Lock()
	c.onPeer = fn

	known := make([]Peer, 0, len(c.order))

	for _, key := range c.order {
		p := c.peers[key]
		if p == nil || !p.verified {
			continue
		}

		known = append(known, Peer{Device: p.device, Key: append([]byte(nil), p.key...), Fingerprint: p.fingerprint, First: p.first})
	}
	c.mu.Unlock()

	// Outside the lock, for the same reason markVerified reports outside it: a
	// callback that sends must not deadlock on the mutex.
	if fn == nil {
		return
	}

	for _, p := range known {
		fn(p)
	}
}

// OnFirstContact reports a device met for the first time, with the code the two
// screens compare.
//
// It fires on both sides of a first contact, because the screen that is not
// asked anything is the one that has to show what it is waiting for. It fires
// as soon as the peer's step is recorded — before the exchange has proved
// anything — so a caller can display the code while the other side decides;
// nothing may be trusted on it beyond the fact that a peer said a name.
//
// A contact recorded before the callback was installed is replayed, for the
// same reason OnPeer replays one that finished early.
func (c *Conn) OnFirstContact(fn func(FirstContact)) {
	c.mu.Lock()
	c.onFirst = fn
	known := append([]FirstContact(nil), c.firsts...)
	c.mu.Unlock()

	for _, contact := range known {
		fn(contact)
	}
}

// announceFirst reports a first contact once per peer, and records it for a
// callback that arrives later. It is called with the lock released: the caller
// may open a dialog.
//
// It is a single-peer report: on a bus there is no code to compare and no one
// person to ask, so nothing is announced and every first contact is pinned the
// way it always was.
func (c *Conn) announceFirst(p *peer) {
	if c.mode != Single {
		return
	}

	c.mu.Lock()

	if p.announced || !p.first {
		c.mu.Unlock()

		return
	}

	p.announced = true
	contact := FirstContact{Name: p.device, Fingerprint: p.fingerprint, Code: p.code}
	c.firsts = append(c.firsts, contact)
	handler := c.onFirst
	c.mu.Unlock()

	if handler != nil {
		handler(contact)
	}
}

// Peer is a correspondent that has completed the handshake.
type Peer struct {
	Device string
	Key    []byte

	// Fingerprint is the short, comparable form of the peer's device key in a
	// device-key session. It is empty in a shared-secret session, which has no
	// per-device identity to show.
	Fingerprint string

	// First is set when this was the first contact: the key had not been seen
	// before and the pin store has just recorded it. It is the moment to ask
	// the user to compare the fingerprint with the other screen.
	First bool
}

// Name is what to call the peer when its device name may be empty.
func (p Peer) Name() string { return peerName(p.Device) }

// Fingerprint is this device's own fingerprint in the device-key model: the
// value the other screen shows for it. It is empty in a shared-secret session,
// which has no per-device identity to show.
//
// It is here because the comparison at first contact needs both values on one
// screen: the peer's fingerprint arrives with the notice, and this is what the
// user compares it with.
func (c *Conn) Fingerprint() string {
	if c.identity == nil {
		return ""
	}

	pub := c.identity.Public()
	if len(pub) != ed25519.PublicKeySize {
		return ""
	}

	return discovery.Fingerprint(pub)
}

// Peers lists the peers whose confirm arrived, in the order they were heard.
func (c *Conn) Peers() []Peer {
	c.mu.Lock()
	defer c.mu.Unlock()

	out := make([]Peer, 0, len(c.order))

	for _, key := range c.order {
		p := c.peers[key]
		if p == nil || !p.verified {
			continue
		}

		out = append(out, Peer{Device: p.device, Key: append([]byte(nil), p.key...), Fingerprint: p.fingerprint, First: p.first})
	}

	return out
}

// Send seals payload for every verified peer and writes it.
//
// One payload becomes one frame per peer and not one frame for all of them: the
// keys are pairwise, so a frame sealed for one peer is unreadable — and rightly
// refused — by the others. On a websocket that is a single write; on a bus it
// is one write per peer, all to the same subject.
//
// Before the handshake finishes there is nowhere safe to send it, so Send fails
// rather than falling back to plaintext.
//
// One send is one critical section, sealing and writing together. The sequence
// number belongs to the wire order — the receiver opens each frame at the
// number it expects and refuses anything else — so two senders that took 1 and
// 2 and then wrote 2 first would be refusing themselves. Two callers do overlap
// in practice: the GUI sends typed text from one goroutine while the file
// transfer pumps chunks from another (issue #192).
func (c *Conn) Send(payload []byte) error {
	if c.IsClosed() {
		return session.ErrClosed
	}

	c.sendMu.Lock()
	defer c.sendMu.Unlock()

	frames, err := c.sealForEveryPeer(payload)
	if err != nil {
		return err
	}

	for _, frame := range frames {
		if err := c.inner.Send(frame); err != nil {
			return err
		}
	}

	return nil
}

// sealForEveryPeer turns payload into one sealed frame per verified peer.
func (c *Conn) sealForEveryPeer(payload []byte) ([][]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.verifiedLocked() == 0 {
		return nil, fmt.Errorf("%w: no peer has completed the handshake yet", ErrHandshake)
	}

	frames := make([][]byte, 0, len(c.order))

	for _, key := range c.order {
		p := c.peers[key]
		if p == nil || !p.verified {
			continue
		}

		seq := p.sendSeq
		p.sendSeq++

		frame, err := seal(p.send, p.tag, seq, payload)
		if err != nil {
			return nil, err
		}

		frames = append(frames, frame)
	}

	return frames, nil
}

// Close ends the session, cancelling the handshake deadline first so a session
// that is going away does not report a timeout on its way out.
func (c *Conn) Close(reason string) {
	c.stopTimer()
	c.inner.Close(reason)
	c.NotifyClose(reason)
}

// receive is the session callback: it is where every frame the transport
// delivers is classified.
func (c *Conn) receive(m session.Message) {
	if c.IsClosed() {
		return
	}

	if wire.IsFrame(m.Payload) {
		c.receiveFrame(m)

		return
	}

	if len(m.Payload) == 0 || m.Payload[0] != SealVersion {
		c.fail(fmt.Errorf("%w: the peer is not encrypting", ErrPlaintext), "")

		return
	}

	head, err := sealedHeader(m.Payload)
	if err != nil {
		c.fail(err, "")

		return
	}

	// A frame marked for another peer is not ours to read, and on a bus it is
	// an ordinary thing to see: every message goes out once per peer to the
	// same subject. It is skipped, not refused, so one peer's traffic cannot
	// look like an attack to the rest of the room.
	if subtle.ConstantTimeCompare(head.tag, c.tag) != 1 {
		return
	}

	plain, key, err := c.openFrom(head, m.Payload)
	if err != nil {
		c.fail(err, key)

		return
	}

	// From is the name that authenticated the frame; Addr is the transport's
	// own idea of where it came from, carried beside the name rather than
	// replaced by it. A caller that shows only one of them loses either who
	// the peer is or which machine it is.
	c.NotifyMessage(session.Message{From: c.senderName(key, m.From), Addr: m.Addr, Payload: plain, Time: m.Time})
}

// senderName is what to call the peer a frame came from. The transport's From
// is the subject or the address — the same for every peer on a bus — so the
// device name that authenticated the handshake is a better answer when the
// peer announced one. The fallback keeps an unnamed peer identifiable.
func (c *Conn) senderName(key, fallback string) string {
	c.mu.Lock()
	p := c.peers[key]
	device := ""
	if p != nil {
		device = p.device
	}
	c.mu.Unlock()

	if device != "" {
		return device
	}

	return fallback
}

// receiveFrame handles a payload that carries the wire magic: either a
// handshake step, or a frame that should have been sealed and was not.
func (c *Conn) receiveFrame(m session.Message) {
	frame, err := wire.Parse(m.Payload)
	if err != nil {
		c.fail(fmt.Errorf("%w: %v", ErrPlaintext, err), "")

		return
	}

	if frame.Header.Kind != wire.KindHandshake {
		c.fail(fmt.Errorf("%w: a %q frame arrived unsealed", ErrPlaintext, frame.Header.Kind), "")

		return
	}

	h, err := decodeHandshake(frame.Payload, c.version)
	if err != nil {
		c.fail(err, "")

		return
	}

	switch h.Step {
	case stepHello:
		c.receiveHello(h)

	case stepConfirm:
		c.receiveConfirm(h)
	}
}

// receiveHello records a peer and answers with this side's confirm. A peer that
// is already known is ignored: a second hello is not a rekey, and it is not a
// second chance at a wrong secret.
func (c *Conn) receiveHello(h handshake) {
	p, fresh, err := c.record(h)
	if err != nil {
		c.fail(err, "")

		return
	}

	c.announceFirst(p)

	if !fresh {
		return
	}

	// A first contact this side has to confirm gets no answer yet: the peer is
	// not authenticated until a person has accepted it, and this side's confirm
	// is the answer that would authenticate it. So it is owed, not sent — the
	// question is put once the peer's own confirm has proved it holds the keys
	// it presented, and a frame nobody proved never opens a dialog.
	if c.awaiting(p) {
		c.oweConfirm(p)

		return
	}

	if err := c.sendConfirm(h); err != nil {
		c.fail(err, "")

		return
	}
}

// receiveConfirm checks the peer's proof, and only then is its traffic allowed
// through.
//
// A confirm addressed to another peer is not this side's to check and is
// skipped: on a bus every confirm reaches every subscriber, and treating the
// rest as forgeries would let ordinary traffic end a room.
//
// A confirm for a peer this side has not heard from is a peer that answered our
// hello before its own arrived — the two frames crossed, or on a bus the peer's
// hello was broadcast before this side subscribed. In the device-key version it
// carries the identity and signature its hello would have, so it is recorded,
// authenticated and pinned exactly as a hello is, and answered once. In the
// shared-secret version the MAC is proof enough. Holding it back instead would
// leave a peer that joined a moment late unable to pair.
func (c *Conn) receiveConfirm(h handshake) {
	if subtle.ConstantTimeCompare(h.To, c.key) != 1 {
		return
	}

	p, fresh, err := c.record(h)
	if err != nil {
		c.fail(err, string(h.Key))

		return
	}

	c.mu.Lock()
	verified := p.verified
	c.mu.Unlock()

	if verified {
		return
	}

	c.announceFirst(p)

	// The transcript is rebuilt from what this side recorded — the identity,
	// the name and the nonce — not from the fields of the step that just
	// arrived. A confirm that repeats a different name under the same ephemeral
	// key therefore produces a MAC that does not match, and the name that is
	// pinned and displayed stays the one the hello introduced.
	theirs := h
	if c.identity != nil {
		theirs.Id = p.id
		theirs.Device = p.device
		theirs.Nonce = p.nonce
	}

	want := confirmMAC(p.confirm, c.transcriptFor(theirs), h.To)
	if subtle.ConstantTimeCompare(want, h.MAC) != 1 {
		proof := "the secret"
		if c.identity != nil {
			proof = "its device key"
		}

		// The peer is left recorded rather than dropped: its ephemeral key is
		// on the wire in the clear, so anyone could name it in a frame with a
		// bad MAC, and dropping it would let a stranger silence a peer that is
		// still working. It is unverified either way, so it can send nothing.
		c.fail(fmt.Errorf("%w: %q did not prove it holds %s", ErrMAC, peerName(h.Device), proof), "")

		return
	}

	// The peer has now proved it holds the key it presented, which is the
	// moment a person can be asked about it: a name and a code from a frame
	// nobody proved would be a dialog a stranger could open at will.
	if c.awaiting(p) {
		c.askFirstContact(p, fresh)

		return
	}

	if err := c.markVerified(p); err != nil {
		c.fail(err, string(p.key))

		return
	}

	if fresh {
		if err := c.sendConfirm(h); err != nil {
			c.fail(err, "")
		}
	}
}

// sendConfirm answers a peer's hello (or a confirm that arrived in its place).
// The MAC is addressed to that peer, so it is useless to anyone else. In the
// device-key version it repeats this side's identity and signature, so a peer
// that never saw the hello can still authenticate the answer.
func (c *Conn) sendConfirm(to handshake) error {
	p := c.peerFor(string(to.Key))
	if p == nil {
		return fmt.Errorf("%w: no peer %q to answer", ErrHandshake, peerName(to.Device))
	}

	theirs := to
	if c.identity != nil {
		theirs.Id = p.id
		theirs.Device = p.device
		theirs.Nonce = p.nonce
	}

	confirm := handshake{
		V:      c.version,
		Step:   stepConfirm,
		Key:    c.key,
		Nonce:  c.nonce,
		Device: c.name,
		To:     to.Key,
		MAC:    confirmMAC(p.confirm, c.transcriptFor(theirs), to.Key),
	}

	if c.identity != nil {
		confirm.Id = c.identity.Public()
		confirm.Sig = ed25519.Sign(c.identity.Key, helloClaim(confirm))
	}

	return c.sendHandshake(confirm)
}

// sendHello opens the exchange. A device-key session signs it, which is what
// ties the ephemeral exchange to the long-term identity the peer pins.
func (c *Conn) sendHello() error {
	h := c.ours()

	if c.identity != nil {
		h.Sig = ed25519.Sign(c.identity.Key, helloClaim(h))
	}

	return c.sendHandshake(h)
}

// sendHandshake writes one step into the session. It is called without the lock
// held, because a transport may deliver the answer synchronously.
func (c *Conn) sendHandshake(h handshake) error {
	frame, err := encodeHandshake(h)
	if err != nil {
		return err
	}

	return c.inner.Send(frame)
}

// ours is this side's hello fields, which every transcript includes.
func (c *Conn) ours() handshake {
	h := handshake{V: c.version, Step: stepHello, Key: c.key, Nonce: c.nonce, Device: c.name}

	if c.identity != nil {
		h.Id = c.identity.Public()
	}

	return h
}

// transcriptFor is the version's transcript: the shared-secret one, or the
// device-key one, which also covers both identities. theirs must carry the
// identity this side recorded for the peer, which is the one the pin belongs
// to; the callers copy it in, so a later step cannot substitute another.
func (c *Conn) transcriptFor(theirs handshake) []byte {
	ours := c.ours()

	if c.identity == nil {
		return transcript(ours, theirs)
	}

	return transcriptIdentity(ours, theirs)
}

// authenticate checks a peer's device identity.
//
// A shared-secret session has nothing to check here: the confirm MAC is the
// proof, and it is checked when it arrives. A device-key session's hello is
// itself the proof — the signature over the claim says the peer holds the
// private half of the key it names — and the pin store says whether that key is
// the one it had last time.
//
// Nothing is pinned here. What arrives at this point could be a hello that is
// never answered, so the pin is recorded by markVerified, once the exchange has
// completed.
func (c *Conn) authenticate(h handshake) (string, bool, error) {
	if c.identity == nil {
		return "", false, nil
	}

	if len(h.Id) != ed25519.PublicKeySize || len(h.Sig) != ed25519.SignatureSize {
		return "", false, fmt.Errorf("%w: %q presented no device key", ErrIdentity, peerName(h.Device))
	}

	if !ed25519.Verify(ed25519.PublicKey(h.Id), helloClaim(h), h.Sig) {
		return "", false, fmt.Errorf("%w: %q did not prove it holds its device key", ErrIdentity, peerName(h.Device))
	}

	fingerprint, first, err := c.pins.Check(h.Id, h.Device)
	if err != nil {
		return "", false, fmt.Errorf("%w: %w", ErrPin, err)
	}

	return fingerprint, first, nil
}

// peerFor returns the peer for an ephemeral key, or nil when it is not one this
// side has recorded.
func (c *Conn) peerFor(key string) *peer {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.peers[key]
}

// record adds a peer the first time its key is seen and derives both keys from
// the exchange. The second return value says whether this was the first time.
func (c *Conn) record(h handshake) (*peer, bool, error) {
	key := string(h.Key)

	c.mu.Lock()
	if p := c.peers[key]; p != nil {
		c.mu.Unlock()

		return p, false, nil
	}
	c.mu.Unlock()

	// The identity is checked outside the lock: verifying a signature and
	// asking the pin store both cost time, and neither needs the peer map.
	fingerprint, first, err := c.authenticate(h)
	if err != nil {
		return nil, false, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// A peer that spoke while this side was checking is already recorded.
	if p := c.peers[key]; p != nil {
		return p, false, nil
	}

	if len(c.peers) >= maxPeers && !c.evictUnverifiedLocked() {
		return nil, false, fmt.Errorf("%w: %d peers is the limit", ErrPeer, maxPeers)
	}

	pub, err := ecdh.X25519().NewPublicKey(h.Key)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %q has no usable key: %v", ErrHandshake, h.Device, err)
	}

	shared, err := c.priv.ECDH(pub)
	if err != nil {
		return nil, false, fmt.Errorf("%w: %q's key is not a point to exchange with: %v", ErrHandshake, h.Device, err)
	}

	send, recv, err := deriveKeys(shared, c.ours(), h)
	if err != nil {
		return nil, false, err
	}

	p := &peer{
		device:      h.Device,
		key:         append([]byte(nil), h.Key...),
		nonce:       append([]byte(nil), h.Nonce...),
		tag:         recipientTag(h.Key),
		send:        send,
		recv:        recv,
		id:          append([]byte(nil), h.Id...),
		fingerprint: fingerprint,
		first:       first,
		step:        h,
		sendSeq:     firstSequence,
		expect:      firstSequence,
	}

	if c.identity != nil {
		p.confirm, err = deriveConfirmKey(shared, handshakeSalt(c.ours(), h), c.ours(), h)
		if err != nil {
			return nil, false, err
		}

		// A code is only meaningful between two devices that compared one, and
		// that is a single correspondent: on a bus the room is the audience,
		// nobody can read several codes off one screen, and the pin-without-
		// asking path is what keeps one stranger from holding the room.
		if c.mode == Single {
			p.code, err = confirmCode(c.ours(), h)
			if err != nil {
				return nil, false, err
			}
		}
	} else {
		p.confirm = c.secret
	}

	c.peers[key] = p
	c.order = append(c.order, key)

	return p, true, nil
}

// openFrom finds the peer a sealed payload belongs to and opens it.
//
// The recipient mark has already said the frame is for this side; which peer
// sent it is decided by the bytes themselves, because the keys are pairwise and
// the tag of exactly one peer opens them. There is no sender field in the clear
// to look up, and there does not need to be: the same bytes that protect the
// payload are what authenticate the sender, and a frame that cannot be opened
// is refused rather than shown.
func (c *Conn) openFrom(head sealedFrame, data []byte) ([]byte, string, error) {
	seq := head.seq

	c.mu.Lock()
	defer c.mu.Unlock()

	due := false

	for _, key := range c.order {
		p := c.peers[key]
		if p == nil || !p.verified || p.expect != seq {
			continue
		}

		due = true

		plain, _, err := open(p.recv, data)
		if err != nil {
			continue
		}

		p.expect++

		return plain, key, nil
	}

	if due {
		return nil, "", fmt.Errorf("%w: a peer expected frame %d and could not open it", ErrAuth, seq)
	}

	return nil, "", fmt.Errorf("%w: frame %d is not the one due from any peer", ErrSequence, seq)
}

// fail applies the rule for a refusal. A single correspondent means there is
// nothing left to talk to, so the session closes with the reason; a bus drops
// the peer that is out of line and keeps the room.
func (c *Conn) fail(err error, key string) {
	c.mu.Lock()

	if key != "" {
		c.dropLocked(key)
	}

	handler := c.onError
	c.mu.Unlock()

	if handler != nil {
		handler(err)
	}

	if c.mode == Single {
		c.Close(err.Error())
	}
}

// markVerified accepts a peer's traffic and, on a device-key session, records
// its pin. The pin is written before the peer is accepted: a store that cannot
// be written is a reason to refuse, not something to carry in memory until the
// process ends.
//
// A shared-secret session has nothing to pin — every device holding the secret
// is the same identity — so it goes straight to accepting the peer.
func (c *Conn) markVerified(p *peer) error {
	c.mu.Lock()
	id, name := p.id, p.device
	c.mu.Unlock()

	if c.identity != nil {
		// Outside the lock: writing the pin store is disk I/O, and a callback
		// that sends must not deadlock on the mutex.
		if err := c.pins.Pin(id, name); err != nil {
			return fmt.Errorf("%w: %w", ErrPin, err)
		}
	}

	c.mu.Lock()

	p.verified = true

	if c.mode == Single && c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}

	handler := c.onPeer
	info := Peer{Device: p.device, Key: append([]byte(nil), p.key...), Fingerprint: p.fingerprint, First: p.first}

	c.mu.Unlock()

	// Outside the lock: a caller that sends in the callback would deadlock on
	// it, and the peer is recorded either way.
	if handler != nil {
		handler(info)
	}

	return nil
}

// handshakeTimedOut closes a single-peer session whose handshake never
// finished. It runs on its own goroutine, so it re-checks the state: a confirm
// that arrived in the meantime wins.
func (c *Conn) handshakeTimedOut(timeout time.Duration) {
	if c.IsClosed() {
		return
	}

	c.mu.Lock()

	if c.verifiedLocked() > 0 {
		c.mu.Unlock()

		return
	}

	// The side that is not asking may be waiting on the other side's person: a
	// first contact is recorded, nothing is verified, and the confirm that
	// would finish the exchange is being held back until that person answers.
	// The deadline is extended once for the confirmation window, so the waiting
	// side does not close a session the other side is about to accept. A peer
	// that is merely silent still runs out.
	if c.identity != nil && c.mode == Single && !c.extended && c.pendingFirstLocked() {
		c.extended = true
		timeout = c.confirmWindowLocked()
		c.timer = time.AfterFunc(timeout, func() { c.handshakeTimedOut(timeout) })
		c.mu.Unlock()

		return
	}

	c.mu.Unlock()

	c.fail(fmt.Errorf("%w: no encrypted handshake within %s", ErrHandshake, timeout), "")
}

// awaiting reports whether this peer's admission needs a person's answer: a
// device-key session on a single connection, for a name that has not been
// pinned before, with somebody to ask.
func (c *Conn) awaiting(p *peer) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.awaitingLocked(p)
}

func (c *Conn) awaitingLocked(p *peer) bool {
	return c.confirm != nil && c.identity != nil && c.mode == Single && p.first && !p.verified
}

// pendingFirstLocked reports a first contact recorded here and not yet proved,
// with no question of this side's own in flight: the shape of a session that is
// waiting for the other side to answer.
func (c *Conn) pendingFirstLocked() bool {
	for _, p := range c.peers {
		if p.first && !p.verified && !p.asking {
			return true
		}
	}

	return false
}

// oweConfirm records that this peer is still waiting for the confirm this side
// withheld, so accepting the contact sends it.
func (c *Conn) oweConfirm(p *peer) {
	c.mu.Lock()
	p.oweConfirm = true
	c.mu.Unlock()
}

// claimAsk marks a peer as pending an answer and reports whether this call is
// the one that asks. oweConfirm records that the answer owes the peer its
// confirm. A second step from the same peer — a hello, then a confirm — joins
// the question already pending instead of opening a second one.
func (c *Conn) claimAsk(p *peer, oweConfirm bool) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	p.oweConfirm = p.oweConfirm || oweConfirm

	if p.asking {
		return false
	}

	p.asking = true

	return true
}

// askFirstContact puts the first-contact question to the caller and puts the
// answer to work without holding up the transport: the question runs on its own
// goroutine, and this side's confirm is withheld until the answer is yes.
func (c *Conn) askFirstContact(p *peer, oweConfirm bool) {
	if !c.claimAsk(p, oweConfirm) {
		return
	}

	c.mu.Lock()

	contact := FirstContact{Name: p.device, Fingerprint: p.fingerprint, Code: p.code}
	confirm := c.confirm
	timeout := c.confirmWindowLocked()

	// The handshake deadline is the wrong clock for a person reading a code
	// aloud: the question gets a bound of its own, and a question that is never
	// answered is refused rather than left half open.
	if c.timer != nil {
		c.timer.Stop()
	}

	c.timer = time.AfterFunc(timeout, func() { c.confirmTimedOut(p, timeout) })
	c.mu.Unlock()

	go func() {
		if confirm != nil && confirm(contact) {
			c.acceptFirstContact(p)

			return
		}

		c.refuseFirstContact(p, contact)
	}()
}

// resolveAsk settles a pending question and reports whether this call owns the
// outcome: a timeout that lost the race to the answer does nothing, and an
// answer that lost the race to the timeout does nothing either.
func (c *Conn) resolveAsk(p *peer) bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !p.asking {
		return false
	}

	p.asking = false

	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}

	return true
}

// acceptFirstContact records the pin and answers the confirm that was held
// back. It runs on the goroutine the question was put on.
func (c *Conn) acceptFirstContact(p *peer) {
	if c.IsClosed() || !c.resolveAsk(p) {
		return
	}

	if err := c.markVerified(p); err != nil {
		c.fail(err, string(p.key))

		return
	}

	c.mu.Lock()
	owe := p.oweConfirm
	p.oweConfirm = false
	step := p.step
	c.mu.Unlock()

	if !owe {
		return
	}

	if err := c.sendConfirm(step); err != nil {
		c.fail(err, string(p.key))
	}
}

// refuseFirstContact drops a peer nobody accepted. Nothing is pinned and
// nothing is answered; the refusal travels the way every other refusal does,
// so a single-peer session closes and a host's listener stays up for the next
// device.
func (c *Conn) refuseFirstContact(p *peer, contact FirstContact) {
	if !c.resolveAsk(p) {
		return
	}

	c.fail(fmt.Errorf("%w: %q was not confirmed (code %s)", ErrConfirm, peerName(contact.Name), contact.Code), string(p.key))
}

// confirmTimedOut refuses a contact whose question ran out of time.
func (c *Conn) confirmTimedOut(p *peer, timeout time.Duration) {
	if c.IsClosed() || !c.resolveAsk(p) {
		return
	}

	c.fail(fmt.Errorf("%w: %q was not confirmed within %s", ErrConfirm, peerName(p.device), timeout), string(p.key))
}

// confirmWindowLocked is how long a pending question may hold the session
// open.
func (c *Conn) confirmWindowLocked() time.Duration {
	if c.confirmTimeout > 0 {
		return c.confirmTimeout
	}

	return DefaultConfirmTimeout
}

// shutdown is the wrapped session's close: it reaches the caller through this
// Conn's own OnClose, once.
func (c *Conn) shutdown(reason string) {
	c.stopTimer()
	c.NotifyClose(reason)
}

// stopTimer cancels the handshake deadline, if one is still running.
func (c *Conn) stopTimer() {
	c.mu.Lock()
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.mu.Unlock()
}

// dropLocked forgets a peer, so nothing more is sent to it or accepted from it.
func (c *Conn) dropLocked(key string) {
	if c.peers[key] == nil {
		return
	}

	delete(c.peers, key)

	for i, k := range c.order {
		if k == key {
			c.order = append(c.order[:i], c.order[i+1:]...)

			break
		}
	}
}

// evictUnverifiedLocked makes room for one more peer by dropping the oldest one
// that never completed the handshake. A peer that only says hello and then goes
// quiet must not be able to fill the room and keep the people in it out; a peer
// that is dropped this way is recorded again the moment it speaks.
func (c *Conn) evictUnverifiedLocked() bool {
	for _, key := range c.order {
		if p := c.peers[key]; p != nil && !p.verified {
			c.dropLocked(key)

			return true
		}
	}

	return false
}

// verifiedLocked counts the peers whose handshake completed.
func (c *Conn) verifiedLocked() int {
	n := 0

	for _, p := range c.peers {
		if p.verified {
			n++
		}
	}

	return n
}

// peerName is what to call a peer in a message when its name may be empty.
func peerName(device string) string {
	if device == "" {
		return "an unnamed peer"
	}

	return device
}
