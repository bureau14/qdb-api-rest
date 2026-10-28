package olap

import (
	"context"
	"database/sql/driver"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/duckdb/duckdb-go/v2"

	"github.com/bureau14/qdb-api-rest/internal/config"
	"github.com/bureau14/qdb-api-rest/internal/model"
)

// Credentials are what an instance attaches the cluster with: one node,
// the user and the cluster public key. An empty Username is the anonymous
// user, and an empty ClusterPublicKey means an insecure cluster.
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

// ErrAttach is the error of a failed cluster attach. The cluster either
// refused the attach or could not be reached, and qdb-duck's message does
// not say which.
var ErrAttach = errors.New("olap: attach failed")

// Instance is an in-memory DuckDB instance with the cluster attached as
// qdb under one user's credentials. Every connection of the instance sees
// that catalog, so an instance serves one user. Concurrent queries are
// safe, each on its own connection.
type Instance struct {
	connector *duckdb.Connector
}

// dsn renders the duckdb configuration block as the settings an instance
// opens with.
func dsn(cfg config.DuckDB) string {
	v := url.Values{}
	// DuckDB reads a bare byte count when it carries the b suffix.
	v.Set("memory_limit", strconv.FormatInt(cfg.MemoryLimit, 10)+"b")
	v.Set("threads", strconv.Itoa(cfg.Threads))
	// temp_directory is always set, because an in-memory instance without
	// it spills to .tmp in the working directory. The empty string
	// disables spilling.
	v.Set("temp_directory", cfg.SpillDirectory)
	v.Set("max_temp_directory_size", strconv.FormatInt(cfg.MaxSpillSize, 10)+"b")
	return "?" + v.Encode()
}

// quote renders a string as a SQL string literal.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// secretName names the secret an instance attaches through. Each
// instance holds its own secret, so one name serves them all.
const secretName = "qdb_rest"

// compressionWords and encryptionWords map the config vocabulary of
// cluster.compression and cluster.encryption onto qdb-duck's words for
// the same C API values. The config vocabulary admits these words only.
var (
	compressionWords = map[string]string{"none": "none", "balanced": "balanced"}
	encryptionWords  = map[string]string{"none": "none", "aes": "aes_gcm_256"}
)

// setGlobal renders one SET GLOBAL statement of a qdb-duck setting.
func setGlobal(name, value string) string {
	return "SET GLOBAL quasar_" + name + " = " + value
}

// millis renders a duration as the millisecond count qdb-duck takes.
func millis(d time.Duration) string {
	return strconv.FormatInt(int64(d/time.Millisecond), 10)
}

// settingsOf renders the qdb-duck globals an instance sets before its
// attach: the quasar block, and the cluster block's network knobs, so a
// qdb-duck session runs under the same wire policy as a native one.
// qdb-duck reads the globals once at the ATTACH, and an instance serves
// one user, so a global is that user's setting. The globals were chosen
// over the DSN, which DuckDB refuses them in.
func settingsOf(q config.Quasar, c config.Cluster) []string {
	// A zero knob is not sent, because 0 is "keep the client default" on
	// both sides. The pool size and the TTL are always sent, because a TTL
	// of 0 means no cache.
	out := []string{
		setGlobal("max_pool_size", strconv.Itoa(q.MaxPoolSize)),
		setGlobal("table_cache_ttl", millis(q.TableCacheTTL)),
	}
	if c.Timeout > 0 {
		out = append(out, setGlobal("timeout", millis(c.Timeout)))
	}
	if q.StabilizationMaxWait > 0 {
		out = append(out, setGlobal("stabilization_max_wait", millis(q.StabilizationMaxWait)))
	}
	if q.TransactionMaxWait > 0 {
		out = append(out, setGlobal("transaction_max_wait", millis(q.TransactionMaxWait)))
	}
	if c.MaxInBufferSize > 0 {
		out = append(out, setGlobal("max_in_buf_size", strconv.FormatInt(c.MaxInBufferSize, 10)))
	}
	if q.SoftMemoryLimit > 0 {
		out = append(out, setGlobal("soft_memory_limit", strconv.FormatInt(q.SoftMemoryLimit, 10)))
	}
	if c.ConnectionsPerAddress > 0 {
		out = append(out, setGlobal("connection_per_address_soft_limit", strconv.Itoa(c.ConnectionsPerAddress)))
	}
	if q.LoadBalancing {
		out = append(out, setGlobal("load_balancing", "true"))
	}
	// cluster.parallelism stays out: duckdb.threads already bounds a
	// scan's sessions, and qdb-duck's per-handle default is one.
	return append(out,
		setGlobal("compression", quote(compressionWords[c.Compression])),
		setGlobal("encryption", quote(encryptionWords[c.Encryption])))
}

