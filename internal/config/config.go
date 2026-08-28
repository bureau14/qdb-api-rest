// Package config loads the server configuration. Precedence, lowest to
// highest: built-in defaults, the YAML file, environment variables,
// command-line flags. Values in the YAML file support ${VAR} environment
// interpolation so secrets can be injected without living on disk.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ErrVersionRequested is returned by Load when the --version flag is
// passed; the caller prints its build metadata and exits. The metadata
// itself lives in package main (injected via -ldflags), which is why the
// flag surfaces as a sentinel instead of being handled here.
var ErrVersionRequested = errors.New("version requested")

// Listen holds the listener addresses. An empty address disables that
// listener.
type Listen struct {
	HTTP  string `yaml:"http"`
	HTTPS string `yaml:"https"`
}

// TLS points the HTTPS listener at a PEM certificate/key pair. Both empty
// means an ephemeral self-signed certificate is generated at startup.
type TLS struct {
	Certificate string `yaml:"certificate"`
	PrivateKey  string `yaml:"private_key"`
}

// Log selects the log output shape.
type Log struct {
	Level  string `yaml:"level"`  // debug | info | warn | error
	Format string `yaml:"format"` // json | console
}

// ServiceUser is the REST API's own QuasarDB user, used by the readiness
// probe: name and secret inline, or the security file QuasarDB generates
// (which carries both). All empty means anonymous.
type ServiceUser struct {
	Name   string `yaml:"name"`
	Secret string `yaml:"secret"`
	File   string `yaml:"file"`
}

// LogValue renders the user without its secret, so a logged config can
// never leak it.
func (u ServiceUser) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("name", u.Name),
		slog.String("file", u.File),
		slog.Bool("secret_set", u.Secret != ""))
}

// Cluster binds the process to one cluster: where it is, how to trust it,
// who the server itself is, and the per-handle C API knobs. A zero knob
// means the C API default.
type Cluster struct {
	URI                   string        `yaml:"uri"`
	PublicKey             string        `yaml:"public_key"`
	PublicKeyFile         string        `yaml:"public_key_file"`
	ServiceUser           ServiceUser   `yaml:"service_user"`
	Compression           string        `yaml:"compression"` // none | balanced | best
	Encryption            string        `yaml:"encryption"`  // none | aes
	Timeout               time.Duration `yaml:"timeout"`     // socket timeout per handle, whole seconds
	MaxInBufferSize       int64         `yaml:"max_in_buffer_size"`
	Parallelism           int           `yaml:"parallelism"`
	ConnectionsPerAddress int           `yaml:"connections_per_address"`
}

// LogValue renders the binding without key material.
func (c Cluster) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("uri", c.URI),
		slog.String("public_key_file", c.PublicKeyFile),
		slog.Bool("public_key_set", c.PublicKey != ""),
		slog.Attr{Key: "service_user", Value: c.ServiceUser.LogValue()},
		slog.String("compression", c.Compression),
		slog.String("encryption", c.Encryption),
		slog.Duration("timeout", c.Timeout))
}

// Breaker sizes the per-cluster circuit breaker.
type Breaker struct {
	Failures int           `yaml:"failures"`
	OpenFor  time.Duration `yaml:"open_for"`
}

// Pool sizes the handle pool: the process-wide budget, the per-user cap,
// the ages at which handles are closed, and the deadline of one C API
// call.
type Pool struct {
	MaxHandles  int           `yaml:"max_handles"`
	PerUserMax  int           `yaml:"per_user_max"`
	IdleTimeout time.Duration `yaml:"idle_timeout"`
	MaxLifetime time.Duration `yaml:"max_lifetime"`
	CallTimeout time.Duration `yaml:"call_timeout"`
	Breaker     Breaker       `yaml:"breaker"`
}

// Status configures the readiness probe.
type Status struct {
	ReadinessQuery string `yaml:"readiness_query"`
}

