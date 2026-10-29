package discovery

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrIdentityChanged is a peer whose name is already pinned to another key.
// It is refused rather than re-pinned: a device that comes back with a new key
// is a new device, and accepting it silently would throw the pin away.
var ErrIdentityChanged = errors.New("discovery: the device name is pinned to another key")

// ErrCertificateChanged is an address whose certificate is already pinned to
// another fingerprint. It is refused for the same reason a device key is: a
// certificate that changed is either a reinstalled host or somebody else, and
// only the user can tell which. Forget is the way back.
var ErrCertificateChanged = errors.New("discovery: the address is pinned to another certificate")

// PinnedPeer is a device identity that has been seen and pinned: the answer to
// "is this the same device as last time?".
type PinnedPeer struct {
	Name        string    `json:"name"`
	Key         string    `json:"key"`
	Fingerprint string    `json:"fingerprint"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// PinnedCert is a host certificate that has been seen and pinned at an
// address: the answer to "is this the same host as last time?" for a
// transport with no certificate authority to ask. The address is the
// host:port the client dialed, because that is all the transport knows
// before the certificates are exchanged.
type PinnedCert struct {
	Address     string    `json:"address"`
	Fingerprint string    `json:"fingerprint"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// pinFile is the on-disk form. The version leaves room for a future change
// that has to read older files differently.
type pinFile struct {
	Version int                   `json:"version"`
	Peers   map[string]PinnedPeer `json:"peers"`
	Certs   map[string]PinnedCert `json:"certs,omitempty"`
}

// PinStore is the set of devices and host certificates this device trusts on
// first contact. It is the trust-on-first-use record: the first key seen for
// a name, and the first certificate seen at an address, are pinned, and every
// later contact has to present the same one.
type PinStore struct {
	path  string
	mu    sync.Mutex
	peers map[string]PinnedPeer
	certs map[string]PinnedCert
}

// DefaultPinsFile returns the file the pins are kept in: beside the device key
// and the other device state.
func DefaultPinsFile() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}

	return filepath.Join(dir, "peers.json"), nil
}

// LoadPins returns the pins kept in the default file.
func LoadPins() (*PinStore, error) {
	path, err := DefaultPinsFile()
	if err != nil {
		return nil, err
	}

	return NewPinStore(path)
}

// NewPinStore returns the pins kept in path. A file that does not exist yet is
// an empty store, not an error: nothing has been pinned on the first run.
func NewPinStore(path string) (*PinStore, error) {
	s := &PinStore{
		path:  path,
		peers: make(map[string]PinnedPeer),
		certs: make(map[string]PinnedCert),
	}

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return s, nil
		}

		return nil, fmt.Errorf("reading the pins %s: %w", path, err)
	}

	var file pinFile
	if err := json.Unmarshal(content, &file); err != nil {
		return nil, fmt.Errorf("parsing the pins %s: %w", path, err)
	}

	if file.Peers != nil {
		s.peers = file.Peers
	}

	if file.Certs != nil {
		s.certs = file.Certs
	}

	return s, nil
}

