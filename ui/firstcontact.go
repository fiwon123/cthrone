package ui

import "github.com/fiwon123/cthrone/internal/e2e"

// firstContactText is the body of the question a host puts to its user when a
// device it has never met wants to join: who is asking, the key it presents,
// and the code the two screens have to agree on.
//
// The code is the part that matters. The fingerprint is exact and hard to read
// aloud; the code is six digits and lives only as long as the question, so a
// stranger that spliced itself between the two devices cannot make both screens
// show the same one.
func firstContactText(c e2e.FirstContact) string {
	name := c.Name
	if name == "" {
		name = "An unnamed device"
	}

	text := name + " wants to connect.\n\nThe code both screens show is " + c.Code + " — they must agree."
	if c.Fingerprint != "" {
		text += "\n\nIts fingerprint is " + c.Fingerprint + "."
	}

	return text
}

// contactName is what to call a device in a status line, where an empty name
// would leave a hole in the sentence.
func contactName(c e2e.FirstContact) string {
	if c.Name == "" {
		return "an unnamed device"
	}

	return c.Name
}

// waitingText is what the screen that is not asked anything says while the
// other side decides: the code it has to find on the other screen first.
//
// It is a waiting line rather than a question because pairing by key asks one
// person, not two: the host compares the two codes and accepts, and the client
// shows what it is waiting on.
func waitingText(c e2e.FirstContact) string {
	name := c.Name
	if name == "" {
		name = "the other device"
	}

	return "Waiting for " + name + " to accept — code " + c.Code + ". It must match the code on the other screen."
}
