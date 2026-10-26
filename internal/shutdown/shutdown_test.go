package shutdown

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestRegisterAndRun(t *testing.T) {
	// reset globals for test
	once = sync.Once{}
	handlers = nil
	done = make(chan struct{})
	called := false
	Register(func(context.Context) error {
		called = true
		return nil
	})
	if err := Run(context.Background(), time.Second); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("handler should be called")
	}
}
