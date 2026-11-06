package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// findPath is the find route.
const findPath = "/api/v2/find"

// handleFind answers the names of the entries the body's find expression
// matches, as {"entries":[...]}. An expression that matches nothing is 200
// with an empty list, and anything else the cluster answered is the
// caller's 400.
func handleFind(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	// A find expression is a line of query text, so it takes the query's
	// content types and cap.
	if !isQueryText(r.Header.Get("Content-Type")) {
		writeProblem(w, http.StatusUnsupportedMediaType, "Content-Type must be text/plain or application/sql")
		return
	}
	expr, ok := readBody(w, r)
	if !ok {
		return
	}
	names, err := qdb.ClusterFrom(ctx).Find(ctx, caller(r), string(expr))
	if err != nil {
		writeClusterError(ctx, w, err, http.StatusBadRequest)
		return
	}
	// The binding answers nil for no match, and nil would encode as null
	// where the body promises a list.
	if names == nil {
		names = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(entriesResponse{Entries: names})
}

// registerFindRoute serves the find route behind the bearer middleware.
func registerFindRoute(mux *http.ServeMux) {
	handleData(mux, "POST "+findPath, withCompression(requireBearer(http.HandlerFunc(handleFind))))
}
