// Package qdbtest is the qdbd fixture shared by every test that dials a
// live cluster. The fixture is the pair that
// scripts/tests/setup/start-services.sh starts, insecure on 2836 and
// secure on 2838, with the key files the script writes into the
// directory it runs from, the repository root. Nothing is skipped, and a
// cluster that is down fails the test at once with the start recipe.
// Cluster describes both clusters of the test services, and Clusters lists
// them for a test that runs once per cluster.
package qdbtest

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/bureau14/qdb-api-rest/internal/config"
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

// SecureUser is the secure cluster's test user as a login body wants
// it: username and secret key. The fixture reads them out of the user
// security file because that is the only place the start script leaves
// them. The server itself never parses one, because callers send the
// pair and the server's own user is a file path the C API opens.
func SecureUser(t testing.TB) (username, secretKey string) {
	t.Helper()
	raw, err := os.ReadFile(UserSecurityFile())
	if err != nil {
		t.Fatal(err)
	}
	var u struct {
		Username  string `json:"username"`
		SecretKey string `json:"secret_key"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatal(err)
	}
	return u.Username, u.SecretKey
}

// Cluster is one cluster of the test services, as data: the name its
// subtest runs under, its URI, whether it is secured, its key files, and
// the user a test acts as there. The user is anonymous on the insecure
// cluster and the fixture's test user on the secure one.
type Cluster struct {
	Name string
	URI  string
	// Secured is stated rather than derived from PublicKeyFile, so a test
	// compares the server's answer with the fixture and not with the
	// server's own rule.
	Secured          bool
	PublicKeyFile    string
	UserSecurityFile string
	Username         string
	SecretKey        string
}

// Insecure is the insecure cluster, where a test acts as the anonymous
// user.
func Insecure() Cluster {
	return Cluster{Name: "insecure", URI: InsecureURI}
}

// Secure is the secure cluster, where a test acts as the fixture's test
// user and the REST API's own user is the same user.
func Secure(t testing.TB) Cluster {
	t.Helper()
	name, secret := SecureUser(t)
	return Cluster{
		Name:             "secure",
		URI:              SecureURI,
		Secured:          true,
		PublicKeyFile:    ClusterPublicKeyFile(),
		UserSecurityFile: UserSecurityFile(),
		Username:         name,
		SecretKey:        secret,
	}
}

// Clusters is both clusters, insecure first.
func Clusters(t testing.TB) []Cluster {
	t.Helper()
	return []Cluster{Insecure(), Secure(t)}
}

// Config is the default configuration bound to c: its URI, and on the
// secure cluster its public key file and the REST API's own user. It
// fails t with the start recipe when c does not answer. The cluster
// fixture and the package qdb tests both build their configurations
// here, so the binding rule has one copy.
func (c Cluster) Config(t testing.TB) config.Config {
	t.Helper()
	Require(t, c.URI)
	cfg := config.Default()
	cfg.Cluster.URI = c.URI
	cfg.Cluster.PublicKeyFile = c.PublicKeyFile
	cfg.Cluster.UserSecurityFile = c.UserSecurityFile
	return cfg
}
