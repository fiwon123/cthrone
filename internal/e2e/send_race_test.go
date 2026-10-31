package e2e

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/session"
	"github.com/fiwon123/cthrone/internal/wire"
)

// blockingInner is a session whose write is held open until the test lets it
// go, so "is a second send already inside the transport?" is a question the
// test can answer instead of a race it has to hope for.
type blockingInner struct {
	session.Session

	entered chan []byte
	release chan struct{}
}

func (b *blockingInner) Send(payload []byte) error {
	b.entered <- append([]byte(nil), payload...)
	<-b.release

	return b.Session.Send(payload)
}

// TestAnOverlappingSendWaitsForTheOneInFlight is the deterministic half of
// issue #192: while one send is inside the transport, a second one must not
// get there yet.
//
// The sequence number is assigned to the frame before it is written, so two
// writes in flight at once are two sequence numbers that can reach the wire in
// the wrong order — and the peer is right to refuse that. Holding the first
// write open makes the overlap certain, which a concurrent hammer alone cannot
// promise.
func TestAnOverlappingSendWaitsForTheOneInFlight(t *testing.T) {
	p := newPair(t, secret, secret, Single)
	p.bothVerified(t)

	inner := &blockingInner{
		Session: p.a.inner,
		entered: make(chan []byte, 2),
		release: make(chan struct{}),
	}
	p.a.inner = inner

	first := make(chan error, 1)

	go func() { first <- p.a.Send([]byte("first")) }()

	// Wait until the first send is inside the transport, past the point where
	// it took its sequence number.
	var onWire []byte

	select {
	case onWire = <-inner.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the first send never reached the transport")
	}

	second := make(chan error, 1)

	go func() { second <- p.a.Send([]byte("second")) }()

	select {
	case <-inner.entered:
		t.Fatal("a second send reached the transport while the first was still in flight")
	case <-time.After(100 * time.Millisecond):
		// The second send is waiting: what it is waiting for is the fix.
	}

	close(inner.release)

	select {
	case err := <-first:
		if err != nil {
			t.Fatalf("the first send failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the first send never finished")
	}

	select {
	case <-inner.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("the second send never reached the transport after the first finished")
	}

	select {
	case err := <-second:
		if err != nil {
			t.Fatalf("the second send failed: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the second send never finished")
	}

	// The held payload is the first one, in the frame that was cut for it: the
	// ordering the receiver depends on is the ordering of the wire, not of the
	// calls.
	head, err := sealedHeader(onWire)
	if err != nil {
		t.Fatalf("reading the held frame: %v", err)
	}

	if head.seq != 1 {
		t.Fatalf("the first send went out at sequence %d, want 1", head.seq)
	}

	if p.a.IsClosed() || p.b.IsClosed() {
		t.Fatalf("the session closed on two lawful sends (bob refused: %v)", p.wb.refused())
	}

	if got := len(p.wb.received()); got != 2 {
		t.Fatalf("bob received %d messages, want 2", got)
	}
}

// TestConcurrentSendsKeepTheSequenceOrder drives the shape the defect was
// reported from: several senders at once, each with its own stream of
// messages, over one session.
//
// The assertion is on the wire, not on the payloads, because every payload
// arrives — it is the sequence numbers that disagree with the order they were
// written in, and that is what the peer refuses.
func TestConcurrentSendsKeepTheSequenceOrder(t *testing.T) {
	p := newPair(t, secret, secret, Single)
	p.bothVerified(t)

	const (
		senders = 8
		each    = 100
		total   = senders * each
	)

	var wg sync.WaitGroup

	start := make(chan struct{})
	errs := make(chan error, total)

	for id := 0; id < senders; id++ {
		wg.Add(1)

		go func(id int) {
			defer wg.Done()

			// Every sender leaves the barrier together, so the sends really do
			// overlap instead of queueing behind the first one's goroutine.
			<-start

			for n := 0; n < each; n++ {
				if err := p.a.Send([]byte(fmt.Sprintf("%d-%d", id, n))); err != nil {
					errs <- err
				}
			}
		}(id)
	}

	close(start)
	wg.Wait()
	close(errs)

	for err := range errs {
		t.Fatalf("a concurrent send failed: %v", err)
	}

	var seqs []uint64

	for _, frame := range p.sa.outgoing() {
		// The handshake is not sealed and carries no sequence number.
		if wire.IsFrame(frame) {
			continue
		}

		head, err := sealedHeader(frame)
		if err != nil {
			t.Fatalf("reading a sealed frame off the wire: %v", err)
		}

		seqs = append(seqs, head.seq)
	}

	if len(seqs) != total {
		t.Fatalf("the wire carried %d sealed frames, want %d", len(seqs), total)
	}

	for i, seq := range seqs {
		if want := uint64(i + 1); seq != want {
			t.Fatalf("frame %d on the wire carries sequence %d, want %d: the wire order is not the send order", i, seq, want)
		}
	}

	// The reorder is not only a wire detail: the peer refuses the frame, and a
	// refusal ends the session.
	if p.a.IsClosed() || p.b.IsClosed() {
		t.Fatalf("the session closed on a lawful sender (alice closed: %v, bob closed: %v, bob refused: %v)", p.a.IsClosed(), p.b.IsClosed(), p.wb.refused())
	}

	if got := len(p.wb.received()); got != total {
		t.Fatalf("bob received %d messages, want %d", got, total)
	}
}
