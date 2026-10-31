package widgets

import (
	"reflect"
	"testing"

	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
)

func newTabsApp(t *testing.T) {
	t.Helper()

	app := test.NewApp()
	t.Cleanup(app.Quit)
}

// tabItems builds panes that can be proved visible or hidden.
func tabItems(names ...string) []*container.TabItem {
	items := make([]*container.TabItem, 0, len(names))
	for _, name := range names {
		items = append(items, container.NewTabItem(name, widget.NewLabel(name+" pane")))
	}

	return items
}

func TestTabsShowsTheFirstTab(t *testing.T) {
	newTabsApp(t)

	items := tabItems("Chat", "Setup", "About")
	tabs := NewTabs(false, nil, items...)

	if tabs.SelectedIndex() != 0 {
		t.Errorf("selected tab = %d, want the first one", tabs.SelectedIndex())
	}

	if !items[0].Content.Visible() {
		t.Error("the first pane is hidden")
	}

	for i := 1; i < len(items); i++ {
		if items[i].Content.Visible() {
			t.Errorf("pane %d is visible beside the first one", i)
		}
	}
}

func TestTabsSelectIndexShowsOnePane(t *testing.T) {
	newTabsApp(t)

	items := tabItems("Chat", "Setup", "About")
	tabs := NewTabs(false, nil, items...)

	tabs.SelectIndex(2)

	if tabs.SelectedIndex() != 2 {
		t.Errorf("selected tab = %d, want 2", tabs.SelectedIndex())
	}

	if !items[2].Content.Visible() {
		t.Error("the selected pane is hidden")
	}

	for i := 0; i < 2; i++ {
		if items[i].Content.Visible() {
			t.Errorf("pane %d is still visible beside the selected one", i)
		}
	}
}

func TestTabsIgnoreAnIndexOutsideTheBar(t *testing.T) {
	newTabsApp(t)

	tabs := NewTabs(false, nil, tabItems("Chat", "Setup")...)

	tabs.SelectIndex(-1)
	if tabs.SelectedIndex() != 0 {
		t.Errorf("a negative index moved the bar to %d", tabs.SelectedIndex())
	}

	tabs.SelectIndex(5)
	if tabs.SelectedIndex() != 0 {
		t.Errorf("an index past the end moved the bar to %d", tabs.SelectedIndex())
	}
}

func TestTabsWithoutItemsHasNoSelection(t *testing.T) {
	newTabsApp(t)

	tabs := NewTabs(false, nil)
	if tabs.SelectedIndex() != -1 {
		t.Errorf("an empty bar reports tab %d, want -1", tabs.SelectedIndex())
	}

	if tabs.ButtonAt(0) != nil {
		t.Error("an empty bar returned a button")
	}

	// The narrow arrangement takes a grid; a zero-column grid would panic.
	NewTabs(true, nil)
}

func TestTappingATabSelectsIt(t *testing.T) {
	newTabsApp(t)

	tabs := NewTabs(false, nil, tabItems("Chat", "Setup")...)

	test.Tap(tabs.ButtonAt(1))

	if tabs.SelectedIndex() != 1 {
		t.Errorf("tapping a tab selected %d, want 1", tabs.SelectedIndex())
	}
}

func TestLongPressingATabCopiesItsLabelWithoutSelecting(t *testing.T) {
	newTabsApp(t)

	copied := ""
	tabs := NewTabs(false, func(text string) { copied = text }, tabItems("Chat", "Setup")...)

	test.TapSecondary(tabs.ButtonAt(1))

	if got := clipboard(t); got != "Setup" {
		t.Errorf("clipboard = %q, want the tab label", got)
	}

	if copied != "Setup" {
		t.Errorf("onCopied got %q, want the tab label", copied)
	}

	if tabs.SelectedIndex() != 0 {
		t.Errorf("a copy gesture switched to tab %d", tabs.SelectedIndex())
	}
}

func TestTabsOnSelectedReportsTheTab(t *testing.T) {
	newTabsApp(t)

	var selected []string
	tabs := NewTabs(false, nil, tabItems("Chat", "Setup")...)
	tabs.OnSelected = func(item *container.TabItem) { selected = append(selected, item.Text) }

	tabs.SelectIndex(1)

	if len(selected) != 1 || selected[0] != "Setup" {
		t.Errorf("OnSelected reported %v, want [Setup]", selected)
	}
}

func TestTabsMarkTheSelectedLabel(t *testing.T) {
	newTabsApp(t)

	tabs := NewTabs(false, nil, tabItems("Chat", "Setup")...)

	if got := tabs.ButtonAt(0).button.Importance; got != widget.MediumImportance {
		t.Errorf("selected tab importance = %v, want MediumImportance", got)
	}

	if got := tabs.ButtonAt(1).button.Importance; got != widget.LowImportance {
		t.Errorf("unselected tab importance = %v, want LowImportance", got)
	}

	tabs.SelectIndex(1)

	if got := tabs.ButtonAt(0).button.Importance; got != widget.LowImportance {
		t.Errorf("previously selected tab importance = %v, want LowImportance", got)
	}

	if got := tabs.ButtonAt(1).button.Importance; got != widget.MediumImportance {
		t.Errorf("newly selected tab importance = %v, want MediumImportance", got)
	}
}

func TestTabsShareTheWidthWhenNarrow(t *testing.T) {
	newTabsApp(t)

	tabs := NewTabs(true, nil, tabItems("Chat", "Setup", "Devices", "About")...)

	if reflect.TypeOf(tabs.bar.Layout) != reflect.TypeOf(layout.NewGridLayoutWithColumns(1)) {
		t.Errorf("narrow tab bar layout is %T, want an equal-width grid", tabs.bar.Layout)
	}
}

func TestTabsKeepOneLineWhenWide(t *testing.T) {
	newTabsApp(t)

	tabs := NewTabs(false, nil, tabItems("Chat", "Setup")...)

	if reflect.TypeOf(tabs.bar.Layout) != reflect.TypeOf(layout.NewHBoxLayout()) {
		t.Errorf("wide tab bar layout is %T, want one line", tabs.bar.Layout)
	}
}

func TestTabsExposeTheirItems(t *testing.T) {
	newTabsApp(t)

	items := tabItems("Chat", "Setup")
	tabs := NewTabs(false, nil, items...)

	if len(tabs.Items) != len(items) {
		t.Fatalf("Tabs.Items holds %d tabs, want %d", len(tabs.Items), len(items))
	}

	if tabs.Items[1] != items[1] {
		t.Error("Tabs.Items does not carry the item it was given")
	}
}
