package shutdown

import (
	"context"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

var (
	mu       sync.Mutex
	handlers []func(context.Context) error
	done     = make(chan struct{})
	once     sync.Once
)

func Register(fn func(context.Context) error) {
	mu.Lock()
	defer mu.Unlock()
	handlers = append(handlers, fn)
}

func Wait() <-chan os.Signal {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	return ch
}

func Run(ctx context.Context, timeout time.Duration) error {
	once.Do(func() { close(done) })
	c, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	mu.Lock()
	h := append([]func(context.Context) error{}, handlers...)
	mu.Unlock()
	var wg sync.WaitGroup
	for _, fn := range h {
		wg.Add(1)
		go func(f func(context.Context) error) {
			defer wg.Done()
			_ = f(c)
		}(fn)
	}
	ch := make(chan struct{})
	go func() {
		wg.Wait()
		close(ch)
	}()
	select {
	case <-ch:
		return nil
	case <-c.Done():
		return c.Err()
	}
}

func Done() <-chan struct{} {
	return done
}
