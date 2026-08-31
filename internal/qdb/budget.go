package qdb

import "context"

// budget is the process-wide ceiling on live sessions: a unit is taken
// before a user pool dials and released when the session is actually
// closed, so a session keeps counting until its qdb_close has returned.
type budget struct {
	tokens chan struct{}
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

func (b *budget) release()   { <-b.tokens }
func (b *budget) inUse() int { return len(b.tokens) }
func (b *budget) max() int   { return cap(b.tokens) }
