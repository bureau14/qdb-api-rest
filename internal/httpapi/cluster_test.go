// This file holds the cluster routes' test helpers. Their good path is the
// round trip (roundtrip_test.go), and their error rows live in
// errors_test.go.
package httpapi

import (
	"net/http"
	"net/http/httptest"
)

// cluster gets every node's statistics.
func (s server) cluster() *httptest.ResponseRecorder {
	return s.send(http.MethodGet, clusterPath, "", map[string]string{"Authorization": "Bearer " + s.token})
}

// node gets the statistics of the node at addr.
func (s server) node(addr string) *httptest.ResponseRecorder {
	return s.send(http.MethodGet, clusterPath+"/nodes/"+addr, "", map[string]string{"Authorization": "Bearer " + s.token})
}