// Check answers whether a peer's key is trusted for its name.
//
// The identity is the key, so a known key under a known name is accepted
// without a question. A known name with a different key is refused: that is
// what pinning means, and the user clears it with Forget. A name that is new —
// or a peer that has none, which is keyed by its fingerprint — is reported as
// first, so the caller can ask the user to compare the fingerprint with the
// other screen.
//
// Nothing is written here, and that is deliberate: a hello can name any device
// and sign it with a key of its own, so a pin recorded from one would let
// anyone who can reach the port or the subject claim a name and lock its owner
// out. The pin belongs to Pin, which is called only after the exchange proved
// the peer holds the key.
func (s *PinStore) Check(pub []byte, name string) (fingerprint string, first bool, err error) {
	fingerprint, key, id, err := pinOf(pub, name)
	if err != nil {
		return "", false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pinned, ok := s.peers[id]
	if !ok {
		return fingerprint, true, nil
	}

	if pinned.Key != key {
		return fingerprint, false, fmt.Errorf("%w: %q", ErrIdentityChanged, displayName(pinned, name))
	}

	return fingerprint, false, nil
}

// Pin records a peer whose handshake has completed: the first contact creates
// the entry, and every later one refreshes the time it was last seen.
//
// A write that fails is reported rather than kept in memory alone: the caller
// refuses the peer, because a pin the next run cannot read is a peer the user
// would be asked to compare again.
func (s *PinStore) Pin(pub []byte, name string) error {
	_, key, id, err := pinOf(pub, name)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	if pinned, ok := s.peers[id]; ok {
		if pinned.Key != key {
			return fmt.Errorf("%w: %q", ErrIdentityChanged, displayName(pinned, name))
		}

		pinned.LastSeen = now
		if name != "" {
			pinned.Name = name
		}
		s.peers[id] = pinned

		return s.save()
	}

	s.peers[id] = PinnedPeer{
		Name:        name,
		Key:         key,
		Fingerprint: Fingerprint(pub),
		FirstSeen:   now,
		LastSeen:    now,
	}

	if err := s.save(); err != nil {
		delete(s.peers, id)

		return err
	}

	return nil
}

// CheckCert answers whether the certificate seen at address is the one pinned
// there. first reports that nothing is pinned yet: the caller records it then,
// because the first contact is the trust decision.
//
// A fingerprint that does not match the pin is refused, the way a different
// device key under a pinned name is. Nothing is written here.
func (s *PinStore) CheckCert(address, fingerprint string) (first bool, err error) {
	address = normalizeAddress(address)
	if address == "" {
		return false, errors.New("discovery: a certificate pin needs the address it was reached at")
	}

	want := normalizePinID(fingerprint)
	if want == "" {
		return false, errors.New("discovery: a certificate pin needs a fingerprint")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	pinned, ok := s.certs[address]
	if !ok {
		return true, nil
	}

	if normalizePinID(pinned.Fingerprint) != want {
		return false, fmt.Errorf("%w: %s was pinned as %s, this certificate is %s", ErrCertificateChanged, address, pinned.Fingerprint, want)
	}

	return false, nil
}

// PinCert records the certificate a host presented at address: the first
// contact creates the pin, and every later one refreshes when it was last
// seen. A write that fails is reported rather than kept in memory alone, for
// the same reason Pin reports one: a pin the next run cannot read is a host
// the user would be asked to trust again.
func (s *PinStore) PinCert(address, fingerprint string) error {
	address = normalizeAddress(address)
	if address == "" {
		return errors.New("discovery: a certificate pin needs the address it was reached at")
	}

	want := normalizePinID(fingerprint)
	if want == "" {
		return errors.New("discovery: a certificate pin needs a fingerprint")
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()

	if pinned, ok := s.certs[address]; ok {
		if normalizePinID(pinned.Fingerprint) != want {
			return fmt.Errorf("%w: %s was pinned as %s, this certificate is %s", ErrCertificateChanged, address, pinned.Fingerprint, want)
		}

		pinned.LastSeen = now
		s.certs[address] = pinned

		return s.save()
	}

	s.certs[address] = PinnedCert{
		Address:     address,
		Fingerprint: want,
		FirstSeen:   now,
		LastSeen:    now,
	}

	if err := s.save(); err != nil {
		delete(s.certs, address)

		return err
	}

	return nil
}

// Certificates returns the pinned host certificates, ordered by address.
func (s *PinStore) Certificates() []PinnedCert {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]PinnedCert, 0, len(s.certs))
	for _, c := range s.certs {
		out = append(out, c)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Address < out[j].Address })

	return out
}

// pinOf validates a device key and names the entry it belongs to: the device's
// name, or its fingerprint when it has none — an unnamed peer is still an
// identity, and two unnamed devices stay two pins.
func pinOf(pub []byte, name string) (fingerprint, key, id string, err error) {
	if len(pub) != ed25519.PublicKeySize {
		return "", "", "", fmt.Errorf("discovery: a device key is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}

	fingerprint = Fingerprint(pub)
	key = hex.EncodeToString(pub)
	id = name

	if id == "" {
		id = fingerprint
	}

	return fingerprint, key, id, nil
}

// List returns the pinned devices, ordered by name then fingerprint.
func (s *PinStore) List() []PinnedPeer {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := make([]PinnedPeer, 0, len(s.peers))
	for _, p := range s.peers {
		out = append(out, p)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}

		return out[i].Fingerprint < out[j].Fingerprint
	})

	return out
}

