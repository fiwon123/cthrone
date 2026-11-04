//go:build gui

package ui

import (
	"fmt"
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// These benchmarks exist for #432: the window is reported to slow to a crawl
// when two devices talk, and the one part of that question this machine can
// answer is whether the transcript itself grows expensive. The log is bounded
// (`maxMessages`), so a flat cost at the cap rules the log out and leaves the
// renderer or the media in the row.
//
// They measure the path `CTHRONE_PERF` times, on Fyne's software driver: no
// pixel is painted. A flat result says the log is not the cost; it does not say
// the window is fast.

// newBenchGUI builds the screen the widget tests build, without their
// *testing.T: the message path reads no configuration, so a benchmark does not
// need a temporary config home.
func newBenchGUI(b *testing.B) *gui {
	b.Helper()

	app := test.NewApp()
	b.Cleanup(app.Quit)

	win := app.NewWindow("cthrone")
	win.Resize(fyne.NewSize(900, 640))

	g := &gui{app: app, win: win}
	g.build()
	b.Cleanup(g.stopCopyHint)

	return g
}

// fill puts n messages in the transcript without going through the widget, so
// the measured iterations are the only ones that cost anything.
func fill(g *gui, n int) {
	g.mu.Lock()
	defer g.mu.Unlock()

	g.messages = make([]chatMessage, 0, n+1)

	for i := 0; i < n; i++ {
		g.messages = append(g.messages, chatMessage{from: "alice", text: "a message from the other device"})
	}
}

// BenchmarkAppendMessageFresh measures one append below the cap, on a buffer
// that is reused: no trim, no growth, nothing but the append.
func BenchmarkAppendMessageFresh(b *testing.B) {
	log := make([]chatMessage, 0, maxMessages+1)

	message := chatMessage{from: "alice", text: "a message from the other device"}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		_ = appendMessage(log[:0], message)
	}
}

// BenchmarkAppendMessageAtTheCap measures the same append once the log is full:
// every message now copies the surviving rows down, which is the only per
// message cost that does not go away with the cap.
func BenchmarkAppendMessageAtTheCap(b *testing.B) {
	log := make([]chatMessage, 0, maxMessages+1)
	for i := 0; i < maxMessages; i++ {
		log = append(log, chatMessage{from: "alice", text: "a message from the other device"})
	}

	message := chatMessage{from: "alice", text: "a message from the other device"}

	b.ReportAllocs()
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		log = appendMessage(log, message)
	}
}

// BenchmarkAddMessage measures what CTHRONE_PERF counts — the append, the
// list's Refresh and the scroll that follows — at several transcript sizes.
//
// The log is put back to its size before every iteration, so `rows` holds for
// the whole run instead of drifting up to the cap: the append's own cost is
// what is being compared, not the path to the cap.
func BenchmarkAddMessage(b *testing.B) {
	for _, rows := range []int{1, 50, 100, 250, maxMessages} {
		b.Run(fmt.Sprintf("rows=%d", rows), func(b *testing.B) {
			g := newBenchGUI(b)
			fill(g, rows)

			g.mu.Lock()
			snapshot := g.messages
			g.mu.Unlock()

			message := chatMessage{from: "alice", text: "a message from the other device"}

			b.ReportAllocs()
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				g.mu.Lock()
				g.messages = snapshot
				g.mu.Unlock()

				g.addMessage(message)
			}
		})
	}
}
