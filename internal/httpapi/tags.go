package httpapi

import (
	"net/http"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

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

// registerTagRoutes serves the tag routes behind the bearer middleware.
// The mux patterns fix method and path.
func registerTagRoutes(mux *http.ServeMux) {
	handleData(mux, "PUT "+tablesPath+"/{name}/tags/{tag}", withCompression(requireBearer(http.HandlerFunc(handleAttachTag))))
	handleData(mux, "DELETE "+tablesPath+"/{name}/tags/{tag}", withCompression(requireBearer(http.HandlerFunc(handleDetachTag))))
}
