package httpapi

import (
	"encoding/json"
	"net/http"
)

// problemContentType is the media type of an RFC 9457 problem details
// body, the one error shape every v2 endpoint answers with.
const problemContentType = "application/problem+json"

// problem is the body: the status repeated, its standard text as the
// title, and the detail of this failure. type is omitted (about:blank,
// the status code's meaning) and instance is omitted (the request id is
// already a header).
type problem struct {
	Status int    `json:"status"`
	Title  string `json:"title"`
	Detail string `json:"detail,omitempty"`
}

// writeProblem answers status with a problem body carrying detail. Any
// header that belongs to the status (WWW-Authenticate, Retry-After) is
// set by the caller before this call.
func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", problemContentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(problem{Status: status, Title: http.StatusText(status), Detail: detail})
}
