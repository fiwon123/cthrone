//go:build (!linux && !windows) || android

package bluetoothcore

import (
	"errors"
	"testing"
	"time"

	"github.com/fiwon123/cthrone/internal/pairing"
)

// A build without the transport must say so, and must refuse rather than
// pretend: the answer and the entry points have to agree, or the GUI would
// offer an option that cannot work.
func TestSupported(t *testing.T) {
	if Supported() {
		t.Fatal("Supported() = true on a build that has no Bluetooth transport")
	}

	if err := Host(nil, pairing.Options{}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("Host() error = %v, want ErrUnsupported", err)
	}

	if err := ScanPeers(time.Millisecond, func(Peer) {}); !errors.Is(err, ErrUnsupported) {
		t.Errorf("ScanPeers() error = %v, want ErrUnsupported", err)
	}
}
