package qdb

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/config"
)

// silentListener accepts TCP connections and never answers, so the C API's
// connect handshake blocks inside cgo. It is the one way to exercise the
// failsafe honestly: a real thread stuck in a real C call, with no
// cancellation path to fake.
func silentListener(t *testing.T) (uri string, stop func()) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var held []net.Conn
	done := make(chan struct{})
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			held = append(held, conn)
			mu.Unlock()
		}
	}()
	stop = func() {
		close(done)
		_ = ln.Close()
		mu.Lock()
		for _, c := range held {
			_ = c.Close()
		}
		mu.Unlock()
	}
	_ = done
	return fmt.Sprintf("qdb://%s", ln.Addr().String()), stop
}

// failsafeConfig points the pool at the silent listener with the smallest
// deadlines the C API allows: call_timeout is a Go-side number, free to be
// 100ms; cluster.timeout is at its 1s floor, so the socket timeout cannot
// rescue the dial before the Go deadline fires.
func failsafeConfig(uri string, maxHandles int) config.Config {
	cfg := config.Default()
	cfg.Cluster.URI = uri
	cfg.Cluster.Timeout = time.Second
	cfg.Pool.MaxHandles = maxHandles
	cfg.Pool.PerUserMax = maxHandles
	cfg.Pool.CallTimeout = 100 * time.Millisecond
	cfg.Pool.Breaker.Failures = 1000 // out of the way of this test
	return cfg
}

// pollStats waits until want holds or the bound passes.
func pollStats(t *testing.T, c *Cluster, want func(Stats) bool) {
	t.Helper()
	deadline := time.Now().Add(15 * time.Second)
	for !want(c.Stats()) {
		if time.Now().After(deadline) {
			t.Fatalf("stats never reached the wanted state: %+v", c.Stats())
		}
		time.Sleep(2 * time.Millisecond)
	}
}

// TestCallTimeoutAbandonsAndRecovers: a dial that blocks in cgo returns
// ErrCallTimeout within ~call_timeout, is reported as wedged with its
// budget unit still held, and the unit comes back once the abandoned
// goroutine has closed the handle.
func TestCallTimeoutAbandonsAndRecovers(t *testing.T) {
	uri, stop := silentListener(t)
	defer stop()
	c := New(failsafeConfig(uri, 1), nil)
	defer closeCluster(t, c)

	start := time.Now()
	err := c.Call(context.Background(), anonymous, func(*Handle) error { return nil })
	elapsed := time.Since(start)
	if !errors.Is(err, ErrCallTimeout) {
		t.Fatalf("want ErrCallTimeout, got %v", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("call did not return promptly: %s", elapsed)
	}
	pollStats(t, c, func(s Stats) bool { return s.Wedged == 1 && s.BudgetInUse == 1 })
	pollStats(t, c, func(s Stats) bool { return s.Wedged == 0 && s.BudgetInUse == 0 })
}

// TestWedgedHandleDoesNotBlockThePool: two concurrent dials to the silent
// listener both wedge and both return promptly, so a wedged handle never
// serializes the pool behind it.
func TestWedgedHandleDoesNotBlockThePool(t *testing.T) {
	uri, stop := silentListener(t)
	defer stop()
	c := New(failsafeConfig(uri, 2), nil)
	defer closeCluster(t, c)

	start := time.Now()
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() {
			if err := c.Call(context.Background(), anonymous, func(*Handle) error { return nil }); !errors.Is(err, ErrCallTimeout) {
				t.Errorf("want ErrCallTimeout, got %v", err)
			}
		})
	}
	wg.Wait()
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("two wedged dials were serialized: %s", elapsed)
	}
	pollStats(t, c, func(s Stats) bool { return s.Wedged == 0 && s.BudgetInUse == 0 })
}
