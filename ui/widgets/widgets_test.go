package widgets

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func TestSelectableLabelCanBeSelected(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	label := SelectableLabel("device key (fingerprint ab:cd)")

	if !label.Selectable {
		t.Error("label is not selectable")
	}
	if label.Text != "device key (fingerprint ab:cd)" {
		t.Errorf("text = %q, want the caption it was given", label.Text)
	}
}

func TestSelectableHeadingIsBold(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	heading := SelectableHeading("Quick Setup")

	if !heading.Selectable {
		t.Error("heading is not selectable")
	}
	if !heading.TextStyle.Bold {
		t.Error("heading is not bold")
	}
}

func TestNewCheckRowKeepsTheCaptionOutOfTheBox(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	changed := 0
	row := NewCheckRow("Encrypt with TLS", func(bool) { changed++ })

	if row.box.Text != "" {
		t.Errorf("check text = %q, want the caption to live in a label instead", row.box.Text)
	}

	caption := captionOf(t, row.Container)
	if !caption.Selectable {
		t.Error("caption is not selectable")
	}
	if caption.Text != "Encrypt with TLS" {
		t.Errorf("caption = %q, want %q", caption.Text, "Encrypt with TLS")
	}

	if row.Checked() {
		t.Error("a new row starts unchecked")
	}

	test.Tap(row.box)
	if !row.Checked() {
		t.Error("tapping the box did not tick it")
	}
	if changed != 1 {
		t.Errorf("changed ran %d times, want 1", changed)
	}
}

func TestCheckRowCheckedRoundTrips(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	row := NewCheckRow("Use NATS instead of websocket", nil)

	row.SetChecked(true)
	if !row.Checked() {
		t.Error("SetChecked(true) left the box clear")
	}

	row.SetChecked(false)
	if row.Checked() {
		t.Error("SetChecked(false) left the box ticked")
	}
}

func TestCheckRowHidesTheCaptionToo(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	row := NewCheckRow("Skip the certificate check (insecure)", nil)
	caption := captionOf(t, row.Container)

	row.Hide()
	if row.Visible() {
		t.Error("the row is still visible after Hide")
	}
	if !caption.Visible() {
		t.Error("the caption was hidden on its own; the row should hide as one")
	}
}

func TestFormRowPutsTheCaptionAboveTheField(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	field := widget.NewEntry()
	item := FormRow("Device name", field)

	if item.Text != "" {
		t.Errorf("FormItem.Text = %q, want it empty so the form draws no label of its own", item.Text)
	}

	objects := item.Widget.(*fyne.Container).Objects
	if len(objects) != 2 {
		t.Fatalf("row holds %d objects, want a caption and the field", len(objects))
	}

	caption, ok := objects[0].(*widget.Label)
	if !ok {
		t.Fatalf("first object is %T, want the caption", objects[0])
	}
	if !caption.Selectable {
		t.Error("caption is not selectable")
	}
	if !caption.TextStyle.Bold {
		t.Error("caption is not bold")
	}
	if caption.Text != "Device name" {
		t.Errorf("caption = %q, want %q", caption.Text, "Device name")
	}
	if objects[1] != fyne.CanvasObject(field) {
		t.Error("second object is not the field it was given")
	}
}

// captionOf returns the selectable caption a CheckRow wraps its box in.
func captionOf(t *testing.T, row *fyne.Container) *widget.Label {
	t.Helper()

	var (
		caption *widget.Label
		box     *widget.Check
	)
	for _, object := range row.Objects {
		switch object := object.(type) {
		case *widget.Label:
			caption = object
		case *widget.Check:
			box = object
		}
	}

	if box == nil {
		t.Fatal("row holds no check")
	}
	if caption == nil {
		t.Fatal("row holds no caption label")
	}

	return caption
}
