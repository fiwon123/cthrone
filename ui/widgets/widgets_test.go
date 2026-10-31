package widgets

import (
	"reflect"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
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

func TestLabelCanBeInertForTouch(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	label := Label("scroll past me", false)

	if label.Selectable {
		t.Error("label is selectable; a drag has to reach the pane that scrolls")
	}
	if label.Wrapping != fyne.TextWrapWord {
		t.Error("label does not wrap; a long line would run off a phone")
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

func TestHeadingCanBeInertForTouch(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	heading := Heading("Quick Setup", false)

	if heading.Selectable {
		t.Error("heading is selectable; a drag has to reach the pane that scrolls")
	}
	if !heading.TextStyle.Bold {
		t.Error("heading is not bold")
	}
}

func TestNewCheckRowKeepsTheCaptionOutOfTheBox(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	changed := 0
	row := NewCheckRow("Encrypt with TLS", func(bool) { changed++ }, true)

	if row.box.Text != "" {
		t.Errorf("check text = %q, want the caption to live in a label instead", row.box.Text)
	}

	caption := captionOf(t, row.content)
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

func TestCheckRowCaptionCanBeInertForTouch(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	row := NewCheckRow("Use NATS instead of websocket", nil, false)

	if caption := captionOf(t, row.content); caption.Selectable {
		t.Error("caption is selectable; a drag has to reach the pane that scrolls")
	}
}

func TestCheckRowCheckedRoundTrips(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	row := NewCheckRow("Use NATS instead of websocket", nil, true)

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

	row := NewCheckRow("Skip the certificate check (insecure)", nil, true)
	caption := captionOf(t, row.content)

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
	item := FormRow("Device name", field, true)

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
	if caption.TextStyle.Bold {
		t.Error("caption is bold; a dialog row is a label, not a section heading")
	}
	if caption.Text != "Device name" {
		t.Errorf("caption = %q, want %q", caption.Text, "Device name")
	}
	if objects[1] != fyne.CanvasObject(field) {
		t.Error("second object is not the field it was given")
	}
}

func TestFormUsesTwoColumnsWhenWide(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	name, port := widget.NewEntry(), widget.NewEntry()
	form := Form(false, true, []Row{
		{Caption: "Device name", Field: name},
		{Caption: "Port", Field: port},
	})

	box, ok := form.(*fyne.Container)
	if !ok {
		t.Fatalf("form is %T, want a container", form)
	}
	if reflect.TypeOf(box.Layout) != reflect.TypeOf(layout.NewFormLayout()) {
		t.Fatalf("layout is %T, want the form's two columns", box.Layout)
	}
	if len(box.Objects) != 4 {
		t.Fatalf("form holds %d objects, want a caption and a field per row", len(box.Objects))
	}

	caption, ok := box.Objects[0].(*widget.Label)
	if !ok {
		t.Fatalf("first cell is %T, want the caption", box.Objects[0])
	}
	if !caption.Selectable {
		t.Error("caption is not selectable on a desktop")
	}
	if caption.Text != "Device name" {
		t.Errorf("first caption = %q, want %q", caption.Text, "Device name")
	}
	if box.Objects[1] != fyne.CanvasObject(name) || box.Objects[3] != fyne.CanvasObject(port) {
		t.Error("the fields do not follow their captions")
	}
}

// TestFormTwoColumnCaptionsKeepTheirWidth pins the regression v0.8.4 shipped:
// the caption column of the desktop Setup form collapsed to the width of the
// padding, so every caption was clipped to a sliver and the fields started
// almost at the left edge.
//
// layout.FormLayout sizes the label column from each caption's minimum width.
// A wrapping label reports only two inner paddings there, so the two-column
// captions must not wrap in their own minimum size — the same shape Fyne's own
// widget.Form uses for its labels.
func TestFormTwoColumnCaptionsKeepTheirWidth(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	field := widget.NewEntry()
	form := Form(false, true, []Row{{Caption: "Certificate fingerprint", Field: field}})

	box, ok := form.(*fyne.Container)
	if !ok {
		t.Fatalf("form is %T, want a container", form)
	}

	caption, ok := box.Objects[0].(*widget.Label)
	if !ok {
		t.Fatalf("first cell is %T, want the caption", box.Objects[0])
	}

	collapsed := 2 * theme.InnerPadding()
	if got := caption.MinSize().Width; got <= collapsed {
		t.Fatalf("caption minimum width = %v, want more than %v: the label column collapses to the padding", got, collapsed)
	}

	box.Resize(fyne.NewSize(600, 200))

	if caption.Size().Width < caption.MinSize().Width {
		t.Errorf("caption laid out %v wide, narrower than its minimum %v", caption.Size().Width, caption.MinSize().Width)
	}
	if field.Position().X < caption.Size().Width {
		t.Errorf("field starts at x=%v, inside the caption column (%v wide)", field.Position().X, caption.Size().Width)
	}
}

func TestFormStacksWhenNarrow(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()

	form := Form(true, false, []Row{
		{Caption: "Device name", Field: widget.NewEntry()},
		{Caption: "Port", Field: widget.NewEntry()},
	})

	box, ok := form.(*fyne.Container)
	if !ok {
		t.Fatalf("form is %T, want a container", form)
	}
	if reflect.TypeOf(box.Layout) != reflect.TypeOf(layout.NewVBoxLayout()) {
		t.Fatalf("layout is %T, want one stacked line per row", box.Layout)
	}
	if len(box.Objects) != 2 {
		t.Fatalf("box holds %d objects, want one line per row", len(box.Objects))
	}

	for i, object := range box.Objects {
		line, ok := object.(*fyne.Container)
		if !ok {
			t.Fatalf("line %d is %T, want a container", i, object)
		}
		if len(line.Objects) != 2 {
			t.Fatalf("line %d holds %d objects, want the caption above the field", i, len(line.Objects))
		}

		caption, ok := line.Objects[0].(*widget.Label)
		if !ok {
			t.Fatalf("line %d starts with %T, want the caption", i, line.Objects[0])
		}
		if caption.Selectable {
			t.Errorf("caption %q is selectable on touch", caption.Text)
		}
	}
}

// captionOf returns the caption a CheckRow wraps its box in.
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
