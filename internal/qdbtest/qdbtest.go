// Package qdbtest is the qdbd fixture shared by every test that dials a
// live cluster: the pair that scripts/tests/setup/start-services.sh
// starts, insecure on 2836 and secure on 2838, with the key files the
// script writes into the directory it runs from, the repository root.
// Nothing is skipped; a cluster that is down fails the test at once with
// the start recipe.
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

// TrafficToSecure is the environment variable that moves every test
// bound to the insecure daemon onto the secure one, with the secure
// daemon's key files, so the insecure daemon idles. It is the switch of
// the experiment that tells whether the insecure daemon's death on the
// Windows agents follows the tests' traffic or the instance
// (docs/ci-qdbd-logs-plan.md while it is alive). scripts/cicd/30.test.sh
// sets it on Windows.
const TrafficToSecure = "QDBTEST_TRAFFIC_TO_SECURE"

// BindInsecure points cfg at the insecure daemon, or at the secure one
// when TrafficToSecure is set, and fails t when that daemon is down.
func BindInsecure(t testing.TB, cfg *config.Config) {
	t.Helper()
	if os.Getenv(TrafficToSecure) != "" {
		BindSecure(t, cfg)
		return
	}
	cfg.Cluster.URI = InsecureURI
	Require(t, cfg.Cluster.URI)
}

// BindSecure points cfg at the secure daemon with the fixture's test user
// as the REST API's own user, and fails t when it is down.
func BindSecure(t testing.TB, cfg *config.Config) {
	t.Helper()
	cfg.Cluster.URI = SecureURI
	cfg.Cluster.PublicKeyFile = ClusterPublicKeyFile()
	cfg.Cluster.UserSecurityFile = UserSecurityFile()
	Require(t, cfg.Cluster.URI)
}

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
// it: username and secret key. They are read out of the user security
// file only because that is the one place the start script leaves them;
// the server itself never parses one (callers send the pair, and the
// server's own user is a file path the C API opens).
func SecureUser(t testing.TB) (username, secretKey string) {
	t.Helper()
	username, secretKey, err := readSecureUser()
	if err != nil {
		t.Fatal(err)
	}
	return username, secretKey
}

func readSecureUser() (username, secretKey string, err error) {
	raw, err := os.ReadFile(UserSecurityFile())
	if err != nil {
		return "", "", err
	}
	var u struct {
		Username  string `json:"username"`
		SecretKey string `json:"secret_key"`
	}
	if err := json.Unmarshal(raw, &u); err != nil {
		return "", "", err
	}
	return u.Username, u.SecretKey, nil
}

// Caller is the user the tests call the insecure cluster as: the
// anonymous user, or the secure cluster's test user under
// TrafficToSecure, because the secure daemon refuses an anonymous login.
// It panics on an unreadable user security file, which the start script
// writes, so that call sites without a testing.TB can use it.
func Caller() (username, secretKey string) {
	if os.Getenv(TrafficToSecure) == "" {
		return "", ""
	}
	username, secretKey, err := readSecureUser()
	if err != nil {
		panic("qdbtest.Caller: " + err.Error())
	}
	return username, secretKey
}
