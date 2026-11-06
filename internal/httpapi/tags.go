package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// tagsPath is the tag collection. A tag is tagsPath + "/" + its name.
const tagsPath = "/api/v2/tags"

// entriesResponse is the body of GET /api/v2/tags/{tag} and of POST
// /api/v2/find. The names sit under a key, so a field can join them later
// without a break.
type entriesResponse struct {
	Entries []string `json:"entries"`
}

// handleAttachTag attaches the path's tag to the path's table and answers
// 204, also when the table already carries it. A table the cluster does
// not know is 404.
func handleAttachTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	err := qdb.ClusterFrom(ctx).AttachTag(ctx, caller(r), r.PathValue("name"), r.PathValue("tag"))
	switch {
	// PUT is idempotent, so a retry after a lost answer succeeds.
	case err == nil || qdb.IsTagAlreadySet(err):
		w.WriteHeader(http.StatusNoContent)
	case qdb.IsTableNotFound(err):
		writeProblem(w, http.StatusNotFound, err.Error())
	default:
		writeClusterError(ctx, w, err, http.StatusBadRequest)
	}
}

// handleDetachTag detaches the path's tag from the path's table and
// answers 204, also when the table does not carry it. A table the cluster
// does not know is 404.
func handleDetachTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	err := qdb.ClusterFrom(ctx).DetachTag(ctx, caller(r), r.PathValue("name"), r.PathValue("tag"))
	switch {
	case err == nil || qdb.IsTagNotSet(err):
		w.WriteHeader(http.StatusNoContent)
	case qdb.IsTableNotFound(err):
		writeProblem(w, http.StatusNotFound, err.Error())
	default:
		writeClusterError(ctx, w, err, http.StatusBadRequest)
	}
}

// handleTagged answers the names of the entries that carry the path's tag.
// A tag no entry carries is 404, a tag with a single quote is 400, and
// anything else the cluster answered is the caller's 400.
func handleTagged(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	names, err := qdb.ClusterFrom(ctx).Tagged(ctx, caller(r), r.PathValue("tag"))
	switch {
	case err == nil:
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(entriesResponse{Entries: names})
	// The tag is the route's resource, as a table is on the read and the
	// delete.
	case qdb.IsTableNotFound(err):
		writeProblem(w, http.StatusNotFound, err.Error())
	case errors.Is(err, qdb.ErrInvalidTag):
		writeProblem(w, http.StatusBadRequest, err.Error())
	default:
		writeClusterError(ctx, w, err, http.StatusBadRequest)
	}
}

// registerTagRoutes serves the tag routes behind the bearer middleware.
// The mux patterns fix method and path.
func registerTagRoutes(mux *http.ServeMux) {
	handleData(mux, "PUT "+tablesPath+"/{name}/tags/{tag}", withCompression(requireBearer(http.HandlerFunc(handleAttachTag))))
	handleData(mux, "DELETE "+tablesPath+"/{name}/tags/{tag}", withCompression(requireBearer(http.HandlerFunc(handleDetachTag))))
	handleData(mux, "GET "+tagsPath+"/{tag}", withCompression(requireBearer(http.HandlerFunc(handleTagged))))
}
