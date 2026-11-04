package qdb

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// Find answers the entry names a find expression matches, as the user, in
// the cluster's order. An expression that matches nothing answers an empty
// list. It is retried once on a retryable failure, as a read is.
func (c *Cluster) Find(ctx context.Context, u User, expr string) ([]string, error) {
	var names []string
	err := c.ObservedCall(ctx, u, OpFind, func(s *Session) error {
		var err error
		names, err = s.session.Find().ExecuteString(expr)
		return err
	}, WithReadRetry())
	if err != nil {
		return nil, err
	}
	return names, nil
}

// Tagged answers the names of the entries that carry the tag, as the user,
// through Find. A tag no entry carries is IsTableNotFound, and a tag
// holding a single quote is ErrInvalidTag.
func (c *Cluster) Tagged(ctx context.Context, u User, tag string) ([]string, error) {
	// The find grammar has no escape for a single quote, so such a tag
	// cannot be named in an expression.
	if strings.Contains(tag, "'") {
		return nil, fmt.Errorf("%w: %q holds a single quote", ErrInvalidTag, tag)
	}
	// The tag goes through Find, because the find core also serves the v1
	// find query (docs/brief.md).
	names, err := c.Find(ctx, u, "find(tag='"+tag+"')")
	if err != nil {
		return nil, err
	}
	// The find answers an empty list for a tag no entry carries, whether or
	// not the tag's entry exists, so the not-found is raised here.
	if len(names) == 0 {
		return nil, fmt.Errorf("%w: no entry carries tag %q", qdbapi.ErrAliasNotFound, tag)
	}
	return names, nil
}
