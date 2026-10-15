package olap

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strconv"
	"strings"

	"github.com/duckdb/duckdb-go/v2"

	"github.com/bureau14/qdb-api-rest/internal/config"
)

// Credentials are what an instance attaches the cluster with: one node of
// the cluster, the user, and the cluster public key. An empty Username is
// the anonymous user, and an empty ClusterPublicKey is an insecure
// cluster.
type Credentials struct {
	Host, Port          string
	Username, SecretKey string
	ClusterPublicKey    string
}

// LogValue renders the credentials without the secret key.
func (c Credentials) LogValue() slog.Value {
	user := c.Username
	if user == "" {
		user = "(anonymous)"
	}
	return slog.GroupValue(
		slog.String("host", c.Host),
		slog.String("port", c.Port),
		slog.String("user", user),
		slog.Bool("secured", c.ClusterPublicKey != ""))
}

// ErrAttach is the error of an instance whose cluster attach failed. The
// cluster refused the attach or could not be reached, and qdb-duck's
// message does not say which.
var ErrAttach = errors.New("olap: attach failed")

// Instance is one in-memory DuckDB instance with the cluster attached as
// qdb under one user's credentials. Every connection of the instance sees
// that catalog, so an instance serves one user. It is safe for concurrent
// queries, each on its own connection.
type Instance struct {
	connector *duckdb.Connector
}

// dsn renders the duckdb block as the settings an instance opens with.
func dsn(cfg config.DuckDB) string {
	v := url.Values{}
	// DuckDB reads a bare byte count when it carries the b suffix.
	v.Set("memory_limit", strconv.FormatInt(cfg.MemoryLimit, 10)+"b")
	v.Set("threads", strconv.Itoa(cfg.Threads))
	// An in-memory instance spills to .tmp in the working directory when
	// temp_directory is unset, so it is always set, and the empty string
	// disables spilling.
	v.Set("temp_directory", cfg.SpillDirectory)
	v.Set("max_temp_directory_size", strconv.FormatInt(cfg.MaxSpillSize, 10)+"b")
	return "?" + v.Encode()
}

// quote renders s as a SQL string literal.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// secretName names the secret an instance attaches through. The secret
// is the instance's own, so one name serves every instance.
const secretName = "qdb_rest"

// secretOf is the CREATE SECRET statement that holds c.
func secretOf(c Credentials) string {
	// The values travel as quoted literals of a secret, because qdb-duck's
	// ATTACH string splits on every = and a QuasarDB key ends in base64
	// padding. Only the host, the port and the credentials travel, so
	// qdb-duck's own defaults apply to the timeout, the compression and
	// the encryption. The secret is TEMPORARY, so it lives in memory with
	// the instance and is never written to disk.
	var b strings.Builder
	fmt.Fprintf(&b, "CREATE TEMPORARY SECRET %s (TYPE quasar, HOST %s, PORT %s",
		secretName, quote(c.Host), quote(c.Port))
	// The anonymous user leaves out the user fields, and an insecure
	// cluster leaves out the cluster key.
	if c.Username != "" {
		fmt.Fprintf(&b, ", USER_NAME %s, USER_PRIVATE_KEY %s", quote(c.Username), quote(c.SecretKey))
	}
	if c.ClusterPublicKey != "" {
		fmt.Fprintf(&b, ", CLUSTER_PUBLIC_KEY %s", quote(c.ClusterPublicKey))
	}
	b.WriteString(")")
	return b.String()
}

// Open opens an instance under cfg and attaches the cluster as qdb with c.
// An attach that fails is ErrAttach with qdb-duck's message, and no
// instance is left open.
func Open(ctx context.Context, cfg config.DuckDB, c Credentials) (*Instance, error) {
	// The instance is in memory, so it holds nothing but the attach and
	// what one user's queries create. Three things fail on the way, and
	// a failure after the open closes what the open made:
	//
	//  1. open the connector under the duckdb block, which DuckDB judges
	//     here;
	//  2. on one connection, create the secret and attach through it;
	//  3. on a failed attach, close the connector and answer ErrAttach.

	// 1. open the connector
	connector, err := duckdb.NewConnector(dsn(cfg), nil)
	if err != nil {
		return nil, err
	}
	// 2. create the secret and attach. The attach is visible to every
	//    later connection of the instance, so this connection closes
	//    when it is done.
	if err := attach(ctx, connector, c); err != nil {
		// 3. close what the open made
		return nil, errors.Join(err, connector.Close())
	}
	return &Instance{connector: connector}, nil
}

// attach creates the secret that holds c and attaches the cluster as qdb
// through it, on one connection of connector. A failed attach is
// ErrAttach with qdb-duck's message.
func attach(ctx context.Context, connector *duckdb.Connector, c Credentials) error {
	conn, err := connector.Connect(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	exec := conn.(driver.ExecerContext)
	if _, err := exec.ExecContext(ctx, secretOf(c), nil); err != nil {
		return err
	}
	if _, err := exec.ExecContext(ctx, "ATTACH 'secret="+secretName+"' AS qdb (TYPE quasar)", nil); err != nil {
		return fmt.Errorf("%w: %w", ErrAttach, err)
	}
	return nil
}

// Close closes the instance. Its attached catalog and qdb-duck's sessions
// close with it.
func (i *Instance) Close() error {
	return i.connector.Close()
}
