package httpapi

import (
	"mime"
	"strings"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
)

// encoders maps each media type the query endpoint serves to its encoder.
var encoders = map[string]encoding.Encoder{
	encoding.JSONContentType:   encoding.JSON{},
	encoding.NDJSONContentType: encoding.NDJSON{},
	encoding.CSVContentType:    encoding.CSV{},
	encoding.ArrowContentType:  encoding.Arrow{},
}

// negotiate picks the encoder for an Accept header: the listed media
// ranges are read in order and the first one an encoder matches wins.
// */*, an absent header and no match all mean JSON. q weights are not
// read: a client that wants a format names it.
func negotiate(accept string) encoding.Encoder {
	for rng := range strings.SplitSeq(accept, ",") {
		// ParseMediaType lowercases the type and strips its parameters
		// (q included), so the lookup is by media type alone.
		mt, _, err := mime.ParseMediaType(strings.TrimSpace(rng))
		if err != nil {
			continue
		}
		if e, ok := encoders[mt]; ok {
			return e
		}
	}
	return encoding.JSON{}
}
