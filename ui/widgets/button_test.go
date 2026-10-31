package widgets

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
)

// newCopyButtonApp gives each test its own clipboard and window-independent app.
func newCopyButtonApp(t *testing.T) {
	t.Helper()

	app := test.NewApp()
	t.Cleanup(app.Quit)
}

func clipboard(t *testing.T) string {
	t.Helper()

	c := fyne.CurrentApp().Clipboard()
	if c == nil {
		t.Fatal("the test app has no clipboard")
	}

	return c.Content()
}

func TestCopyButtonPlacesTheCatcherAboveTheButton(t *testing.T) {
	newCopyButtonApp(t)

	button := NewButton("Host", nil, nil)

	if len(button.content.Objects) != 2 {
		t.Fatalf("button holds %d objects, want the button and the catcher", len(button.content.Objects))
	}

	if button.content.Objects[0] != fyne.CanvasObject(button.button) {
		t.Error("the real button is not the first object")
	}

	if button.content.Objects[1] != fyne.CanvasObject(button.catcher) {
		t.Error("the catcher is not the last object; the driver would stop at the button")
	}

	if button.button.Text != "Host" {
		t.Errorf("button text = %q, want %q", button.button.Text, "Host")
	}
}

func TestCopyButtonTapRunsTheAction(t *testing.T) {
	newCopyButtonApp(t)

	taps := 0
	button := NewButton("Host", func() { taps++ }, nil)

	test.Tap(button)

	if taps != 1 {
		t.Errorf("tap ran the action %d times, want 1", taps)
	}

	if got := clipboard(t); got != "" {
		t.Errorf("a normal tap copied %q; copying belongs to the long press", got)
	}
}

func TestCopyButtonLongPressCopiesTheLabel(t *testing.T) {
	newCopyButtonApp(t)

	copied := ""
	button := NewButton("More options", nil, func(text string) { copied = text })

	test.TapSecondary(button)

	if got := clipboard(t); got != "More options" {
		t.Errorf("clipboard = %q, want the button label", got)
	}

	if copied != "More options" {
		t.Errorf("onCopied got %q, want the label", copied)
	}
}

// The catcher is what the driver actually meets, so the gestures have to work
// through it as well as through the widget.
func TestCopyCatcherForwardsBothGestures(t *testing.T) {
	newCopyButtonApp(t)

	taps := 0
	button := NewButton("Connect", func() { taps++ }, nil)

	test.Tap(button.catcher)
	if taps != 1 {
		t.Errorf("tap through the catcher ran the action %d times, want 1", taps)
	}

	test.TapSecondary(button.catcher)
	if got := clipboard(t); got != "Connect" {
		t.Errorf("clipboard = %q, want the label", got)
	}
}

func TestCopyButtonSetTextLeavesTheCopyLabelAlone(t *testing.T) {
	newCopyButtonApp(t)

	button := NewButton("Copy all", nil, nil)
	button.SetText("")

	if button.button.Text != "" {
		t.Errorf("visible text = %q, want an icon-only button", button.button.Text)
	}

	test.TapSecondary(button)

	if got := clipboard(t); got != "Copy all" {
		t.Errorf("clipboard = %q, want the full label an icon-only button still carries", got)
	}
}

func TestCopyButtonSetCopyTextFollowsAChangingLabel(t *testing.T) {
	newCopyButtonApp(t)

	button := NewButton("More options", nil, nil)
	button.SetText("Fewer options")
	button.SetCopyText("Fewer options")

	test.TapSecondary(button)

	if got := clipboard(t); got != "Fewer options" {
		t.Errorf("clipboard = %q, want the label currently on screen", got)
	}
}

func TestCopyButtonDisabledDoesNothing(t *testing.T) {
	newCopyButtonApp(t)

	taps := 0
	button := NewButton("Send", func() { taps++ }, nil)
	button.Disable()

	if !button.Disabled() {
		t.Fatal("Disable left the button enabled")
	}

	test.Tap(button)
	test.TapSecondary(button)

	if taps != 0 {
		t.Errorf("a disabled button ran its action %d times", taps)
	}

	if got := clipboard(t); got != "" {
		t.Errorf("a disabled button copied %q", got)
	}
}

func TestCopyButtonEnabledAgainWorks(t *testing.T) {
	newCopyButtonApp(t)

	taps := 0
	button := NewButton("Send", func() { taps++ }, nil)
	button.Disable()
	button.Enable()

	test.Tap(button)

	if taps != 1 {
		t.Errorf("an enabled button ran its action %d times, want 1", taps)
	}
}

func TestCopyButtonEmptyLabelCopiesNothing(t *testing.T) {
	newCopyButtonApp(t)

	copied := false
	button := NewButton("", nil, func(string) { copied = true })

	test.TapSecondary(button)

	if got := clipboard(t); got != "" {
		t.Errorf("an empty label copied %q", got)
	}

	if copied {
		t.Error("onCopied ran for an empty label")
	}
}

func TestCopyButtonWithIconKeepsTheIcon(t *testing.T) {
	newCopyButtonApp(t)

	button := NewButtonWithIcon("Send", theme.MailSendIcon(), nil, nil)

	if button.button.Icon == nil {
		t.Fatal("the icon was dropped")
	}

	if button.button.Icon.Name() != theme.MailSendIcon().Name() {
		t.Errorf("icon = %q, want %q", button.button.Icon.Name(), theme.MailSendIcon().Name())
	}
}

func TestCopyButtonSetOnTappedReplacesTheAction(t *testing.T) {
	newCopyButtonApp(t)

	first, second := 0, 0
	button := NewButton("Save as…", func() { first++ }, nil)
	button.SetOnTapped(func() { second++ })

	test.Tap(button)

	if first != 0 {
		t.Errorf("the old action ran %d times", first)
	}

	if second != 1 {
		t.Errorf("the new action ran %d times, want 1", second)
	}
}

func TestCopyButtonSetImportanceReachesTheButton(t *testing.T) {
	newCopyButtonApp(t)

	button := NewButton("Chat", nil, nil)
	button.SetImportance(widget.LowImportance)

	if button.button.Importance != widget.LowImportance {
		t.Errorf("importance = %v, want LowImportance", button.button.Importance)
	}
}

// The catcher is the object the pointer meets, so it has to carry the cursor
// and pass hover through, or the button under it never highlights.
func TestCopyCatcherCarriesHoverAndCursor(t *testing.T) {
	newCopyButtonApp(t)

	button := NewButton("Host", nil, nil)
	catcher := button.catcher

	catcher.MouseIn(&desktop.MouseEvent{})
	catcher.MouseMoved(&desktop.MouseEvent{})
	catcher.MouseOut()

	if got := catcher.Cursor(); got != button.button.Cursor() {
		t.Errorf("cursor = %v, want the button's cursor %v", got, button.button.Cursor())
	}
}
