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
	"fyne.io/fyne/v2/widget"
)

// SelectableLabel is a label a user can drag across and copy.
//
// A plain widget.Label paints text that cannot be selected, which forces a copy
// button beside every value worth keeping; a selectable label needs none.
func SelectableLabel(text string) *widget.Label {
	label := widget.NewLabel(text)
	label.Selectable = true
	label.Wrapping = fyne.TextWrapWord

	return label
}

// SelectableHeading is a bold SelectableLabel: a tab's or a section's title,
// which is still text a user may want to copy.
func SelectableHeading(text string) *widget.Label {
	label := SelectableLabel(text)
	label.TextStyle = fyne.TextStyle{Bold: true}

	return label
}

// CheckRow is a checkbox whose caption is selectable text.
//
// A Fyne check paints its own caption and exposes no way to make it selectable,
// so the caption becomes a label beside a check with no text. The box is the
// only thing that toggles: a caption that toggled on tap could not also be
// selected by dragging across it, and selecting it is the point of the row.
type CheckRow struct {
	*fyne.Container

	box *widget.Check
}

// NewCheckRow builds a check whose caption can be dragged and copied.
//
// The check sits in the border's leading slot rather than in an HBox so the
// caption wraps in the width that is left: a phone-width window has no room for
// "Skip the certificate check (insecure)" on one line.
func NewCheckRow(text string, changed func(bool)) *CheckRow {
	box := widget.NewCheck("", changed)

	return &CheckRow{
		Container: container.NewBorder(nil, nil, box, nil, SelectableLabel(text)),
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

// FormRow is a form item whose caption is selectable text.
//
// widget.Form draws the caption itself from FormItem.Text and hands back no
// handle on it, so the caption moves into the row: an empty Text leaves the
// form's label column zero-width and the caption sits above its field. That is
// also the shape the toolkit picks for a form on a small screen, so the desktop
// does not get a layout the phone cannot show.
func FormRow(text string, field fyne.CanvasObject) *widget.FormItem {
	return &widget.FormItem{Text: "", Widget: container.NewVBox(SelectableHeading(text), field)}
}
