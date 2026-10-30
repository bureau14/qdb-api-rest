// Package config loads the server configuration. Every key is defined
// once, as a field of Config with its yaml tag, and reaches the process
// through three layers named after that key path: the YAML file
// (listen.http), the environment (QDB_REST_LISTEN_HTTP) and the command
// line (--listen-http). The environment overrides the file, explicitly
// passed flags override both, and the built-in defaults sit below all
// three. Values in the file support ${VAR} environment interpolation, so
// secrets can be injected without living on disk.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-viper/mapstructure/v2"
	"github.com/knadh/koanf/parsers/yaml"
	"github.com/knadh/koanf/providers/confmap"
	"github.com/knadh/koanf/providers/rawbytes"
	"github.com/knadh/koanf/v2"
	yamlv3 "gopkg.in/yaml.v3"
)

// ErrVersionRequested is returned by Load when the --version flag is
// passed. The caller prints its build metadata and exits. The metadata
// lives in package main, injected via -ldflags, so this package returns
// a sentinel and does not handle the flag itself.
var ErrVersionRequested = errors.New("version requested")

// Listen holds the listener addresses and the drain deadline. The HTTP
// listener is off unless an address is set, and it is the only one that
// can be off (ADR-0001). The deadline sits with the listeners it drains.
// A shutdown block of its own would hold one key, and a server block
// would hold nothing else.
type Listen struct {
	HTTP          string        `yaml:"http" help:"HTTP listener address; off unless set"`
	HTTPS         string        `yaml:"https" help:"HTTPS listener address"`
	Flight        string        `yaml:"flight" help:"Flight SQL listener address (gRPC over TLS)"`
	ShutdownGrace time.Duration `yaml:"shutdown_grace" help:"deadline for draining in-flight calls on every listener and the session pool on SIGTERM"`
}

// TLS points the HTTPS listener at a PEM certificate/key pair. Both empty
// means an ephemeral self-signed certificate is generated at startup.
type TLS struct {
	Certificate string `yaml:"certificate" help:"PEM certificate for the HTTPS listener, set together with tls.private_key"`
	PrivateKey  string `yaml:"private_key" help:"PEM private key for the HTTPS listener"`
}

// Log selects the log output shape and how much of a query's statement
// a debug line keeps.
type Log struct {
	Level             string `yaml:"level" help:"debug | info | warn | error"`
	Format            string `yaml:"format" help:"json | text"`
	StatementMaxBytes int    `yaml:"statement_max_bytes" help:"bytes of a query's statement a debug line keeps; 0 keeps it whole"`
}

// Cluster binds the process to one cluster. It holds the cluster URI, the
// cluster public key file, the REST API's own user and the per-session C
// API knobs. The REST API's user is a user security file with a username
// and a secret key, and an empty path means anonymous. A zero knob means
// the C API default. Key material comes from files only, in the form
// QuasarDB's tooling produces, so no secret sits in the YAML. Callers'
// credentials arrive through the API.
type Cluster struct {
	URI                   string        `yaml:"uri" help:"cluster URI, comma-separated for several nodes"`
	PublicKeyFile         string        `yaml:"public_key_file" help:"cluster public key file; empty means an insecure cluster"`
	UserSecurityFile      string        `yaml:"user_security_file" help:"user security file of the REST API's own user"`
	Compression           string        `yaml:"compression" help:"client-side C API compression: none | balanced"`
	Encryption            string        `yaml:"encryption" help:"client-cluster traffic encryption: none | aes"`
	Timeout               time.Duration `yaml:"timeout" help:"C API socket timeout per session, whole seconds; 0 leaves the Go API's default"`
	MaxInBufferSize       int64         `yaml:"max_in_buffer_size" help:"client input buffer cap per session, in bytes; 0 means the C API default"`
	Parallelism           int           `yaml:"parallelism" help:"C API worker threads per session; 0 means the C API default"`
	ConnectionsPerAddress int           `yaml:"connections_per_address" help:"soft limit on connections per node address, per session; 0 means the C API default"`
}

