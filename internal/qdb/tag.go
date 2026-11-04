package qdb

import (
	"context"
	"errors"

	qdbapi "github.com/bureau14/qdb-api-go/v3"
)

// IsTagAlreadySet reports whether err means the entry already carries the
// tag. The HTTP layer reads it as success.
func IsTagAlreadySet(err error) bool {
	return errors.Is(err, qdbapi.ErrTagAlreadySet)
}

// IsTagNotSet reports whether err means the entry does not carry the tag.
// The HTTP layer reads it as success.
func IsTagNotSet(err error) bool {
	return errors.Is(err, qdbapi.ErrTagNotSet)
}

// AttachTag attaches the tag to the named entry as the user. A tag the
// entry already carries is IsTagAlreadySet, and an unknown entry is
// IsTableNotFound. It is never retried.
func (c *Cluster) AttachTag(ctx context.Context, u User, name, tag string) error {
	// No WithReadRetry, because only reads are safe to run twice.
	return c.ObservedCall(ctx, u, OpAttachTag, func(s *Session) error { return s.session.Table(name).AttachTag(tag) })
}

// DetachTag detaches the tag from the named entry as the user. A tag the
// entry does not carry is IsTagNotSet, and an unknown entry is
// IsTableNotFound. The tag's own entry stays. It is never retried.
func (c *Cluster) DetachTag(ctx context.Context, u User, name, tag string) error {
	return c.ObservedCall(ctx, u, OpDetachTag, func(s *Session) error { return s.session.Table(name).DetachTag(tag) })
}
