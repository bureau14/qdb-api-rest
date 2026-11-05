package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"slices"

	"github.com/bureau14/qdb-api-rest/internal/qdb"
)

// clusterPath is the route of the cluster's statistics.
const clusterPath = "/api/v2/cluster"

// nodeResponse is one node as both cluster routes answer it: the
// binding's statistics with the node's address as id.
type nodeResponse struct {
	ID string `json:"id"`
	// encoding/json writes the embedded struct's fields beside id, so a
	// node is one flat object.
	qdb.NodeStats
}

// clusterResponse is the cluster route's body: every node, sorted by id.
type clusterResponse struct {
	Nodes []nodeResponse `json:"nodes"`
}

// writeNodeError answers a statistics failure: 404 for ErrNodeNotFound,
// 503 for ErrNodeUnreachable, and writeClusterError at 400 for the rest.
func writeNodeError(ctx context.Context, w http.ResponseWriter, err error) {
	switch {
	// The node is the route's resource, so its handler refines the caller's
	// 400 here and writeClusterError never sees it.
	case errors.Is(err, qdb.ErrNodeNotFound):
		writeProblem(w, http.StatusNotFound, err.Error())
	// No Retry-After, because nothing here knows when the node returns.
	case errors.Is(err, qdb.ErrNodeUnreachable):
		writeProblem(w, http.StatusServiceUnavailable, err.Error())
	default:
		writeClusterError(ctx, w, err, http.StatusBadRequest)
	}
}

// handleCluster answers every node's statistics, sorted by address.
func handleCluster(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	stats, err := qdb.ClusterFrom(ctx).Statistics(ctx, caller(r))
	if err != nil {
		writeNodeError(ctx, w, err)
		return
	}
	// The binding answers a map, so the handler sorts by address to answer
	// the same order on every call.
	nodes := make([]nodeResponse, 0, len(stats))
	for _, addr := range slices.Sorted(maps.Keys(stats)) {
		nodes = append(nodes, nodeResponse{ID: addr, NodeStats: stats[addr]})
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(clusterResponse{Nodes: nodes})
}

// registerClusterRoutes mounts the cluster routes behind the bearer edge.
func registerClusterRoutes(mux *http.ServeMux) {
	handleData(mux, "GET "+clusterPath, withCompression(requireBearer(http.HandlerFunc(handleCluster))))
}