// LogValue renders the binding compactly for the startup log.
func (c Cluster) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("uri", c.URI),
		slog.String("public_key_file", c.PublicKeyFile),
		slog.String("user_security_file", c.UserSecurityFile),
		slog.String("compression", c.Compression),
		slog.String("encryption", c.Encryption),
		slog.Duration("timeout", c.Timeout))
}

// Breaker sizes the per-cluster circuit breaker.
type Breaker struct {
	Failures int           `yaml:"failures" help:"consecutive retryable failures that open the breaker"`
	OpenFor  time.Duration `yaml:"open_for" help:"how long the breaker stays open before one call is let through"`
}

// Pool sizes the session pool: the process-wide budget, the per-user cap,
// and the ages at which sessions are closed.
type Pool struct {
	MaxSessions int           `yaml:"max_sessions" help:"sessions this process may hold across all users"`
	PerUserMax  int           `yaml:"per_user_max" help:"sessions one user may hold"`
	IdleTimeout time.Duration `yaml:"idle_timeout" help:"a session unused this long is closed; a user pool empty this long is evicted"`
	MaxLifetime time.Duration `yaml:"max_lifetime" help:"a session older than this is closed on return"`
	Breaker     Breaker       `yaml:"breaker"`
}

// Status configures the readiness probe.
type Status struct {
	ReadinessQuery string `yaml:"readiness_query" help:"query the readiness probe runs after its dial"`
}

// Argon2id sets the cost of deriving token keys from passphrases. Each
// attacker guess pays this cost. Every instance behind a load balancer
// must run the same values, because changing any of them re-derives
// every key and behaves as a key rotation.
type Argon2id struct {
	Time        int `yaml:"time" help:"argon2id passes over the memory"`
	MemoryMiB   int `yaml:"memory_mib" help:"argon2id memory cost, in MiB"`
	Parallelism int `yaml:"parallelism" help:"argon2id lanes"`
}

// Auth configures the tokens: the passphrases their keys derive from, the
// derivation cost, and the two token lifetimes. The passphrases are a
// rolling list, where the first entry mints and the rest verify. An
// empty list means an ephemeral key per start, so tokens survive neither
// a restart nor a second instance behind a load balancer.
type Auth struct {
	TokenSecrets []string      `yaml:"token_secrets" help:"rolling token passphrases, first mints and the rest verify; comma-separated as a flag or variable"`
	Argon2id     Argon2id      `yaml:"argon2id"`
	AccessTTL    time.Duration `yaml:"access_ttl" help:"validity of an access token from its login"`
	RefreshTTL   time.Duration `yaml:"refresh_ttl" help:"validity of a refresh token from the login or refresh that minted it"`
}

// LogValue renders the auth block without its secrets.
func (a Auth) LogValue() slog.Value {
	return slog.GroupValue(
		slog.Int("token_secrets", len(a.TokenSecrets)),
		slog.Int("argon2id_time", a.Argon2id.Time),
		slog.Int("argon2id_memory_mib", a.Argon2id.MemoryMiB),
		slog.Int("argon2id_parallelism", a.Argon2id.Parallelism),
		slog.Duration("access_ttl", a.AccessTTL),
		slog.Duration("refresh_ttl", a.RefreshTTL))
}

// Quasar tunes the qdb-duck sessions behind one DuckDB instance, which
// the instance sets as qdb-duck's quasar_* globals before its attach. A
// zero wait or size leaves the C API default. The TTL and the pool size
// have no zero rule: 0 is a value for the TTL (no cache) and refused
// for the pool size.
type Quasar struct {
	MaxPoolSize          int           `yaml:"max_pool_size" help:"qdbd sessions qdb-duck pools behind one instance, 1 to 1024"`
	TableCacheTTL        time.Duration `yaml:"table_cache_ttl" help:"how long a qdb-duck session caches a table's metadata; 0 disables the cache"`
	StabilizationMaxWait time.Duration `yaml:"stabilization_max_wait" help:"longest wait for an unstable cluster per qdb-duck session; 0 means the C API default"`
	TransactionMaxWait   time.Duration `yaml:"transaction_max_wait" help:"longest retry of a conflicting transaction per qdb-duck session; 0 means the C API default"`
	SoftMemoryLimit      int64         `yaml:"soft_memory_limit" help:"client-side soft memory limit per qdb-duck session, in bytes; 0 means the C API default"`
	LoadBalancing        bool          `yaml:"load_balancing" help:"spread a qdb-duck session's requests across the nodes instead of pinning it to one"`
}

