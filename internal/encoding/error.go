package encoding

import (
	"errors"
	"fmt"

	"github.com/apache/arrow-go/v18/arrow"
)

// ErrUnsupportedType is the encode error for a column whose Arrow type
// has no rendering: the binding's vocabulary can grow, and the wire
// refuses loudly rather than guess.
var ErrUnsupportedType = errors.New("unsupported type")

// unsupportedType wraps ErrUnsupportedType with the column and its type.
func unsupportedType(f arrow.Field) error {
	return fmt.Errorf("encoding: column %q has type %s: %w", f.Name, f.Type, ErrUnsupportedType)
}

// ErrInvalidRows: the body cannot be read as rows. The message names the
// row and the column.
var ErrInvalidRows = errors.New("invalid rows")