// secretOf is the CREATE SECRET statement that holds the credentials.
func secretOf(c Credentials) string {
	// The credentials travel as quoted literals of a TEMPORARY secret, so
	// a user's text cannot reach the SQL, duckdb_secrets() shows the key
	// redacted, and nothing reaches disk. The ATTACH string was rejected,
	// because qdb-duck splits it on every = and a QuasarDB key ends in
	// base64 padding.
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

// Open opens an instance under the two configuration blocks and attaches
// the cluster as qdb with the credentials. A failed attach is ErrAttach
// with qdb-duck's message, and leaves no instance open. A setting the C
// API refuses fails the attach the same way.
func Open(ctx context.Context, duck config.DuckDB, cluster config.Cluster, c Credentials) (*Instance, error) {
	// The instance is in memory, so it holds only the attach and what one
	// user's queries create. A failure after the open closes what the
	// open made:
	//
	//  1. open the connector, where DuckDB judges the configuration;
	//  2. on one connection, set the globals, create the secret and
	//     attach through it;
	//  3. on a failed attach, close the connector and answer ErrAttach.

	// 1. open the connector
	connector, err := duckdb.NewConnector(dsn(duck), nil)
	if err != nil {
		return nil, err
	}
	// 2. set the globals, create the secret and attach. Every later
	//    connection sees the attach, so this connection closes when it is
	//    done.
	if err := attach(ctx, connector, settingsOf(duck.Quasar, cluster), c); err != nil {
		// 3. close what the open made
		return nil, errors.Join(err, connector.Close())
	}
	return &Instance{connector: connector}, nil
}

// attach sets the globals, creates the secret and attaches the cluster
// as qdb through it, on one connection of the connector. A failed attach
// is ErrAttach with qdb-duck's message.
func attach(ctx context.Context, connector *duckdb.Connector, settings []string, c Credentials) error {
	conn, err := connector.Connect(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()
	exec := conn.(driver.ExecerContext)
	// Only the ATTACH wraps ErrAttach, because only it dials. A refused
	// SET or secret is a configuration error and returns unchanged.
	for _, stmt := range settings {
		if _, err := exec.ExecContext(ctx, stmt, nil); err != nil {
			return err
		}
	}
	if _, err := exec.ExecContext(ctx, secretOf(c), nil); err != nil {
		return err
	}
	if _, err := exec.ExecContext(ctx, "ATTACH 'secret="+secretName+"' AS qdb (TYPE quasar)", nil); err != nil {
		return fmt.Errorf("%w: %w", ErrAttach, err)
	}
	return nil
}

// lent turns the reader's batches into a sequence, each batch lent until
// yield returns. A batch is one DuckDB vector, which is smaller than
// model.BatchRows and passes through unchanged. Setting model.BatchRows
// to the vector's size was rejected, because a native result would then
// take 32 times the messages. The reader owns each batch until it reads
// the next, so the receiver releases none. An error after the last batch
// is the last element, and a reader of no batches yields one empty batch
// of its schema.
func lent(rd array.RecordReader) iter.Seq2[arrow.RecordBatch, error] {
	return func(yield func(arrow.RecordBatch, error) bool) {
		yielded := false
		for rd.Next() {
			yielded = true
			if !yield(rd.RecordBatch(), nil) {
				return
			}
		}
		if err := rd.Err(); err != nil {
			yield(nil, err)
			return
		}
		if !yielded {
			rec := model.EmptyBatch(rd.Schema())
			defer rec.Release()
			yield(rec, nil)
		}
	}
}

// Query runs a query on a connection of its own and hands the sink its
// result as record batches, each lent until yield returns. A text of
// several statements runs every statement and answers the last
// statement's result, as DuckDB's C API does. An error before the sink
// runs is DuckDB's parse, bind or execution error. The sink's error is
// returned unchanged. A result of no batches is one empty batch of its
// schema.
func (i *Instance) Query(ctx context.Context, q string, sink func(iter.Seq2[arrow.RecordBatch, error]) error) error {
	// The connection lives for one query and closes when the sink
	// returns, so a request holds nothing of the instance afterwards:
	//
	//  1. open a connection and its Arrow interface;
	//  2. run the query, which fails before the sink runs;
	//  3. run the sink over the reader's batches;
	//  4. release the reader and close the connection, whatever the sink
	//     did.

	// 1. open a connection and its Arrow interface
	conn, err := i.connector.Connect(ctx)
	if err != nil {
		return err
	}
	// 4. close the connection, whatever the sink did
	defer func() { _ = conn.Close() }()
	a, err := duckdb.NewArrowFromConn(conn)
	if err != nil {
		return err
	}
	// 2. run the query. The binding materializes the result before it
	//    answers the reader, so DuckDB's parse, bind and execution errors
	//    arrive here, before anything reaches the sink. The binding runs
	//    the earlier statements of a text inside this call and discards
	//    their results, so their errors arrive here too. The reader itself
	//    fails only on an Arrow conversion or a cancelled ctx.
	rd, err := a.QueryContext(ctx, q)
	if err != nil {
		return err
	}
	// 4. release the reader, whatever the sink did
	defer rd.Release()
	// 3. run the sink
	return sink(lent(rd))
}

// Close closes the instance. Its attached catalog and qdb-duck's sessions
// close with it.
func (i *Instance) Close() error {
	return i.connector.Close()
}
