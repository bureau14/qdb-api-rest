package v1

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/encoding"
	"github.com/bureau14/qdb-api-rest/internal/observe"
)

// tagRootFind is the old server's find expression for the tag listing. It
// answers the entries that carry the tag root, and qdbd attaches the root
// to no entry by itself, so a tag appears once a client attaches the root
// to the tag's entry.
const tagRootFind = "find(tag='$qdb.tagroot')"

// tagsTables answers the old server's tag listing: one table without a
// name, holding one string column called name.
func tagsTables(names []string) v1Tables {
	data := make([]any, len(names))
	for i, n := range names {
		data[i] = n
	}
	return v1Tables{Tables: []v1Table{{Columns: []v1Column{{Data: data, Name: "name", Type: "string"}}}}}
}

// matching answers the names re matches, in their order, or every name
// when re is nil.
func matching(names []string, re *regexp.Regexp) []string {
	if re == nil {
		return names
	}
	var kept []string
	for _, n := range names {
		if re.MatchString(n) {
			kept = append(kept, n)
		}
	}
	return kept
}

// answerTags runs the v1 request, writes its answer, and answers the
// status written.
func answerTags(w http.ResponseWriter, r *http.Request, v2 http.Handler) int {
	// 1. take the tokens, and answer 401 when there are none
	// 2. call the v2 find of the tag root, and answer its 401
	// 3. compile ?regex=, and answer 500 when it does not compile
	// 4. answer any other find failure, else the names the regex matches
	ctx := r.Context()

	// 1. A request without a token is refused as the v1 query refuses it.
	tokens := v1Tokens(r)
	if len(tokens) == 0 {
		writeV1JSON(w, http.StatusUnauthorized, v1AuthError{Code: http.StatusUnauthorized, Message: unauthenticated})
		return http.StatusUnauthorized
	}

	// 2. The find runs before the regex compiles, because the old server
	//    authenticated first and v1 learns the token's verdict only from v2.
	rec := callAuthorized(ctx, v2, tokens, v2FindPath, encoding.JSONContentType, []byte(tagRootFind))
	if rec.Code == http.StatusUnauthorized {
		return writeV2Failure(w, rec)
	}

	// 3. A bad regex is 500, and it wins over a failed find, as the old
	//    server compiled it before it ran the find.
	var re *regexp.Regexp
	if q := r.URL.Query(); q.Has("regex") {
		var err error
		if re, err = regexp.Compile(q.Get("regex")); err != nil {
			writeV1JSON(w, http.StatusInternalServerError, v1Error{Message: err.Error()})
			return http.StatusInternalServerError
		}
	}

	// 4. The v2 find answers 400 for a failed find, as the old server did.
	if rec.Code != http.StatusOK {
		return writeV2Failure(w, rec)
	}
	var found struct {
		Entries []string `json:"entries"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &found); err != nil {
		writeV1JSON(w, http.StatusInternalServerError, v1Error{Message: err.Error()})
		return http.StatusInternalServerError
	}
	writeV1JSON(w, http.StatusOK, tagsTables(matching(found.Entries, re)))
	return http.StatusOK
}

// tagsHandler answers the old server's tag listing through the v2 find of
// the tag root, filtered by the optional ?regex=. It answers the old
// {"tables"} body, and every failure in the old server's shapes. route is
// the path the request duration is recorded under.
func tagsHandler(v2 http.Handler, route string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		status := answerTags(w, r, v2)
		// The v2 find records under its own path, so one v1 request counts
		// once under each.
		observe.MetricsFrom(r.Context()).ObserveRequest(http.MethodGet, route, status, time.Since(start))
	})
}
