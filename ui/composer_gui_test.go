//go:build gui

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/widget"
)

// The composer's actions read like a chat app's: a paperclip, and Send as the
// primary action (#324). This is the shape, not the pixels.
func TestComposerActionsReadLikeAChatApp(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		if mobile {
			name = "phone"
		}

		t.Run(name, func(t *testing.T) {
			g := newScreenGUI(t, mobile)

			if got := g.attachBtn.Text(); got != "" {
				t.Errorf("the attach action shows %q, want the paperclip alone", got)
			}

			if got := g.sendBtn.Importance(); got != widget.HighImportance {
				t.Errorf("Send has importance %v, want the primary one", got)
			}
		})
	}
}

// On a phone the three actions share one row and Send fills the rest; a
// multi-line message grows the field and leaves the action row alone (#474).
func TestPhoneComposerActionsStayOnOneRow(t *testing.T) {
	g := newScreenGUI(t, true)

	g.win.Resize(fyne.NewSize(360, 720))
	g.win.Content().Resize(fyne.NewSize(360, 720))
	g.win.Content().Refresh()

	if g.sendBtn.Size().Width <= g.attachBtn.Size().Width {
		t.Errorf("Send is %.1f wide and attach %.1f: Send does not take the rest of the row",
			g.sendBtn.Size().Width, g.attachBtn.Size().Width)
	}

	actionsHeight := g.composerActions.MinSize().Height
	entryHeight := g.entry.MinSize().Height

	g.entry.SetText("one\ntwo\nthree\nfour\nfive")
	g.win.Content().Refresh()

	if g.entry.MinSize().Height <= entryHeight {
		t.Error("a five-line message did not grow the field")
	}

	if got := g.composerActions.MinSize().Height; got != actionsHeight {
		t.Errorf("the action row is %.1f high after a five-line message, was %.1f", got, actionsHeight)
	}
}

// The message field says nothing until the user types: the hint it carried was
// the field's first line spent on instructions (#344).
func TestTheComposerHasNoPlaceholder(t *testing.T) {
	for _, mobile := range []bool{false, true} {
		name := "desktop"
		if mobile {
			name = "phone"
		}

		t.Run(name, func(t *testing.T) {
			g := newScreenGUI(t, mobile)

			if got := g.entry.PlaceHolder; got != "" {
				t.Errorf("the composer shows the placeholder %q, want none", got)
			}
		})
	}
}

// On a phone, Enter breaks the line — every time — and the button sends. Fyne
// routes Return down two paths depending on a hidden flag, so the rule has to be
// stated rather than inferred (#362).
func TestOnAPhoneEnterAlwaysBreaksTheLine(t *testing.T) {
	g := newScreenGUI(t, true)

	sess := newSendRecorder()
	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	typeRunes(g.entry, "one")

	for press := 1; press <= 3; press++ {
		g.entry.TypedKey(&fyne.KeyEvent{Name: fyne.KeyEnter})
	}

	if got, want := g.entry.Text, "one\n\n\n"; got != want {
		t.Errorf("after three Enters the field reads %q, want %q", got, want)
	}

	select {
	case <-sess.done:
		t.Fatal("Enter sent the message on a phone, want a line break")
	default:
	}

	// The button sends the whole body.
	g.send(g.entry.Text)

	if got, want := string(sess.wait(t)), "one"; got != want {
		t.Errorf("the button sent %q, want %q", got, want)
	}
}

// The keyboard's action key sends on a phone, whatever the entry otherwise does
// with Return.
func TestAPhonesActionKeySends(t *testing.T) {
	g := newScreenGUI(t, true)

	sess := newSendRecorder()
	g.mu.Lock()
	g.sess = sess
	g.mu.Unlock()

	g.entry.SetText("ready to go")
	g.entry.OnSubmitted("ready to go")

	if got, want := string(sess.wait(t)), "ready to go"; got != want {
		t.Errorf("the action key sent %q, want %q", got, want)
	}
}
