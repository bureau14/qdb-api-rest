package v1

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"testing"

	qdbapi "github.com/bureau14/qdb-api-go/v3"

	"github.com/bureau14/qdb-api-rest/internal/qdbtest/cluster"
	"github.com/bureau14/qdb-api-rest/internal/qdbtest/table"
)

// tagsPaths are the two spellings of the v1 tag listing.
var tagsPaths = []string{"/api/v1/tags", "/api/tags"}

// getTags gets path with the query string under the bearer token, and
// answers the recorded response.
func getTags(ctx context.Context, h http.Handler, path, query, token string) *httptest.ResponseRecorder {
	if query != "" {
		path += "?" + query
	}
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp := httptest.NewRecorder()
	h.ServeHTTP(resp, req)
	return resp
}

// TestTags: on both clusters and at both spellings, the listing answers
// one unnamed table whose string column name lists every tag, a regex
// keeps the tags it matches, and a regex that matches nothing answers an
// empty column. A bad regex is 500 and a missing token 401.
func TestTags(t *testing.T) {
	cluster.Each(t, func(t *testing.T, f cluster.Fixture) {
		ctx, h := newServer(t, f.Cluster)
		token := mintAccess(t, ctx, f.User)
		tbl := table.Table{Name: "qdbtest_v1_tags", Columns: []table.Column{{Name: "c0", Type: qdbapi.TsColumnInt64}}}
		table.CreateAs(t, f.Cluster, f.User, tbl)
		tags := []string{"qdbtest_v1_tags_01", "qdbtest_v1_tags_02", "qdbtest_v1_tags_03"}
		attach := func(entry, tag string) {
			req := httptest.NewRequestWithContext(ctx, http.MethodPut, "/api/v2/tables/"+entry+"/tags/"+url.PathEscape(tag), nil)
			req.Header.Set("Authorization", "Bearer "+token)
			resp := httptest.NewRecorder()
			h.ServeHTTP(resp, req)
			if resp.Code != http.StatusNoContent {
				t.Fatalf("attach %s to %s: status %d: %s", tag, entry, resp.Code, resp.Body.String())
			}
		}
		for _, tag := range tags {
			table.RemoveTagOnCleanupAs(t, f.Cluster, f.User, tag)
			attach(tbl.Name, tag)
			// qdbd does not list a tag under the root by itself, so the test
			// attaches the root to the tag's entry, as the Grafana CI does.
			attach(tag, "$qdb.tagroot")
		}
		for _, path := range tagsPaths {
			// The shared cluster holds other tests' tags, so the full
			// listing is checked to contain this test's tags.
			resp := getTags(ctx, h, path, "", token)
			if resp.Code != http.StatusOK {
				t.Fatalf("%s: status %d: %s", path, resp.Code, resp.Body.String())
			}
			body := readOld(t, resp)
			if len(body.Tables) != 1 || body.Tables[0].Name != nil || len(body.Tables[0].Columns) != 1 {
				t.Fatalf("%s: want one unnamed table of one column: %s", path, resp.Body.String())
			}
			for _, tag := range tags {
				if !slices.Contains(body.Tables[0].Columns[0].Data, any(tag)) {
					t.Fatalf("%s: %s missing: %s", path, tag, resp.Body.String())
				}
			}
			for _, row := range []struct{ regex, body string }{
				{"^qdbtest_v1_tags_0[12]$", `{"tables":[{"columns":[{"data":["qdbtest_v1_tags_01","qdbtest_v1_tags_02"],"name":"name","type":"string"}]}]}` + "\n"},
				{"^qdbtest_v1_tags_none$", `{"tables":[{"columns":[{"data":[],"name":"name","type":"string"}]}]}` + "\n"},
			} {
				resp := getTags(ctx, h, path, "regex="+url.QueryEscape(row.regex), token)
				if resp.Code != http.StatusOK || resp.Body.String() != row.body {
					t.Fatalf("%s %s: status %d: %s", path, row.regex, resp.Code, resp.Body.String())
				}
			}
			errorShape(t, path+" bad regex", getTags(ctx, h, path, "regex="+url.QueryEscape("("), token), http.StatusInternalServerError, 0)
			errorShape(t, path+" no token", getTags(ctx, h, path, "", ""), http.StatusUnauthorized, http.StatusUnauthorized)
		}
	})
}
