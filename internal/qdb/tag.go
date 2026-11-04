package qdb

import (
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