// DuckDB governs every DuckDB instance the server opens, one per user:
// the memory and the worker threads one instance may take, where it may
// spill and how much, and how many instances may be live at once. The
// sizes are in bytes. Quasar holds what qdb-duck reads, apart from what
// DuckDB reads.
type DuckDB struct {
	MemoryLimit    int64  `yaml:"memory_limit" help:"memory one DuckDB instance may use, in bytes"`
	Threads        int    `yaml:"threads" help:"worker threads of one DuckDB instance"`
	SpillDirectory string `yaml:"spill_directory" help:"directory a DuckDB instance spills to; empty disables spilling"`
	MaxSpillSize   int64  `yaml:"max_spill_size" help:"bytes one DuckDB instance may spill"`
	MaxInstances   int    `yaml:"max_instances" help:"DuckDB instances live at once, one per user"`
	Quasar         Quasar `yaml:"quasar"`
}

// Metrics switches the Prometheus exposition and names its series.
type Metrics struct {
	Enabled   bool   `yaml:"enabled" help:"serve the Prometheus exposition at GET /metrics on the HTTP listeners"`
	Namespace string `yaml:"namespace" help:"prefix of every series this binary names, so several exporters on one host do not collide"`
}

// Debug switches the runtime profiler. The block exists for that one
// key, because the brief spells it debug.pprof.
type Debug struct {
	Pprof bool `yaml:"pprof" help:"serve the Go runtime profiler under /debug/pprof/ on the HTTP listeners"`
}

// Config is the full server configuration.
type Config struct {
	Listen  Listen  `yaml:"listen"`
	TLS     TLS     `yaml:"tls"`
	Log     Log     `yaml:"log"`
	Cluster Cluster `yaml:"cluster"`
	Pool    Pool    `yaml:"pool"`
	Status  Status  `yaml:"status"`
	Auth    Auth    `yaml:"auth"`
	DuckDB  DuckDB  `yaml:"duckdb"`
	Metrics Metrics `yaml:"metrics"`
	Debug   Debug   `yaml:"debug"`
}

// Default returns the configuration used when nothing is specified:
//
//   - the HTTPS and Flight listeners on their customary ports, and eight
//     seconds to drain on SIGTERM
//   - JSON logs at info, with a query's statement truncated at a
//     kibibyte on debug lines
//   - the local insecure cluster, with the C API knobs at their defaults
//   - the pool sized for one gateway in front of one cluster
//   - no token passphrases, so an ephemeral key per start
//   - argon2id at the second recommended cost of RFC 9106
//   - one DuckDB instance per user at one gibibyte and four threads,
//     with no spilling and at most sixteen instances at once, over
//     qdb-duck's own defaults
//   - the Prometheus exposition on under the qdb_rest namespace, and
//     the runtime profiler off
func Default() Config {
	return Config{
		// Eight seconds of grace is not measured. Anything that stops the
		// binary and then kills it waits longer than this before the kill.
		Listen: Listen{HTTPS: ":40443", Flight: ":40493", ShutdownGrace: 8 * time.Second},
		// The kibibyte of statement is not measured. A statement of that
		// size is several lines of SQL. The length is a key rather than a
		// constant in the code, so an operator tunes it without a build.
		Log: Log{Level: "info", Format: "json", StatementMaxBytes: 1024},
		Cluster: Cluster{
			URI:         "qdb://127.0.0.1:2836",
			Compression: "none",
			Encryption:  "none",
		},
		Pool: Pool{
			MaxSessions: 64,
			PerUserMax:  8,
			IdleTimeout: 5 * time.Minute,
			MaxLifetime: 15 * time.Minute,
			Breaker:     Breaker{Failures: 3, OpenFor: 10 * time.Second},
		},
		Status: Status{ReadinessQuery: "SELECT 1"},
		// TokenSecrets defaults to a non-nil empty slice. The defaults
		// round-trip through YAML in defaults(), where nil marshals as []
		// and decodes back as a non-nil empty slice. One canonical empty
		// value keeps whole-value comparisons honest.
		Auth: Auth{
			TokenSecrets: []string{},
			Argon2id:     Argon2id{Time: 3, MemoryMiB: 64, Parallelism: 4},
			AccessTTL:    15 * time.Minute,
			RefreshTTL:   14 * 24 * time.Hour,
		},
		// DuckDB's own defaults take most of the RAM and every core. That
		// suits a workstation and not a gateway next to qdbd, so these
		// values are conservative. Half a gibibyte, two threads and eight
		// instances were rejected as too tight for joins. Four threads
		// bound one user's scan at four qdbd sessions, because a qdb-duck
		// scan takes one worker per four shards and one qdbd handle per
		// worker. The empty spill directory disables spilling, so a query
		// over the memory limit fails instead of writing to disk. The pool
		// size and the TTL are qdb-duck's own defaults, so a server with
		// no quasar block behaves as one that never set them. A smaller
		// pool tied to Threads was rejected: the session budget is the
		// brief's Risks item, for after the milestones.
		DuckDB: DuckDB{
			MemoryLimit:  1 << 30,
			Threads:      4,
			MaxInstances: 16,
			Quasar:       Quasar{MaxPoolSize: 100, TableCacheTTL: time.Minute},
		},
		Metrics: Metrics{Enabled: true, Namespace: "qdb_rest"},
	}
}

