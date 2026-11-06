// This file holds the test helpers of the tag routes and the find route.
// Their good path is the round trip (roundtrip_test.go), and their error
// rows live in errors_test.go.
package httpapi

import (
	"net/http"
	"net/http/httptest"
)

// attachTag puts the tag on the named table.
func (s server) attachTag(name, tag string) *httptest.ResponseRecorder {
	return s.send(http.MethodPut, tablesPath+"/"+name+"/tags/"+tag, "", map[string]string{"Authorization": "Bearer " + s.token})
}

// detachTag deletes the tag from the named table.
func (s server) detachTag(name, tag string) *httptest.ResponseRecorder {
	return s.send(http.MethodDelete, tablesPath+"/"+name+"/tags/"+tag, "", map[string]string{"Authorization": "Bearer " + s.token})
}

// tagged gets the entries that carry the tag.
func (s server) tagged(tag string) *httptest.ResponseRecorder {
	return s.send(http.MethodGet, tagsPath+"/"+tag, "", map[string]string{"Authorization": "Bearer " + s.token})
}

// find posts the find expression.
func (s server) find(expr string) *httptest.ResponseRecorder {
	return s.post(findPath, expr, map[string]string{"Authorization": "Bearer " + s.token})
}
