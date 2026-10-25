package websockethandler

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func wsPair(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		wsConn = c
		close(serverReady)
		select {}
	}))
	t.Cleanup(srv.Close)

	var mu sync.Mutex
	wsConn = nil
	serverReady = make(chan struct{})

	dialer := websocket.Dialer{}
	client, _, err := dialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	<-serverReady

	mu.Lock()
	c := wsConn
	mu.Unlock()
	if c == nil {
		t.Fatal("server conn nil")
	}
	t.Cleanup(func() { c.Close() })
	return client, c
}

var (
	wsConn      *websocket.Conn
	serverReady = make(chan struct{})
)

func TestReceiveMsgLoopPrintsMessage(t *testing.T) {
	client, server := wsPair(t)
	var buf bytes.Buffer
	var mu sync.Mutex
	stdout = &safeWriter{w: &buf, mu: &mu}
	defer func() { stdout = os.Stdout }()

	current := []rune{}
	go receiveMsgLoop(server, &current)

	if err := client.WriteMessage(websocket.TextMessage, []byte("hello")); err != nil {
		t.Fatal(err)
	}

	time.Sleep(200 * time.Millisecond)
	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Errorf("expected output to contain hello, got %q", out)
	}
}

func TestSendMsgLoopSendsMessage(t *testing.T) {
	client, server := wsPair(t)
	var buf bytes.Buffer
	var mu sync.Mutex
	stdout = &safeWriter{w: &buf, mu: &mu}
	defer func() { stdout = os.Stdout }()

	r, w, _ := os.Pipe()
	oldStdin := stdin
	stdin = r
	defer func() { stdin = oldStdin }()

	current := []rune{}
	go sendMsgLoop(server, &current)

	fmt.Fprint(w, "hi\r")
	w.Close()

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := client.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "hi" {
		t.Errorf("expected hi, got %q", string(msg))
	}
}

func TestSendMsgLoopBackspace(t *testing.T) {
	client, server := wsPair(t)
	var buf bytes.Buffer
	var mu sync.Mutex
	stdout = &safeWriter{w: &buf, mu: &mu}
	defer func() { stdout = os.Stdout }()

	r, w, _ := os.Pipe()
	oldStdin := stdin
	stdin = r
	defer func() { stdin = oldStdin }()

	current := []rune{}
	go sendMsgLoop(server, &current)

	fmt.Fprint(w, "ab\x7f\r")
	w.Close()

	client.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := client.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	if string(msg) != "a" {
		t.Errorf("expected a, got %q", string(msg))
	}
}

func TestSendMsgLoopCtrlC(t *testing.T) {
	_, server := wsPair(t)
	var buf bytes.Buffer
	var mu sync.Mutex
	stdout = &safeWriter{w: &buf, mu: &mu}
	defer func() { stdout = os.Stdout }()

	r, w, _ := os.Pipe()
	oldStdin := stdin
	stdin = r
	defer func() { stdin = oldStdin }()

	current := []rune{}
	done := make(chan struct{})
	go func() {
		sendMsgLoop(server, &current)
		close(done)
	}()

	fmt.Fprint(w, "\x03")
	w.Close()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("sendMsgLoop did not exit on ctrl-c")
	}
}

type safeWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (s *safeWriter) Write(p []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.w.Write(p)
}
