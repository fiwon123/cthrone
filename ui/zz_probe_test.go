//go:build gui

package ui

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

func TestProbeMobileLongPress(t *testing.T) {
	g := newScreenGUI(t, true)
	g.mu.Lock()
	g.messages = []chatMessage{{from: "laptop", text: "hello there", hidden: true}}
	g.mu.Unlock()
	g.chatList.Refresh()
	g.win.Resize(fyne.NewSize(360, 640))
	g.win.Content().Refresh()

	row := paintedText(g.win.Content(), hiddenBody, 0)
	if row == nil {
		t.Fatal("row not painted")
	}

	pos, ok := canvasPosition(g.win.Content(), row)
	if !ok {
		t.Fatal("no position")
	}
	centre := pos.Add(fyne.NewPos(row.Size().Width/2, row.Size().Height/2))
	found, double := hitTest(g.win.Content(), centre)
	t.Logf("row=%T size=%v  hit=%T (%v) double=%v", row, row.Size(), found, found, double)
	if sec, ok := found.(fyne.SecondaryTappable); ok {
		t.Logf("hit implements SecondaryTappable: %T", sec)
	}
	_ = fmt.Sprint
	_ = test.Tap
}
