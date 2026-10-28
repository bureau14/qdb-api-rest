package observe

import (
	"testing"

	"github.com/bureau14/qdb-api-rest/internal/build"
)

// TestNewMetricsExposesBuildInfo gathers the registry once and asserts
// the build-info series is there with its three labels, because the
// name and the labels are the wire shape dashboards read, and the e2e
// scrape sees only the name.
func TestNewMetricsExposesBuildInfo(t *testing.T) {
	families, err := NewMetrics().Registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	// Find the family by name, then read the label pairs and the value.
	// build.Version is "dev" under go test, so the assertion is on the
	// label names and the value, never on the version's text.
	for _, f := range families {
		if f.GetName() != "qdb_rest_build_info" {
			continue
		}
		if n := len(f.GetMetric()); n != 1 {
			t.Fatalf("qdb_rest_build_info has %d series, want 1", n)
		}
		m := f.GetMetric()[0]
		if v := m.GetGauge().GetValue(); v != 1 {
			t.Fatalf("qdb_rest_build_info = %v, want 1", v)
		}
		got := map[string]string{}
		for _, l := range m.GetLabel() {
			got[l.GetName()] = l.GetValue()
		}
		want := map[string]string{"version": build.Version, "commit": build.Commit, "build_mode": build.BuildMode}
		for k, v := range want {
			if got[k] != v {
				t.Fatalf("label %s = %q, want %q (labels %v)", k, got[k], v, got)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("labels %v, want exactly %v", got, want)
		}
		return
	}
	t.Fatal("qdb_rest_build_info is not in the exposition")
}