// Config is the full server configuration. It stays comparable with ==:
// tests fold layers and compare whole values.
type Config struct {
	Listen  Listen  `yaml:"listen"`
	TLS     TLS     `yaml:"tls"`
	Log     Log     `yaml:"log"`
	Cluster Cluster `yaml:"cluster"`
	Pool    Pool    `yaml:"pool"`
	Status  Status  `yaml:"status"`
}

// Default returns the configuration used when nothing is specified: both
// listeners on their customary ports, JSON logs at info, the local
// insecure cluster, C API knobs at their C API defaults, and the pool
// sized for one gateway in front of one cluster.
func Default() Config {
	return Config{
		Listen: Listen{HTTP: ":40080", HTTPS: ":40443"},
		Log:    Log{Level: "info", Format: "json"},
		Cluster: Cluster{
			URI:         "qdb://127.0.0.1:2836",
			Compression: "none",
			Encryption:  "none",
			Timeout:     60 * time.Second,
		},
		Pool: Pool{
			MaxHandles:  64,
			PerUserMax:  8,
			IdleTimeout: 5 * time.Minute,
			MaxLifetime: 15 * time.Minute,
			CallTimeout: 60 * time.Second,
			Breaker:     Breaker{Failures: 3, OpenFor: 10 * time.Second},
		},
		Status: Status{ReadinessQuery: "SELECT 1"},
	}
}

// envReference matches a ${VAR} reference inside a value.
var envReference = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)

// interpolateEnv replaces every ${VAR} in text via lookup. An unset
// variable is an error: a silently empty secret is worse than a refused
// start.
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

// Environment variables are named after the key path, upper-cased, with
// dots as underscores: cluster.service_user.name is
// QDB_REST_CLUSTER_SERVICE_USER_NAME.
const envPrefix = "QDB_REST_"

// stringFields maps environment variables onto the string fields of cfg;
// it doubles as the walk interpolation takes, so every inline secret is
// a ${VAR} candidate.
func stringFields(cfg *Config) map[string]*string {
	return map[string]*string{
		envPrefix + "LISTEN_HTTP":                 &cfg.Listen.HTTP,
		envPrefix + "LISTEN_HTTPS":                &cfg.Listen.HTTPS,
		envPrefix + "TLS_CERTIFICATE":             &cfg.TLS.Certificate,
		envPrefix + "TLS_PRIVATE_KEY":             &cfg.TLS.PrivateKey,
		envPrefix + "LOG_LEVEL":                   &cfg.Log.Level,
		envPrefix + "LOG_FORMAT":                  &cfg.Log.Format,
		envPrefix + "CLUSTER_URI":                 &cfg.Cluster.URI,
		envPrefix + "CLUSTER_PUBLIC_KEY":          &cfg.Cluster.PublicKey,
		envPrefix + "CLUSTER_PUBLIC_KEY_FILE":     &cfg.Cluster.PublicKeyFile,
		envPrefix + "CLUSTER_SERVICE_USER_NAME":   &cfg.Cluster.ServiceUser.Name,
		envPrefix + "CLUSTER_SERVICE_USER_SECRET": &cfg.Cluster.ServiceUser.Secret,
		envPrefix + "CLUSTER_SERVICE_USER_FILE":   &cfg.Cluster.ServiceUser.File,
		envPrefix + "CLUSTER_COMPRESSION":         &cfg.Cluster.Compression,
		envPrefix + "CLUSTER_ENCRYPTION":          &cfg.Cluster.Encryption,
		envPrefix + "STATUS_READINESS_QUERY":      &cfg.Status.ReadinessQuery,
	}
}

// A setter writes one environment value into its field, parsing it as
// the field's type.
type setter func(text string) error

func setString(field *string) setter {
	return func(text string) error {
		*field = text
		return nil
	}
}

func setInt(field *int) setter {
	return func(text string) error {
		n, err := strconv.Atoi(text)
		if err != nil {
			return fmt.Errorf("want an integer, got %q", text)
		}
		*field = n
		return nil
	}
}

// setSize parses a byte count; sizes are plain integers, no suffixes.
func setSize(field *int64) setter {
	return func(text string) error {
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return fmt.Errorf("want a size in bytes, got %q", text)
		}
		*field = n
		return nil
	}
}

