// Package widgets holds the small building blocks the GUI is assembled from.
//
// It is a package of its own rather than a file in ui because package ui is
// also built without the gui tag: the command-line binary links it for Run and
// the dialog-size arithmetic, and importing Fyne's widget tree there would add
// megabytes to a binary that never draws a window. Here the helpers stay out of
// that binary, and they can be tested by `make check`, which runs where the
// graphics toolkit is not installed.
package widgets

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"
)

// Label is text a user may need to copy.
//
// selectable turns it into drag-selectable text with the platform's copy
// shortcut. It has to be false on a touch screen: a selectable label takes the
// drag before the scroll container sees it, so a pane cannot be scrolled by
// starting the gesture on text. Copying there is served by an explicit action.
func Label(text string, selectable bool) *widget.Label {
	label := widget.NewLabel(text)
	label.Selectable = selectable
	label.Wrapping = fyne.TextWrapWord

	return label
}

// SelectableLabel is Label with selection on: the desktop default.
func SelectableLabel(text string) *widget.Label {
	return Label(text, true)
}

// Heading is a bold Label: a tab's or a section's title, which is still text a
// user may want to copy where the gesture does not have to scroll.
func Heading(text string, selectable bool) *widget.Label {
	label := Label(text, selectable)
	label.TextStyle = fyne.TextStyle{Bold: true}

	return label
}

// SelectableHeading is Heading with selection on: the desktop default.
func SelectableHeading(text string) *widget.Label {
	return Heading(text, true)
}

// CheckRow is a checkbox whose caption is a Label.
//
// A Fyne check paints its own caption and exposes no way to make it selectable,
// so the caption becomes a label beside a check with no text. The box is the
// only thing that toggles: a caption that toggled on tap could not also be
// selected by dragging across it, and selecting it is the point of the row.
type CheckRow struct {
	*fyne.Container

	box *widget.Check
}

// NewCheckRow builds a check whose caption can be dragged and copied where the
// caption is selectable.
//
// The check sits in the border's leading slot rather than in an HBox so the
// caption wraps in the width that is left: a phone-width window has no room for
// "Skip the certificate check (insecure)" on one line.
func NewCheckRow(text string, changed func(bool), selectable bool) *CheckRow {
	box := widget.NewCheck("", changed)

	return &CheckRow{
		Container: container.NewBorder(nil, nil, box, nil, Label(text, selectable)),
		box:       box,
	}
}

// Checked reports whether the box is ticked.
//
// It is a method rather than a reachable field so a caller cannot hide the box
// on its own, which would leave the caption behind on screen.
func (r *CheckRow) Checked() bool { return r.box.Checked }

// SetChecked ticks or clears the box.
func (r *CheckRow) SetChecked(checked bool) { r.box.SetChecked(checked) }

// FormRow is a dialog row whose caption sits above its field.
//
// widget.Form draws the caption itself from FormItem.Text and hands back no
// handle on it, so the caption moves into the row: an empty Text leaves the
// form's label column zero-width and the caption sits above its field. That is
// the shape the toolkit picks for a form on a small screen, and a dialog is
// narrow on every screen.
func FormRow(text string, field fyne.CanvasObject, selectable bool) *widget.FormItem {
	return &widget.FormItem{Text: "", Widget: container.NewVBox(Label(text, selectable), field)}
}

// Row is one caption and the field it names.
type Row struct {
	Caption string
	Field   fyne.CanvasObject
}

// Form lays the rows out as a form.
//
// A wide window gets the familiar two columns: every caption in one column and
// every field beside it. A narrow one stacks each caption above its field, so a
// phone has the width for the field rather than for a second column. The
// captions are selectable only where a drag does not have to scroll, for the
// reason Label gives.
func Form(stack, selectable bool, rows []Row) fyne.CanvasObject {
	if stack {
		lines := make([]fyne.CanvasObject, 0, len(rows))
		for _, row := range rows {
			lines = append(lines, container.NewVBox(Label(row.Caption, selectable), row.Field))
		}

		return container.NewVBox(lines...)
	}

	cells := make([]fyne.CanvasObject, 0, len(rows)*2)
	for _, row := range rows {
		cells = append(cells, Label(row.Caption, selectable), row.Field)
	}

	return container.New(layout.NewFormLayout(), cells...)
}
