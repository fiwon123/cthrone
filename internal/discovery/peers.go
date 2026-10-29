package discovery

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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

// PinnedPeer is a device identity that has been seen and pinned: the answer to
// "is this the same device as last time?".
type PinnedPeer struct {
	Name        string    `json:"name"`
	Key         string    `json:"key"`
	Fingerprint string    `json:"fingerprint"`
	FirstSeen   time.Time `json:"first_seen"`
	LastSeen    time.Time `json:"last_seen"`
}

// pinFile is the on-disk form. The version leaves room for a future change
// that has to read older files differently.
type pinFile struct {
	Version int                   `json:"version"`
	Peers   map[string]PinnedPeer `json:"peers"`
}

// PinStore is the set of devices this device trusts on first contact. It is
// the trust-on-first-use record: the first key seen for a name is pinned, and
// every later contact has to present the same key.
type PinStore struct {
	path  string
	mu    sync.Mutex
	peers map[string]PinnedPeer
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
	s := &PinStore{path: path, peers: make(map[string]PinnedPeer)}

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

	return s, nil
}

// Accept decides whether a peer's key is trusted, and records it on first
// contact.
//
// The identity is the key, so a known key under a known name is accepted
// without a question. A known name with a different key is refused: that is
// what pinning means, and the user clears it with Forget. A name that is new —
// or a peer that has none, which is keyed by its fingerprint — is pinned now,
// and first says so, so the caller can ask the user to compare the
// fingerprint with the other screen.
func (s *PinStore) Accept(pub []byte, name string) (string, bool, error) {
	if len(pub) != ed25519.PublicKeySize {
		return "", false, fmt.Errorf("discovery: a device key is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}

	fingerprint := Fingerprint(pub)
	key := hex.EncodeToString(pub)

	id := name
	if id == "" {
		// An unnamed peer is still an identity: the fingerprint is what names
		// it, and two unnamed devices stay two pins.
		id = fingerprint
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if pinned, ok := s.peers[id]; ok {
		if pinned.Key != key {
			return fingerprint, false, fmt.Errorf("%w: %q", ErrIdentityChanged, displayName(pinned, name))
		}

		pinned.LastSeen = time.Now()
		if name != "" {
			pinned.Name = name
		}
		s.peers[id] = pinned

		return fingerprint, false, s.save()
	}

	now := time.Now()
	s.peers[id] = PinnedPeer{
		Name:        name,
		Key:         key,
		Fingerprint: fingerprint,
		FirstSeen:   now,
		LastSeen:    now,
	}

	return fingerprint, true, s.save()
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

// Forget removes a pin by name or fingerprint and reports whether one was
// there. It is the way back from a reinstalled device, or from a name that was
// paired by mistake.
func (s *PinStore) Forget(id string) bool {
	want := normalizePinID(id)

	s.mu.Lock()
	defer s.mu.Unlock()

	for key, p := range s.peers {
		if normalizePinID(key) == want || normalizePinID(p.Name) == want || normalizePinID(p.Fingerprint) == want {
			delete(s.peers, key)

			return s.save() == nil
		}
	}

	return false
}

// save writes the pins in a form a person can read and a future version can
// extend. The file holds public keys only, so it is not a secret — but it is
// still written 0600, because who this device talks to is nobody else's
// business.
func (s *PinStore) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(s.path), err)
	}

	content, err := json.MarshalIndent(pinFile{Version: 1, Peers: s.peers}, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding the pins: %w", err)
	}

	if err := os.WriteFile(s.path, append(content, '\n'), 0o600); err != nil {
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