// vocab lists the words each enumerated key accepts. validate checks
// against it, and the tests draw from it.
var vocab = map[string][]string{
	"log.level":           {"debug", "info", "warn", "error"},
	"log.format":          {"json", "text"},
	"cluster.compression": {"none", "balanced"},
	"cluster.encryption":  {"none", "aes"},
}

// A key is one leaf of Config: its dotted path (from the yaml tags), the
// Go type its value parses as, and the help line the command line shows.
type key struct {
	path string
	typ  reflect.Type
	help string
}

// keys walks Config in declaration order and returns one key per leaf.
// The struct is the single definition of every key. The environment
// variable and the flag names derive from the path (envName, flagName).
func keys() []key {
	var out []key
	var walk func(t reflect.Type, prefix string)
	walk = func(t reflect.Type, prefix string) {
		for i := range t.NumField() {
			f := t.Field(i)
			path := prefix + strings.Split(f.Tag.Get("yaml"), ",")[0]
			if f.Type.Kind() == reflect.Struct {
				walk(f.Type, path+".")
				continue
			}
			out = append(out, key{path: path, typ: f.Type, help: f.Tag.Get("help")})
		}
	}
	walk(reflect.TypeFor[Config](), "")
	return out
}

// envPrefix heads every environment variable name. The variable name is
// the path upper-cased with dots as underscores (cluster.user_security_file
// is QDB_REST_CLUSTER_USER_SECURITY_FILE). The flag name is the path with
// dots and underscores as hyphens (--cluster-user-security-file).
const envPrefix = "QDB_REST_"

func envName(path string) string {
	return envPrefix + strings.ToUpper(strings.ReplaceAll(path, ".", "_"))
}

func flagName(path string) string {
	return strings.NewReplacer(".", "-", "_", "-").Replace(path)
}

// aliases are the flag spellings shared with qdbsh, kept next to the
// derived names so every QuasarDB binary reads the same on a command line.
var aliases = map[string]string{
	"cluster":            "cluster.uri",
	"user-security-file": "cluster.user_security_file",
}

