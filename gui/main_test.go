package main

import (
	"os"
	"testing"
)

// The window starts whatever the arguments are: a double-click passes none, a
// user who read an old instruction types "ui", and a stray argument from a
// shortcut must not turn the window into "unknown command" (#382).
func TestTheWindowStartsWhateverTheArgumentsAre(t *testing.T) {
	previous := runWindow
	t.Cleanup(func() { runWindow = previous })

	saved := os.Args
	t.Cleanup(func() { os.Args = saved })

	cases := [][]string{
		{"cthrone_gui"},
		{"cthrone_gui", "ui"},
		{"cthrone_gui", "--version"},
		{"cthrone_gui", "scan", "--bluetooth"},
		{"cthrone_gui", ""},
	}

	for _, args := range cases {
		calls := 0
		runWindow = func() { calls++ }

		os.Args = args
		main()

		if calls != 1 {
			t.Errorf("main() with %q started the window %d times, want 1", args, calls)
		}
	}
}
