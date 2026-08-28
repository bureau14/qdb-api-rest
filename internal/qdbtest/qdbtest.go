// Package qdbtest is the qdbd fixture shared by every test that dials a
// live cluster: the pair that scripts/tests/setup/start-services.sh
// starts, insecure on 2836 and secure on 2838, with the key files the
// script writes into the directory it runs from, the repository root.
// Nothing is skipped; a cluster that is down fails the test at once with
// the start recipe.
package qdbtest

import (
	"net"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

const (
	InsecureURI = "qdb://127.0.0.1:2836"
	SecureURI   = "qdb://127.0.0.1:2838"
)

// Require fails t with the start recipe when the node behind uri does not
// accept a TCP connection.
func Require(t testing.TB, uri string) {
	t.Helper()
	addr := strings.TrimPrefix(uri, "qdb://")
	conn, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatalf("qdbd not answering on %s; run: bash scripts/tests/setup/start-services.sh", addr)
	}
	_ = conn.Close()
}

// repoRoot is two directories up from this file.
func repoRoot() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..")
}

// ClusterPublicKeyFile is the secure cluster's public key file.
func ClusterPublicKeyFile() string { return filepath.Join(repoRoot(), "cluster_public.key") }

// UserSecurityFile is the user security file of the secure cluster's
// test user.
func UserSecurityFile() string { return filepath.Join(repoRoot(), "user_private.key") }
