//go:build gui

package ui

import (
	"errors"

	"github.com/fiwon123/cthrone/internal/session"
)

// There are two reasons a window has nowhere to send, and they need different
// words: no connection at all, or a host whose room is still empty.
var (
	errNoSession = errors.New("connect, or wait for a device to join")
	errEmptyRoom = errors.New("nobody has joined the room yet")
)

// roomSender sends a payload to every session a host holds.
//
// A host is a room, and its own message has to reach all of it. It used to go
// through g.sess — the one session the window happened to hold, which is the
// last device that joined — so a second device never saw the host speak (#320).
type roomSender struct{ hub *session.Hub }

func (r roomSender) Send(payload []byte) error {
	if r.hub == nil {
		return session.ErrClosed
	}

	if r.hub.Count() == 0 {
		return errEmptyRoom
	}

	r.hub.Broadcast("", payload)

	return nil
}

// outgoing picks where this window's messages go: the room when hosting, the one
// session when a client. NATS needs no special case — its session publishes to
// the subject every device is subscribed to — and a host there has no hub.
func (g *gui) outgoing() (chunkSender, error) {
	g.mu.Lock()
	hub, sess := g.hub, g.sess
	g.mu.Unlock()

	if hub != nil {
		if hub.Count() == 0 {
			return nil, errEmptyRoom
		}

		return roomSender{hub: hub}, nil
	}

	if sess == nil {
		return nil, errNoSession
	}

	return sess, nil
}
