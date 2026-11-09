package v1

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// v1Query is the old server's query body.
type v1Query struct {
	Query string `json:"query"`
}

// v2QueryPath and v2FindPath are the v2 endpoints the v1 query calls.
const (
	v2QueryPath = "/api/v2/query"
	v2FindPath  = "/api/v2/find"
)

// callV2 posts the text body to the v2 path under the token, asking for
// the media type, and answers the recorded response.
func callV2(ctx context.Context, v2 http.Handler, path, token, accept string, body []byte) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	// The inner request carries the outer context, because the v2 handler
	// reads the logger, the cluster and the keychain from it.
	in, err := http.NewRequestWithContext(ctx, http.MethodPost, path, bytes.NewReader(body))
	if err != nil {
		// Unreachable, because the method and the paths are constants.
		rec.WriteHeader(http.StatusInternalServerError)
		return rec
	}
	// No Accept-Encoding, because the v1 route decodes the v2 answer and
	// compresses only the body it writes itself.
	in.Header.Set("Content-Type", "text/plain")
	in.Header.Set("Accept", accept)
	in.Header.Set("Authorization", "Bearer "+token)
	v2.ServeHTTP(rec, in)
	return rec
}

// callAuthorized calls the v2 path under each token in turn and answers
// the first response that is not 401, or the last 401.
func callAuthorized(ctx context.Context, v2 http.Handler, tokens []string, path, accept string, body []byte) *httptest.ResponseRecorder {
	var rec *httptest.ResponseRecorder
	for _, t := range tokens {
		rec = callV2(ctx, v2, path, t, accept, body)
		// A 401 answers before the cluster runs anything, so the next token
		// runs the query once.
		if rec.Code != http.StatusUnauthorized {
			return rec
		}
	}
	return rec
}

// writeV2Failure answers a v2 failure in the old server's shape: 401 as
// {"code","message"}, 400 and 413 as 400 {"message"}, and anything else
// as 500 {"message"}. The message is the problem's detail. It answers the
// status written.
func writeV2Failure(w http.ResponseWriter, rec *httptest.ResponseRecorder) int {
	var problem struct {
		Detail string `json:"detail"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &problem) != nil || problem.Detail == "" {
		problem.Detail = http.StatusText(rec.Code)
	}
	switch rec.Code {
	case http.StatusUnauthorized:
		writeV1JSON(w, rec.Code, v1AuthError{Code: rec.Code, Message: problem.Detail})
		return rec.Code
	// The old server had no cap, so an oversized query is the caller's 400.
	case http.StatusBadRequest, http.StatusRequestEntityTooLarge:
		writeV1JSON(w, http.StatusBadRequest, v1Error{Message: problem.Detail})
		return http.StatusBadRequest
	default:
		// 503 is 500, because the old server answered 500 when it could not
		// lease a session.
		writeV1JSON(w, http.StatusInternalServerError, v1Error{Message: problem.Detail})
		return http.StatusInternalServerError
	}
}

// readFindTables reads the v2 find's {"entries"} into the old find's
// body: one table per name, with columns null.
func readFindTables(r io.Reader) (v1Tables, error) {
	var found struct {
		Entries []string `json:"entries"`
	}
	if err := json.NewDecoder(r).Decode(&found); err != nil {
		return v1Tables{}, err
	}
	tables := make([]v1Table, len(found.Entries))
	for i, name := range found.Entries {
		tables[i] = v1Table{Name: name}
	}
	return v1Tables{Tables: tables}, nil
}

// answerQuery runs the v1 request, writes its answer, and answers the
// status written.
func answerQuery(w http.ResponseWriter, r *http.Request, v2 http.Handler) int {
	// 1. take the tokens, and answer 401 when there are none
	// 2. read the body and decode {"query"}
	// 3. call the v2 find for a find query, or the v2 query for Arrow
	// 4. convert the answer into the old body, or the failure into the old shapes
	ctx := r.Context()

	// 1. The old server authenticated before it bound the body.
	tokens := v1Tokens(r)
	if len(tokens) == 0 {
		writeV1JSON(w, http.StatusUnauthorized, v1AuthError{Code: http.StatusUnauthorized, Message: unauthenticated})
		return http.StatusUnauthorized
	}

	// 2. The body is read under any Content-Type, as the v1 login reads it.
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeV1JSON(w, http.StatusBadRequest, v1AuthError{Code: http.StatusBadRequest, Message: err.Error()})
		return http.StatusBadRequest
	}
	// 2. 422 with code 602 is the old server's required-body error.
	if len(body) == 0 {
		writeV1JSON(w, http.StatusUnprocessableEntity, v1AuthError{Code: 602, Message: "query in body is required"})
		return http.StatusUnprocessableEntity
	}
	// 2. 400 with code 400 is the old server's parse error, and a bare
	//    string is one.
	var q v1Query
	if err := json.Unmarshal(body, &q); err != nil {
		writeV1JSON(w, http.StatusBadRequest, v1AuthError{Code: http.StatusBadRequest, Message: err.Error()})
		return http.StatusBadRequest
	}

	// 3. The prefix is raw and case-sensitive, as the old server matched
	//    it, so leading whitespace or FIND reaches the native engine.
	path, accept, convert := v2QueryPath, encoding.ArrowContentType, readV1Tables
	if strings.HasPrefix(q.Query, "find") {
		path, accept, convert = v2FindPath, encoding.JSONContentType, readFindTables
	}
	rec := callAuthorized(ctx, v2, tokens, path, accept, []byte(q.Query))
	if rec.Code != http.StatusOK {
		return writeV2Failure(w, rec)
	}
	tables, err := convert(rec.Body)
	if err != nil {
		// 4. A type no cell writer knows is 500, as v2 answers it.
		writeV1JSON(w, http.StatusInternalServerError, v1Error{Message: err.Error()})
		return http.StatusInternalServerError
	}
	writeV1JSON(w, http.StatusOK, tables)
	return http.StatusOK
}

// queryHandler answers the old server's query through the v2 query, or
// through the v2 find for a query that begins with find. It answers the
// old {"tables"} body, and every failure in the old server's shapes.
// route is the path the request duration is recorded under.
func queryHandler(v2 http.Handler, route string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		status := answerQuery(w, r, v2)
		// The v2 route records under its own path, so one v1 request counts
		// once under each.
		observe.MetricsFrom(r.Context()).ObserveRequest(http.MethodPost, route, status, time.Since(start))
	})
}