func setDuration(field *time.Duration) setter {
	return func(text string) error {
		d, err := time.ParseDuration(text)
		if err != nil {
			return fmt.Errorf("want a duration such as 30s or 5m, got %q", text)
		}
		*field = d
		return nil
	}
}

// envOverrides maps every environment variable onto the setter of its
// field: the string fields verbatim, the typed fields through a parser.
func envOverrides(cfg *Config) map[string]setter {
	overrides := map[string]setter{
		envPrefix + "CLUSTER_TIMEOUT":                 setDuration(&cfg.Cluster.Timeout),
		envPrefix + "CLUSTER_MAX_IN_BUFFER_SIZE":      setSize(&cfg.Cluster.MaxInBufferSize),
		envPrefix + "CLUSTER_PARALLELISM":             setInt(&cfg.Cluster.Parallelism),
		envPrefix + "CLUSTER_CONNECTIONS_PER_ADDRESS": setInt(&cfg.Cluster.ConnectionsPerAddress),
		envPrefix + "POOL_MAX_HANDLES":                setInt(&cfg.Pool.MaxHandles),
		envPrefix + "POOL_PER_USER_MAX":               setInt(&cfg.Pool.PerUserMax),
		envPrefix + "POOL_IDLE_TIMEOUT":               setDuration(&cfg.Pool.IdleTimeout),
		envPrefix + "POOL_MAX_LIFETIME":               setDuration(&cfg.Pool.MaxLifetime),
		envPrefix + "POOL_CALL_TIMEOUT":               setDuration(&cfg.Pool.CallTimeout),
		envPrefix + "POOL_BREAKER_FAILURES":           setInt(&cfg.Pool.Breaker.Failures),
		envPrefix + "POOL_BREAKER_OPEN_FOR":           setDuration(&cfg.Pool.Breaker.OpenFor),
	}
	for name, field := range stringFields(cfg) {
		overrides[name] = setString(field)
	}
	return overrides
}

// interpolateValues expands ${VAR} in every string field of cfg. Values
// only, never the raw file: comments may mention the syntax freely.
func interpolateValues(cfg *Config, lookup func(string) (string, bool)) error {
	for _, field := range stringFields(cfg) {
		expanded, err := interpolateEnv(*field, lookup)
		if err != nil {
			return err
		}
		*field = expanded
	}
	return nil
}

// loadFile decodes the YAML file at path over cfg, then interpolates the
// environment into its values. Unknown keys are an error so a typo cannot
// silently fall back to a default.
func loadFile(path string, lookup func(string) (string, bool), cfg *Config) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := yaml.NewDecoder(strings.NewReader(string(raw)))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := interpolateValues(cfg, lookup); err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// applyEnv writes every set environment variable over its field; a value
// that does not parse as the field's type refuses the start.
func applyEnv(cfg *Config, lookup func(string) (string, bool)) error {
	for name, set := range envOverrides(cfg) {
		if value, ok := lookup(name); ok {
			if err := set(value); err != nil {
				return fmt.Errorf("%s: %w", name, err)
			}
		}
	}
	return nil
}

// flagValues holds one parsed command line; set records which flags were
// actually passed, so only those override the file and the environment
// (an explicitly empty flag value, e.g. --listen-tls=, is an override too).
type flagValues struct {
	configPath  string
	showVersion bool
	values      Config
	set         map[string]bool
}

// usageText renders the options GNU-style (--name VALUE), the spelling
// every QuasarDB binary uses; the flag package parses one or two dashes
// alike. Value placeholders come from backquoted words in the usage
// strings (flag.UnquoteUsage); a default is shown only when it is set.
func usageText(fs *flag.FlagSet) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Usage: %s [--config FILE] [options]\n\n", fs.Name())
	b.WriteString("Every option also has a config-file key and a QDB_REST_* environment\n")
	b.WriteString("variable; precedence: defaults < file < environment < flags.\n\nOptions:\n")
	fs.VisitAll(func(f *flag.Flag) {
		placeholder, usage := flag.UnquoteUsage(f)
		fmt.Fprintf(&b, "  %-26s %s", strings.TrimSpace("--"+f.Name+" "+placeholder), usage)
		if f.DefValue != "" && f.DefValue != "false" {
			fmt.Fprintf(&b, " (default %q)", f.DefValue)
		}
		b.WriteString("\n")
	})
	return b.String()
}

