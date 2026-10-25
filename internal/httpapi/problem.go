package httpapi

import (
	"encoding/json"
	"net/http"
)

// problemContentType is the media type of an RFC 9457 problem details
// body. Every v2 endpoint answers its errors in this shape.
const problemContentType = "application/problem+json"

// problem is the RFC 9457 body. It repeats the status, uses the status
// text as the title and describes this failure in the detail. It omits
// type, because about:blank already means the status code's meaning,
// and it omits instance, because the request id is already a header.
// The name comes from RFC 9457 and refers to the wire shape only. A Go
// call that fails returns an error, never a problem.
type problem struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// writeProblem writes the status and a problem body with the given
// detail. The caller sets any header that belongs to the status
// (WWW-Authenticate, Retry-After) before the call.
func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Status: status, Title: http.StatusText(status), Detail: detail})
}