// parseValue parses the text of an environment variable or a flag into
// the key's Go type. Parsing here lets the caller refuse a malformed
// value with the name of the variable or flag that carried it.
func parseValue(k key, text string) (any, error) {
	switch k.typ {
	case reflect.TypeFor[string]():
		return text, nil
	case reflect.TypeFor[time.Duration]():
		d, err := time.ParseDuration(text)
		if err != nil {
			return nil, fmt.Errorf("want a duration such as 30s or 5m, got %q", text)
		}
		return d, nil
	case reflect.TypeFor[int]():
		n, err := strconv.Atoi(text)
		if err != nil {
			return nil, fmt.Errorf("want an integer, got %q", text)
		}
		return n, nil
	case reflect.TypeFor[int64]():
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("want a size in bytes, got %q", text)
		}
		return n, nil
	case reflect.TypeFor[[]string]():
		// The environment and the command line carry a list
		// comma-separated, so an element containing a comma must arrive
		// through the YAML list form. Empty text is the canonical
		// empty list (see Default).
		if text == "" {
			return []string{}, nil
		}
		return strings.Split(text, ","), nil
	case reflect.TypeFor[bool]():
		// ParseBool, so the file, the variable and the flag take the same
		// words. Every flag is a string flag, so a bool is spelled
		// --metrics-enabled=false, and a bare --metrics-enabled is refused
		// by the flag package for want of an argument.
		b, err := strconv.ParseBool(text)
		if err != nil {
			return nil, fmt.Errorf("want true or false, got %q", text)
		}
		return b, nil
	}
	panic("config: unsupported key type " + k.typ.String())
}

// placeholder names a key's value in the usage text.
func placeholder(k key) string {
	switch k.typ {
	case reflect.TypeFor[time.Duration]():
		return "DURATION"
	case reflect.TypeFor[int](), reflect.TypeFor[int64]():
		return "N"
	case reflect.TypeFor[[]string]():
		return "LIST"
	case reflect.TypeFor[bool]():
		return "BOOL"
	}
	return "VALUE"
}

// envReference matches a ${VAR} reference inside a value.
var envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// interpolateEnv replaces every ${VAR} in the text with the variable's
// value. An unset variable is an error, because a silently empty secret
// is worse than a refused start.
func interpolateEnv(text string, lookup func(string) (string, bool)) (string, error) {
	var missing []string
	expanded := envReference.ReplaceAllStringFunc(text, func(ref string) string {
		name := ref[2 : len(ref)-1]
		value, ok := lookup(name)
		if !ok {
			missing = append(missing, name)
		}
		return value
	})
	if len(missing) > 0 {
		return "", fmt.Errorf("unset environment variables referenced: %s", strings.Join(missing, ", "))
	}
	return expanded, nil
}

// flagValues holds one parsed command line: the meta flags and, by key
// path, the value of every key flag that was passed. Only the passed
// flags override the file and the environment. An explicitly empty value
// such as --listen-http= is an override too.
type flagValues struct {
	configPath  string
	showVersion bool
	passed      map[string]string
}

// usageText renders the options GNU-style (--name VALUE), the spelling
// every QuasarDB binary uses. The flag package parses one or two dashes
// alike. Keys come in declaration order, with their default when it is
// not empty, and an alias is listed under its target.
func usageText(name string, defaults *koanf.Koanf) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %s [--config FILE] [options]\n\n", name)
	b.WriteString("Every option is also a config-file key (listen.http) and a QDB_REST_*\n")
	b.WriteString("environment variable (QDB_REST_LISTEN_HTTP); precedence: defaults < file <\n")
	b.WriteString("environment < flags.\n\nOptions:\n")
	fmt.Fprintf(&b, "  %-36s %s\n", "--config FILE", "read configuration from FILE")
	fmt.Fprintf(&b, "  %-36s %s\n", "--version", "print version information and exit")
	byTarget := map[string][]string{}
	for alias, path := range aliases {
		byTarget[path] = append(byTarget[path], alias)
	}
	for _, k := range keys() {
		line := k.help
		if d := fmt.Sprint(defaults.Get(k.path)); d != "" && d != "0" {
			line += fmt.Sprintf(" (default %q)", d)
		}
		fmt.Fprintf(&b, "  %-36s %s\n", "--"+flagName(k.path)+" "+placeholder(k), line)
		for _, alias := range byTarget[k.path] {
			fmt.Fprintf(&b, "  %-36s %s\n", "--"+alias+" "+placeholder(k), "alias of --"+flagName(k.path))
		}
	}
	return b.String()
}