// flagFields maps each flag onto the field it overrides. The cluster
// flags are the three qdbsh accepts, spelled the same; every other
// cluster and pool key is file or environment only.
func flagFields(cfg *Config) map[string]*string {
	return map[string]*string{
		"listen":                  &cfg.Listen.HTTP,
		"listen-tls":              &cfg.Listen.HTTPS,
		"tls-cert":                &cfg.TLS.Certificate,
		"tls-key":                 &cfg.TLS.PrivateKey,
		"log-level":               &cfg.Log.Level,
		"log-format":              &cfg.Log.Format,
		"cluster":                 &cfg.Cluster.URI,
		"cluster-public-key-file": &cfg.Cluster.PublicKeyFile,
		"user-security-file":      &cfg.Cluster.ServiceUser.File,
	}
}

// parseFlags parses args. The defaults shown by --help come from
// Default().
func parseFlags(name string, args []string, output io.Writer) (flagValues, error) {
	parsed := flagValues{values: Default(), set: map[string]bool{}}
	fields := flagFields(&parsed.values)
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(output)
	fs.Usage = func() { _, _ = io.WriteString(output, usageText(fs)) }
	fs.StringVar(&parsed.configPath, "config", "", "read configuration from `FILE`")
	fs.BoolVar(&parsed.showVersion, "version", false, "print version information and exit")
	fs.StringVar(fields["listen"], "listen", *fields["listen"], "HTTP listen `ADDR`; empty disables")
	fs.StringVar(fields["listen-tls"], "listen-tls", *fields["listen-tls"], "HTTPS listen `ADDR`; empty disables")
	fs.StringVar(fields["tls-cert"], "tls-cert", "", "PEM certificate `FILE` for the HTTPS listener")
	fs.StringVar(fields["tls-key"], "tls-key", "", "PEM private key `FILE` for the HTTPS listener")
	fs.StringVar(fields["log-level"], "log-level", *fields["log-level"], "log `LEVEL`: debug | info | warn | error")
	fs.StringVar(fields["log-format"], "log-format", *fields["log-format"], "log `FORMAT`: json | console")
	fs.StringVar(fields["cluster"], "cluster", *fields["cluster"], "cluster `URI`, comma-separated for several nodes")
	fs.StringVar(fields["cluster-public-key-file"], "cluster-public-key-file", "", "cluster public key `FILE`")
	fs.StringVar(fields["user-security-file"], "user-security-file", "", "service user security `FILE`")
	if err := fs.Parse(args); err != nil {
		return flagValues{}, err
	}
	fs.Visit(func(f *flag.Flag) { parsed.set[f.Name] = true })
	return parsed, nil
}

