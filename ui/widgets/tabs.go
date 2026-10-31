package widgets

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// Tabs is a tab bar whose labels can be copied.
//
// container.AppTabs paints its own tab buttons and exposes no handle on them,
// so a tab label could not become a CopyButton there. This is the same shape —
// a bar on top, one pane below — built from widgets the app can reach: each tab
// is a CopyButton whose short press selects the tab and whose long press copies
// the tab's text.
//
// It accepts *container.TabItem so a caller lays its tabs out exactly as it
// would for AppTabs.
type Tabs struct {
	*fyne.Container

	// Items are the tabs, in bar order. The field is exported because the
	// keyboard shortcut needs the count, just as it did on AppTabs.
	Items []*container.TabItem

	// OnSelected runs after the selected tab changes.
	OnSelected func(*container.TabItem)

	bar      *fyne.Container
	stack    *fyne.Container
	buttons  []*CopyButton
	selected int
}

// NewTabs builds a tab bar from items.
//
// stacked is the narrow-screen arrangement: equal-width tabs in a grid, so four
// names share the width instead of pushing each other off the edge. onCopied is
// forwarded to every tab label, so one callback confirms a copy wherever it
// happened.
func NewTabs(stacked bool, onCopied func(string), items ...*container.TabItem) *Tabs {
	t := &Tabs{
		Items:    items,
		selected: -1,
		buttons:  make([]*CopyButton, len(items)),
	}

	barObjects := make([]fyne.CanvasObject, len(items))
	contents := make([]fyne.CanvasObject, 0, len(items))

	for i, item := range items {
		index := i
		button := NewButtonWithIcon(item.Text, item.Icon, func() { t.SelectIndex(index) }, onCopied)

		t.buttons[i] = button
		barObjects[i] = button

		// Every pane starts hidden; SelectIndex shows the first one. A hidden
		// pane is also left out of the stack's minimum size, so the tallest tab
		// does not set the height of the one on screen.
		if item.Content != nil {
			item.Content.Hide()
			contents = append(contents, item.Content)
		}
	}

	if stacked {
		t.bar = container.NewGridWithColumns(max(1, len(items)), barObjects...)
	} else {
		t.bar = container.NewHBox(barObjects...)
	}

	t.stack = container.NewStack(contents...)
	t.Container = container.NewBorder(
		container.NewVBox(container.NewPadded(t.bar), widget.NewSeparator()),
		nil, nil, nil,
		t.stack,
	)

	if len(items) > 0 {
		t.SelectIndex(0)
	}

	return t
}

// SelectIndex shows the tab at index, and marks its label as the selected one.
//
// An index outside the bar is ignored rather than clamped: a caller that asked
// for a tab that does not exist has a bug, and moving somewhere else would hide
// it.
func (t *Tabs) SelectIndex(index int) {
	if index < 0 || index >= len(t.Items) {
		return
	}

	t.selected = index

	for i, item := range t.Items {
		if item.Content == nil {
			continue
		}

		if i == index {
			item.Content.Show()
		} else {
			item.Content.Hide()
		}
	}

	for i, button := range t.buttons {
		if i == index {
			button.SetImportance(widget.MediumImportance)
		} else {
			button.SetImportance(widget.LowImportance)
		}
	}

	if t.stack != nil {
		t.stack.Refresh()
	}

	if t.OnSelected != nil {
		t.OnSelected(t.Items[index])
	}
}

// SelectedIndex reports the tab on screen, or -1 when the bar is empty.
func (t *Tabs) SelectedIndex() int {
	return t.selected
}

// ButtonAt returns the label of the tab at index, or nil outside the bar.
//
// It is how a caller reaches a tab label as an object — the app uses it to
// drive the bar in tests, and it keeps the buttons private so nothing can
// quietly take one out of the bar.
func (t *Tabs) ButtonAt(index int) *CopyButton {
	if index < 0 || index >= len(t.buttons) {
		return nil
	}

	return t.buttons[index]
}
