//go:build gui

package ui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2/storage"
)

// openServeWindow is how long the one-file server behind a phone's Open stays
// up. The browser starts fetching at once; the window is the slack for a slow
// device, and it bounds a listener nobody remembers opening.
var openServeWindow = 10 * time.Minute

// opensInBrowser reports whether a file is handed to the browser instead of the
// system opener.
//
// Android cannot pass a private file:// path to another app, and this APK has
// no content provider to pass a content:// one (#470). A loopback URL needs
// neither: the browser fetches the file from this app, which is the only writer
// and the only reader that matters. It is a variable so a test can present a
// phone.
var opensInBrowser = func() bool {
	return runtime.GOOS == "android"
}

// openFile hands a file to whatever this device uses for that kind of file.
//
// It is the answer for the kinds this program has no engine for — a PDF, an
// audio clip — and it goes through the same opener the About tab's links use,
// so there is one place that reports "no handler for this" instead of two. On
// Android that opener is a loopback URL, because a private file path cannot
// leave the app (#470).
func (g *gui) openFile(pathname, name string) {
	if opensInBrowser() {
		g.openInBrowser(pathname, name)

		return
	}

	g.openPage(storage.NewFileURI(pathname).String())
}

// openInBrowser serves one file on loopback and points the browser at it.
//
// The listener is bound to 127.0.0.1, the path carries 128 random bits, and the
// server is closed by the next Open, by openServeWindow, or by shutdown. The
// file is opened per request so a range request cannot race another on one file
// descriptor. The browser is a different app and the file is in this app's
// storage, which is exactly why a URL works where a file:// path does not.
func (g *gui) openInBrowser(pathname, name string) {
	if _, err := os.Stat(pathname); err != nil {
		g.showError(err)

		return
	}

	token, err := openToken()
	if err != nil {
		g.showError(err)

		return
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		g.showError(err)

		return
	}

	kind := typeOf(pathname, name)
	if kind == "" {
		kind = octetStream
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/"+token+"/", func(w http.ResponseWriter, r *http.Request) {
		file, err := os.Open(pathname)
		if err != nil {
			http.Error(w, "the file is no longer there", http.StatusNotFound)

			return
		}
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			http.Error(w, "the file is no longer there", http.StatusNotFound)

			return
		}

		w.Header().Set("Content-Type", kind)
		w.Header().Set("Content-Disposition", contentDisposition(name))
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")

		http.ServeContent(w, r, name, info.ModTime(), file)
	})

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	// One at a time: a second Open replaces the first, and the old listener
	// stops rather than leaving a second file reachable.
	g.setOpenServer(srv)

	go func() {
		_ = srv.Serve(listener)
	}()

	go func() {
		time.Sleep(openServeWindow)
		g.closeOpenServerIf(srv)
	}()

	g.openPage("http://" + listener.Addr().String() + "/" + token + "/")
}

// setOpenServer replaces the live one-file server, closing the previous one.
func (g *gui) setOpenServer(srv *http.Server) {
	g.mu.Lock()
	previous := g.openServer
	g.openServer = srv
	g.mu.Unlock()

	if previous != nil && previous != srv {
		_ = previous.Close()
	}
}

// closeOpenServerIf closes srv only when it is still the live one, so a timer
// from an older Open cannot stop a newer one.
func (g *gui) closeOpenServerIf(srv *http.Server) {
	g.mu.Lock()
	if g.openServer != srv {
		g.mu.Unlock()

		return
	}

	g.openServer = nil
	g.mu.Unlock()

	_ = srv.Close()
}

// closeOpenServer stops the one-file server, if one is up.
func (g *gui) closeOpenServer() {
	g.mu.Lock()
	srv := g.openServer
	g.openServer = nil
	g.mu.Unlock()

	if srv != nil {
		_ = srv.Close()
	}
}

// openToken is the path a browser must know: 128 random bits. It is not a
// credential for the chat, only a reason a stray app on the device cannot guess
// the URL while the server is up.
func openToken() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("making the open link: %w", err)
	}

	return hex.EncodeToString(buf[:]), nil
}

// contentDisposition asks the browser to show the file where it can, with the
// name the sender gave it. Anything outside printable ASCII is replaced: a
// header is not a place for a quote, a backslash, a line break or a name the
// browser would have to guess an encoding for.
func contentDisposition(name string) string {
	safe := strings.Map(func(r rune) rune {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			return '_'
		}

		return r
	}, name)

	if safe == "" {
		safe = "file"
	}

	return `inline; filename="` + safe + `"`
}
