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
// payload is sealed. It does not give each device its own identity either: the
// shared secret is the identity, so three devices holding it are three
// identities with one name, and there is no revocation.
//
// The secret is now a key. An eavesdropper who recorded the ciphertext and
// later learned the secret could read what they kept, so it belongs in the same
// category as the token: never a flag, never a screenshot, never a log.
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
	"crypto/rand"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"

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
type Config struct {
	// Secret is the shared secret. It is the identity of every device that
	// holds it, and nothing else is needed to be one.
	Secret []byte

	// Name is this device's name. It is part of the MAC transcript, so a peer
	// cannot rename itself mid-handshake, and it is what the peer shows.
	Name string

	// Mode is Single (the zero value) or Bus.
	Mode Mode

	// Timeout is how long a single-peer session waits for the confirm. Zero
	// means DefaultTimeout; it is ignored in Bus mode.
	Timeout time.Duration
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
)

// peer is one correspondent: what it told us about itself and the two ciphers
// derived from the exchange.
type peer struct {
	device string
	key    []byte
	tag    []byte
	send   cipher.AEAD
	recv   cipher.AEAD

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

	priv  *ecdh.PrivateKey
	key   []byte
	tag   []byte
	nonce []byte

	mu      sync.Mutex
	peers   map[string]*peer
	order   []string
	timer   *time.Timer
	onError func(error)
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

	if len(cfg.Secret) < MinSecret {
		return nil, fmt.Errorf("%w: %d bytes of secret, at least %d are needed",
			ErrSecret, len(cfg.Secret), MinSecret)
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
		Base:   session.NewBase(s.ID(), s.RemoteAddr()),
		inner:  s,
		secret: append([]byte(nil), cfg.Secret...),
		name:   cfg.Name,
		mode:   cfg.Mode,
		priv:   priv,
		key:    priv.PublicKey().Bytes(),
		nonce:  nonce,
		peers:  make(map[string]*peer),
	}
	c.tag = recipientTag(c.key)

	s.OnClose(func(reason string) { c.shutdown(reason) })

	// The hello goes out before the callback is installed, and that order is
	// deliberate: it is the peer's hello that makes this side answer, so a
	// callback installed afterwards cannot miss a reply to our own frame. A
	// frame that somehow arrives first is not lost either — a confirm for a
	// peer this side has not heard from is treated as one that carries its own
	// hello.
	if err := c.sendHandshake(handshake{
		V:      Version,
		Step:   stepHello,
		Key:    c.key,
		Nonce:  c.nonce,
		Device: c.name,
	}); err != nil {
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

// Peer is a correspondent that has completed the handshake.
type Peer struct {
	Device string
	Key    []byte
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

		out = append(out, Peer{Device: p.device, Key: append([]byte(nil), p.key...)})
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
func (c *Conn) Send(payload []byte) error {
	if c.IsClosed() {
		return session.ErrClosed
	}

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

	c.NotifyMessage(session.Message{From: m.From, Payload: plain, Time: m.Time})
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

	h, err := decodeHandshake(frame.Payload)
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
	_, fresh, err := c.record(h)
	if err != nil {
		c.fail(err, "")

		return
	}

	if !fresh {
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
// hello before its own arrived — the two frames crossed. It carries the same
// hello fields, and the MAC covers them, so it is recorded and answered once,
// exactly as a hello would have been. Holding it back instead would deadlock a
// peer whose hello was lost on the way.
func (c *Conn) receiveConfirm(h handshake) {
	if subtle.ConstantTimeCompare(h.To, c.key) != 1 {
		return
	}

	p, fresh, err := c.record(h)
	if err != nil {
		c.fail(err, "")

		return
	}

	c.mu.Lock()
	verified := p.verified
	c.mu.Unlock()

	if verified {
		return
	}

	want := confirmMAC(c.secret, transcript(c.ours(), h), h.To)
	if subtle.ConstantTimeCompare(want, h.MAC) != 1 {
		c.fail(fmt.Errorf("%w: %q did not prove it holds the secret", ErrMAC, peerName(h.Device)), string(h.Key))

		return
	}

	c.markVerified(p)

	if fresh {
		if err := c.sendConfirm(h); err != nil {
			c.fail(err, "")
		}
	}
}

// sendConfirm answers a peer's hello (or a confirm that arrived in its place).
// The MAC is addressed to that peer, so it is useless to anyone else.
func (c *Conn) sendConfirm(to handshake) error {
	return c.sendHandshake(handshake{
		V:      Version,
		Step:   stepConfirm,
		Key:    c.key,
		Nonce:  c.nonce,
		Device: c.name,
		To:     to.Key,
		MAC:    confirmMAC(c.secret, transcript(c.ours(), to), to.Key),
	})
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
	return handshake{V: Version, Step: stepHello, Key: c.key, Nonce: c.nonce, Device: c.name}
}

// record adds a peer the first time its key is seen and derives both keys from
// the exchange. The second return value says whether this was the first time.
func (c *Conn) record(h handshake) (*peer, bool, error) {
	key := string(h.Key)

	c.mu.Lock()
	defer c.mu.Unlock()

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
		device:  h.Device,
		key:     append([]byte(nil), h.Key...),
		tag:     recipientTag(h.Key),
		send:    send,
		recv:    recv,
		sendSeq: firstSequence,
		expect:  firstSequence,
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

// markVerified accepts a peer's traffic and stops waiting for the handshake.
func (c *Conn) markVerified(p *peer) {
	c.mu.Lock()

	p.verified = true

	if c.mode == Single && c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}

	c.mu.Unlock()
}

// handshakeTimedOut closes a single-peer session whose handshake never
// finished. It runs on its own goroutine, so it re-checks the state: a confirm
// that arrived in the meantime wins.
func (c *Conn) handshakeTimedOut(timeout time.Duration) {
	if c.IsClosed() {
		return
	}

	c.mu.Lock()
	verified := c.verifiedLocked() > 0
	c.mu.Unlock()

	if verified {
		return
	}

	c.fail(fmt.Errorf("%w: no encrypted handshake within %s", ErrHandshake, timeout), "")
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
