package qdb

import (
	"context"
	"sync/atomic"
)

// budget is the process-wide ceiling on live sessions. A native session
// holds a unit from before its dial until its qdb_close has returned. A
// DuckDB call holds a unit for as long as it runs, and duck counts those
// units.
type budget struct {
	tokens chan struct{}
	duck   atomic.Int64 // the DuckDB part of the units in use, read by the collector without a lock
}

func newBudget(max int) *budget {
	return &budget{tokens: make(chan struct{}, max)}
}

// acquire takes a unit, waiting for one or for ctx to end.
func (b *budget) acquire(ctx context.Context) error {
	select {
	case b.tokens <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// acquireDuck takes a unit for a DuckDB call, waiting as acquire does.
func (b *budget) acquireDuck(ctx context.Context) error {
	if err := b.acquire(ctx); err != nil {
		return err
	}
	// Counted after the take, so duck never exceeds inUse.
	b.duck.Add(1)
	return nil
}

// releaseDuck gives a DuckDB call's unit back.
func (b *budget) releaseDuck() {
	// Uncounted before the release, so duck never exceeds inUse.
	b.duck.Add(-1)
	b.release()
}

func (b *budget) release()       { <-b.tokens }
func (b *budget) inUse() int     { return len(b.tokens) }
func (b *budget) max() int       { return cap(b.tokens) }
func (b *budget) duckInUse() int { return int(b.duck.Load()) }