// parseFlags parses the arguments against one string flag per key, the
// qdbsh aliases and the two meta flags. Every flag starts empty, and
// fs.Visit reports what the user passed.
func parseFlags(name string, args []string, defaults *koanf.Koanf, output io.Writer) (flagValues, error) {
	parsed := flagValues{passed: map[string]string{}}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() { _, _ = io.WriteString(output, usageText(name, defaults)) }
	fs.StringVar(&parsed.configPath, "config", "", "")
	fs.BoolVar(&parsed.showVersion, "version", false, "")
	byFlag := map[string]string{}
	for _, k := range keys() {
		byFlag[flagName(k.path)] = k.path
	}
	for alias, path := range aliases {
		byFlag[alias] = path
	}
	for name := range byFlag {
		fs.String(name, "", "")
	}
	if err := fs.Parse(args); err != nil {
		return flagValues{}, err
	}
	fs.Visit(func(f *flag.Flag) {
		if path, ok := byFlag[f.Name]; ok {
			parsed.passed[path] = f.Value.String()
		}
	})
	return parsed, nil
}

// layer parses the text values of one source, keyed by path, into a
// typed map for the loader. The environment and the command line deliver
// their values in that form. The label function names the source in
// errors.
func layer(values map[string]string, label func(path string) string) (map[string]any, error) {
	out := map[string]any{}
	for _, k := range keys() {
		text, ok := values[k.path]
		if !ok {
			continue
		}
		value, err := parseValue(k, text)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", label(k.path), err)
		}
		out[k.path] = value
	}
	return out, nil
}

// envLayer reads the environment variable of every key.
func envLayer(lookup func(string) (string, bool)) (map[string]any, error) {
	values := map[string]string{}
	for _, k := range keys() {
		if text, ok := lookup(envName(k.path)); ok {
			values[k.path] = text
		}
	}
	return layer(values, envName)
}

// interpolate expands ${VAR} in every string value the file loaded,
// list elements included (a secret in auth.token_secrets arrives this
// way). It expands values only and never the raw file, so comments may
// mention the syntax freely.
func interpolate(k *koanf.Koanf, lookup func(string) (string, bool)) error {
	for path, value := range k.All() {
		switch v := value.(type) {
		case string:
			expanded, err := interpolateEnv(v, lookup)
			if err != nil {
				return err
			}
			if err := k.Set(path, expanded); err != nil {
				return err
			}
		case []any:
			// koanf hands a YAML list back as []any, and only its string
			// elements can carry ${VAR}.
			for i, item := range v {
				text, ok := item.(string)
				if !ok {
					continue
				}
				expanded, err := interpolateEnv(text, lookup)
				if err != nil {
					return err
				}
				v[i] = expanded
			}
			// The loop expanded the list in place, and Set makes koanf see
			// the expanded list.
			if err := k.Set(path, v); err != nil {
				return err
			}
		}
	}
	return nil
}

// loadFile decodes the YAML file over the loaded layers, then
// interpolates the environment into its values.
func loadFile(k *koanf.Koanf, path string, lookup func(string) (string, bool)) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if err := k.Load(rawbytes.Provider(raw), yaml.Parser()); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := interpolate(k, lookup); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// decode turns the folded layers into a Config. Unknown keys are an
// error so a typo cannot silently fall back to a default. Durations
// arrive as strings from every layer and parse here.
func decode(k *koanf.Koanf) (Config, error) {
	var cfg Config
	err := k.UnmarshalWithConf("", &cfg, koanf.UnmarshalConf{
		Tag: "yaml",
		DecoderConfig: &mapstructure.DecoderConfig{
			DecodeHook:       mapstructure.StringToTimeDurationHookFunc(),
			ErrorUnused:      true,
			WeaklyTypedInput: true,
			Result:           &cfg,
		},
	})
	if err != nil {
		return Config{}, fmt.Errorf("configuration: %w", err)
	}
	return cfg, nil
}