// applyFlags copies every explicitly passed flag over its field.
func applyFlags(cfg *Config, parsed flagValues) {
	from := flagFields(&parsed.values)
	for name, field := range flagFields(cfg) {
		if parsed.set[name] {
			*field = *from[name]
		}
	}
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

// oneOf rejects a value outside its vocabulary.
func oneOf(key, value string, words ...string) error {
	for _, w := range words {
		if value == w {
			return nil
		}
	}
	return fmt.Errorf("unknown %s %q (%s)", key, value, strings.Join(words, " | "))
}

// positive rejects a duration that is not strictly positive.
func positive(key string, d time.Duration) error {
	if d <= 0 {
		return fmt.Errorf("%s must be positive, got %s", key, d)
	}
	return nil
}

// validateCluster: the trust material is inline or a file, never both;
// a cluster key needs a user to authenticate; the C API knobs are within
// what the C API accepts.
func validateCluster(c Cluster) error {
	if !strings.HasPrefix(c.URI, "qdb://") {
		return fmt.Errorf("cluster.uri must start with qdb://, got %q", c.URI)
	}
	if c.PublicKey != "" && c.PublicKeyFile != "" {
		return errors.New("cluster.public_key and cluster.public_key_file are mutually exclusive")
	}
	u := c.ServiceUser
	if u.File != "" && (u.Name != "" || u.Secret != "") {
		return errors.New("cluster.service_user.file is exclusive with name and secret")
	}
	if (u.Name == "") != (u.Secret == "") {
		return errors.New("cluster.service_user.name and secret must be set together")
	}
	if (c.PublicKey != "" || c.PublicKeyFile != "") && u.File == "" && u.Name == "" {
		return errors.New("a cluster public key requires cluster.service_user")
	}
	if err := oneOf("cluster.compression", c.Compression, "none", "balanced", "best"); err != nil {
		return err
	}
	if err := oneOf("cluster.encryption", c.Encryption, "none", "aes"); err != nil {
		return err
	}
	if c.Timeout < time.Second || c.Timeout%time.Second != 0 {
		return fmt.Errorf("cluster.timeout must be a whole number of seconds, at least 1s, got %s", c.Timeout)
	}
	if c.MaxInBufferSize < 0 {
		return fmt.Errorf("cluster.max_in_buffer_size must not be negative, got %d", c.MaxInBufferSize)
	}
	if c.Parallelism < 0 {
		return fmt.Errorf("cluster.parallelism must not be negative, got %d", c.Parallelism)
	}
	if n := c.ConnectionsPerAddress; n != 0 && (n < 2 || n > 100000) {
		return fmt.Errorf("cluster.connections_per_address must be 0 or within 2..100000, got %d", n)
	}
	return nil
}

// validatePool: every count at least one, the per-user cap within the
// budget, every age and deadline positive.
func validatePool(p Pool) error {
	if p.MaxHandles < 1 {
		return fmt.Errorf("pool.max_handles must be at least 1, got %d", p.MaxHandles)
	}
	if p.PerUserMax < 1 || p.PerUserMax > p.MaxHandles {
		return fmt.Errorf("pool.per_user_max must be within 1..pool.max_handles (%d), got %d", p.MaxHandles, p.PerUserMax)
	}
	if err := positive("pool.idle_timeout", p.IdleTimeout); err != nil {
		return err
	}
	if err := positive("pool.max_lifetime", p.MaxLifetime); err != nil {
		return err
	}
	if err := positive("pool.call_timeout", p.CallTimeout); err != nil {
		return err
	}
	if p.Breaker.Failures < 1 {
		return fmt.Errorf("pool.breaker.failures must be at least 1, got %d", p.Breaker.Failures)
	}
	return positive("pool.breaker.open_for", p.Breaker.OpenFor)
}

// validate rejects impossible configurations before the server starts.
func validate(cfg Config) error {
	if cfg.Listen.HTTP == "" && cfg.Listen.HTTPS == "" {
		return errors.New("both listeners disabled: set listen.http or listen.https")
	}
	if (cfg.TLS.Certificate == "") != (cfg.TLS.PrivateKey == "") {
		return errors.New("tls.certificate and tls.private_key must be set together")
	}
	if err := oneOf("log.level", cfg.Log.Level, "debug", "info", "warn", "error"); err != nil {
		return err
	}
	if err := oneOf("log.format", cfg.Log.Format, "json", "console"); err != nil {
		return err
	}
	if err := validateCluster(cfg.Cluster); err != nil {
		return err
	}
	if err := validatePool(cfg.Pool); err != nil {
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
	parsed, err := parseFlags(name, args, output)
	if err != nil {
		return Config{}, err
	}
	if parsed.showVersion {
		return Config{}, ErrVersionRequested
	}
	cfg := Default()
	if path := configPath(parsed, lookup); path != "" {
		if err := loadFile(path, lookup, &cfg); err != nil {
			return Config{}, err
		}
	}
	if err := applyEnv(&cfg, lookup); err != nil {
		return Config{}, err
	}
	applyFlags(&cfg, parsed)
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
