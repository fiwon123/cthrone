//go:build gui

package ui

import (
	"testing"
	"time"
)

// stubLiveServer stands in for a running host: the button state only needs the
// handle's presence, not a bound port.
type stubLiveServer struct{}

func (stubLiveServer) Addr() string { return "127.0.0.1:8080" }
func (stubLiveServer) Stop() error  { return nil }

// Host and Connect change what this device is, and it can be only one of them:
// a second Host on a port the first still holds fails with "address already in
// use", which reads as broken hosting rather than as one at a time. Both are
// off for as long as the transport is owned — by a start, a stop, a host, or a
// session.
func TestHostAndConnectAreOffWhileTheTransportIsOwned(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(g *gui)
		wantOff bool
	}{
		{"idle", func(*gui) {}, false},
		{"a start is binding", func(g *gui) { g.starting = true }, true},
		{"a stop is closing the port", func(g *gui) { g.stopping = true }, true},
		{"this device is hosting", func(g *gui) { g.server = stubLiveServer{} }, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := newTestGUI(t)
			tt.prepare(g)

			g.updateButtons()

			if got := g.hostBtn.Disabled(); got != tt.wantOff {
				t.Errorf("Host disabled = %v, want %v", got, tt.wantOff)
			}

			if got := g.connectBtn.Disabled(); got != tt.wantOff {
				t.Errorf("Connect disabled = %v, want %v", got, tt.wantOff)
			}
		})
	}
}

// While a host is up, Stop is the action that makes sense — it has to stay
// available, or a host with nobody connected could not be ended from the app.
func TestStopIsOfferedWhileHosting(t *testing.T) {
	g := newTestGUI(t)
	g.server = stubLiveServer{}

	g.updateButtons()

	if g.stopBtn.Disabled() {
		t.Error("Stop is disabled while this device hosts, so the host cannot be ended")
	}
}

// A second Host or Connect started while one is still binding is refused before
// it touches the port, and the attempt that got there first keeps its sequence
// number — which is how a Stop cancels it.
func TestBeginStartRefusesASecondTransport(t *testing.T) {
	g := newTestGUI(t)

	seq, ok := g.beginStart()
	if !ok {
		t.Fatal("the first Host/Connect was refused on an idle window")
	}

	if _, again := g.beginStart(); again {
		t.Fatal("a second Host/Connect was allowed while the first was binding")
	}

	if !g.finishStart(seq, nil) {
		t.Fatal("the first attempt was reported cancelled without a Stop")
	}
}

// A Stop that arrives while a start is binding cancels it: the attempt is not
// installed, so whatever it opened is closed instead of becoming the live
// transport behind a button nobody is holding.
func TestAStopCancelsAStartInFlight(t *testing.T) {
	g := newTestGUI(t)

	seq, ok := g.beginStart()
	if !ok {
		t.Fatal("the first Host/Connect was refused on an idle window")
	}

	g.stop()

	installed := false
	if g.finishStart(seq, func() { installed = true }) {
		t.Fatal("an attempt a Stop cancelled was still installed")
	}

	if installed {
		t.Fatal("the cancelled attempt's install ran")
	}

	// The reservation a stop takes is released once the port is closed, or Host
	// and Connect never come back and the app is stuck between them.
	deadline := time.Now().Add(5 * time.Second)
	for {
		g.updateButtons()

		if !g.hostBtn.Disabled() && !g.connectBtn.Disabled() {
			break
		}

		if time.Now().After(deadline) {
			t.Fatal("Host and Connect stayed disabled after a stop finished")
		}

		time.Sleep(10 * time.Millisecond)
	}
}