// defaults loads Default() as the bottom layer.
func defaults() (*koanf.Koanf, error) {
	raw, err := yamlv3.Marshal(Default())
	if err != nil {
		return nil, err
	}
	k := koanf.New(".")
	return k, k.Load(rawbytes.Provider(raw), yaml.Parser())
}

// configPath resolves the YAML file location: the --config flag, else the
// QDB_REST_CONFIG environment variable, else none.
func configPath(parsed flagValues, lookup func(string) (string, bool)) string {
	if parsed.configPath != "" {
		return parsed.configPath
	}
	if path, ok := lookup("QDB_REST_CONFIG"); ok {
		return path
	}
	return ""
}

// oneOf rejects a value outside its key's vocabulary.
func oneOf(path, value string) error {
	words := vocab[path]
	for _, w := range words {
		if value == w {
			return nil
		}
	}
	return fmt.Errorf("unknown %s %q (%s)", path, value, strings.Join(words, " | "))
}

// positive rejects a duration that is not strictly positive.
func positive(key string, d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("%s must be positive, got %s", key, d)
	}
	return nil
}

// validateCluster checks only what the binding does not check:
//
//   - the vocabulary this config maps onto the binding's enums
//   - the socket timeout, which is zero for the Go API's default or else
//     a whole number of seconds of at least one, the granularity of the
//     C API
//   - the buffer size, which is an int64 here and a uint in the binding
//
// The C API judges the URI scheme and the knob ranges at dial.
func validateCluster(c Cluster) error {
	if err := oneOf("cluster.compression", c.Compression); err != nil {
		return err
	}
	if err := oneOf("cluster.encryption", c.Encryption); err != nil {
		return err
	}
	if c.Timeout != 0 && (c.Timeout < time.Second || c.Timeout%time.Second != 0) {
		return fmt.Errorf("cluster.timeout must be zero or a whole number of seconds, at least 1s, got %s", c.Timeout)
	}
	if c.MaxInBufferSize < 0 {
		return fmt.Errorf("cluster.max_in_buffer_size must not be negative, got %d", c.MaxInBufferSize)
	}
	return nil
}

// validatePool requires every count to be at least one, the per-user cap
// to fit within the budget, and every age to be positive.
func validatePool(p Pool) error {
	if p.MaxSessions < 1 {
		return fmt.Errorf("pool.max_sessions must be at least 1, got %d", p.MaxSessions)
	}
	if p.PerUserMax < 1 || p.PerUserMax > p.MaxSessions {
		return fmt.Errorf("pool.per_user_max must be within 1..pool.max_sessions (%d), got %d", p.MaxSessions, p.PerUserMax)
	}
	if err := positive("pool.idle_timeout", p.IdleTimeout); err != nil {
		return err
	}
	if err := positive("pool.max_lifetime", p.MaxLifetime); err != nil {
		return err
	}
	if p.Breaker.Failures < 1 {
		return fmt.Errorf("pool.breaker.failures must be at least 1, got %d", p.Breaker.Failures)
	}
	return positive("pool.breaker.open_for", p.Breaker.OpenFor)
}

// validateQuasar requires at least one pooled handle, and a TTL, two
// waits and a soft memory limit that are not negative. The upper bounds
// and the rule that a wait is at least the timeout are qdb-duck's, judged
// at the attach.
func validateQuasar(q Quasar) error {
	if q.MaxPoolSize < 1 {
		return fmt.Errorf("duckdb.quasar.max_pool_size must be at least 1, got %d", q.MaxPoolSize)
	}
	if q.TableCacheTTL < 0 {
		return fmt.Errorf("duckdb.quasar.table_cache_ttl must not be negative, got %s", q.TableCacheTTL)
	}
	if q.StabilizationMaxWait < 0 {
		return fmt.Errorf("duckdb.quasar.stabilization_max_wait must not be negative, got %s", q.StabilizationMaxWait)
	}
	if q.TransactionMaxWait < 0 {
		return fmt.Errorf("duckdb.quasar.transaction_max_wait must not be negative, got %s", q.TransactionMaxWait)
	}
	if q.SoftMemoryLimit < 0 {
		return fmt.Errorf("duckdb.quasar.soft_memory_limit must not be negative, got %d", q.SoftMemoryLimit)
	}
	return nil
}

