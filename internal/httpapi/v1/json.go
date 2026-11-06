package v1

import (
	"encoding/json"
	"net/http"
)

// v1Error is the old server's error body.
type v1Error struct {
	Message string `json:"message"`
}

// v1AuthError is the old server's 401, and the body of its request
// errors: the status as code, and the message.
type v1AuthError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// writeV1JSON writes the status and v as the old server encoded its
// bodies: application/json, HTML characters unescaped, and a trailing
// newline.
func writeV1JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