// Forget removes a pin by name, fingerprint or address and reports whether
// one was there. It is the way back from a reinstalled device or a host whose
// certificate changed, and from a pairing that was made by mistake. A store
// that cannot be written keeps the pin and reports why: reporting "not found"
// for a failed save would send the user looking for the wrong problem.
func (s *PinStore) Forget(id string) (bool, error) {
	want := normalizePinID(id)

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, p := range s.peers {
		if normalizePinID(key) == want || normalizePinID(p.Name) == want || normalizePinID(p.Fingerprint) == want {
			delete(s.peers, key)

			if err := s.save(); err != nil {
				s.peers[key] = p

				return false, err
			}

			return true, nil
		}
	}

	// An id without a port names every certificate under that host, so all of
	// them go in one write. A user forgets the host they dialed, not the port
	// the tool appended to it, and leaving the other ports pinned would keep
	// refusing a host that was told to forget.
	removed := make(map[string]PinnedCert)

	for key, c := range s.certs {
		if sameAddress(id, key) || normalizePinID(c.Fingerprint) == want {
			removed[key] = c

			delete(s.certs, key)
		}
	}

	if len(removed) == 0 {
		return false, nil
	}

	if err := s.save(); err != nil {
		for key, c := range removed {
			s.certs[key] = c
		}

		return false, err
	}

	return true, nil
}

// save writes the pins in a form a person can read and a future version can
// extend. The file holds public keys only, so it is not a secret — but it is
// still written 0600, because who this device talks to is nobody else's
// business.
//
// The write goes to a temporary file beside the real one and is renamed into
// place, so a crash or a full disk cannot leave half a document where the pins
// were: the next run would fail to parse it, and every command refuses to start
// until someone deleted the file by hand.
func (s *PinStore) save() error {
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", dir, err)
	}

	content, err := json.MarshalIndent(pinFile{Version: 1, Peers: s.peers, Certs: s.certs}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the pins: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".peers-*.json")
	if err != nil {
		return fmt.Errorf("writing the pins: %w", err)
	}

	name := tmp.Name()

	// A successful rename leaves nothing to remove; every other way out does.
	defer os.Remove(name)

	if _, err := tmp.Write(append(content, '\n')); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("writing the pins: %w", err)
	}

	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()

		return fmt.Errorf("writing the pins: %w", err)
	}

	if err := tmp.Close(); err != nil {
		return fmt.Errorf("writing the pins: %w", err)
	}

	if err := os.Rename(name, s.path); err != nil {
		return fmt.Errorf("writing %s: %w", s.path, err)
	}

	return nil
}

// Fingerprint is the human-comparable form of a device key: the first 16 bytes
// of its SHA-256, in groups of four hex characters, so it can be read aloud
// and compared on two screens without counting characters.
func Fingerprint(pub []byte) string {
	sum := sha256.Sum256(pub)
	hexed := hex.EncodeToString(sum[:16])

	var out strings.Builder
	out.Grow(len(hexed) + len(hexed)/4)

	for i := 0; i < len(hexed); i += 4 {
		if i > 0 {
			out.WriteByte('-')
		}

		out.WriteString(hexed[i : i+4])
	}

	return out.String()
}

// displayName is what to call a peer when its identity changed. A peer with no
// name is named by its fingerprint, which is what the user has to compare.
func displayName(pinned PinnedPeer, name string) string {
	if name != "" {
		return name
	}

	return pinned.Fingerprint
}

// normalizePinID makes a name or a fingerprint comparable regardless of the
// separators a person typed or a tool printed.
func normalizePinID(id string) string {
	replacer := strings.NewReplacer("-", "", ":", "", " ", "", "\n", "", "\t", "")

	return strings.ToLower(replacer.Replace(id))
}

// normalizeAddress is the form an address is pinned and compared in. Unlike a
// fingerprint, its colons are part of it: host:port is the key.
func normalizeAddress(address string) string {
	return strings.ToLower(strings.TrimSpace(address))
}

// sameAddress compares an id the user typed with a pinned address. An id
// without a port names every pin under that host — a user forgets the address
// they dialed, not the port the tool appended to it.
func sameAddress(id, pinned string) bool {
	id, pinned = normalizeAddress(id), normalizeAddress(pinned)
	if id == pinned {
		return true
	}

	if host, _, err := net.SplitHostPort(pinned); err == nil {
		return normalizeAddress(host) == id
	}

	return false
}