// validateDuckDB requires a positive memory limit, at least one thread,
// a spill cap that is not negative, at least one instance, and a valid
// quasar block.
func validateDuckDB(d DuckDB) error {
	if d.MemoryLimit <= 0 {
		return fmt.Errorf("duckdb.memory_limit must be positive, got %d", d.MemoryLimit)
	}
	if d.Threads < 1 {
		return fmt.Errorf("duckdb.threads must be at least 1, got %d", d.Threads)
	}
	if d.MaxSpillSize < 0 {
		return fmt.Errorf("duckdb.max_spill_size must not be negative, got %d", d.MaxSpillSize)
	}
	// Zero instances would answer 503 to every DuckDB call, and nothing
	// would say why until a request came in.
	if d.MaxInstances < 1 {
		return fmt.Errorf("duckdb.max_instances must be at least 1, got %d", d.MaxInstances)
	}
	return validateQuasar(d.Quasar)
}

// metricName is the Prometheus data model's name grammar, which a
// namespace must fit because it is the head of every series name.
var metricName = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// validateMetrics requires a namespace that fits the name grammar. An
// empty one is refused rather than read as no prefix, because the
// prefix is what keeps the series apart from every other Go service a
// host scrapes (ADR-0019), and the client panics at registration on a
// name outside the grammar.
func validateMetrics(m Metrics) error {
	if !metricName.MatchString(m.Namespace) {
		return fmt.Errorf("metrics.namespace must match %s, got %q", metricName, m.Namespace)
	}
	return nil
}

// validate rejects impossible configurations before the server starts.
func validate(cfg Config) error {
	if cfg.Listen.HTTPS == "" {
		return errors.New("listen.https must be set")
	}
	if cfg.Listen.Flight == "" {
		return errors.New("listen.flight must be set")
	}
	// A zero grace would cut every call at SIGTERM, and a negative one
	// is meaningless.
	if err := positive("listen.shutdown_grace", cfg.Listen.ShutdownGrace); err != nil {
		return err
	}
	if err := oneOf("log.level", cfg.Log.Level); err != nil {
		return err
	}
	if err := oneOf("log.format", cfg.Log.Format); err != nil {
		return err
	}
	if cfg.Log.StatementMaxBytes < 0 {
		return fmt.Errorf("log.statement_max_bytes must not be negative, got %d", cfg.Log.StatementMaxBytes)
	}
	if err := validateCluster(cfg.Cluster); err != nil {
		return err
	}
	if err := validatePool(cfg.Pool); err != nil {
		return err
	}
	if err := validateDuckDB(cfg.DuckDB); err != nil {
		return err
	}
	if err := validateMetrics(cfg.Metrics); err != nil {
		return err
	}
	if cfg.Status.ReadinessQuery == "" {
		return errors.New("status.readiness_query must not be empty")
	}
	return nil
}

// Load assembles the configuration for one process: defaults, then the
// YAML file (if any), then environment variables, then explicitly passed
// flags. Usage and flag errors are written to output.
func Load(name string, args []string, lookup func(string) (string, bool), output io.Writer) (Config, error) {
	k, err := defaults()
	if err != nil {
		return Config{}, err
	}
	parsed, err := parseFlags(name, args, k, output)
	if err != nil {
		return Config{}, err
	}
	if parsed.showVersion {
		return Config{}, ErrVersionRequested
	}
	if path := configPath(parsed, lookup); path != "" {
		if err := loadFile(k, path, lookup); err != nil {
			return Config{}, err
		}
	}
	env, err := envLayer(lookup)
	if err != nil {
		return Config{}, err
	}
	flags, err := layer(parsed.passed, func(path string) string { return "--" + flagName(path) })
	if err != nil {
		return Config{}, err
	}
	for _, m := range []map[string]any{env, flags} {
		if err := k.Load(confmap.Provider(m, "."), nil); err != nil {
			return Config{}, err
		}
	}
	cfg, err := decode(k)
	if err != nil {
		return Config{}, err
	}
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
