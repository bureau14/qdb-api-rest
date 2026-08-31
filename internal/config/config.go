// Package config loads the server configuration. Every key is defined
// once, as a field of Config with its yaml tag, and reaches the process
// through three layers named after that key path: the YAML file
// (listen.http), the environment (QDB_REST_LISTEN_HTTP) and the command
// line (--listen-http). Precedence, lowest to highest: built-in defaults,
// the file, the environment, explicitly passed flags. Values in the file
// support ${VAR} environment interpolation so secrets can be injected
// without living on disk.
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
// passed; the caller prints its build metadata and exits. The metadata
// itself lives in package main (injected via -ldflags), which is why the
// flag surfaces as a sentinel instead of being handled here.
var ErrVersionRequested = errors.New("version requested")

// Listen holds the listener addresses. An empty address disables that
// listener.
type Listen struct {
	HTTP  string `yaml:"http" help:"HTTP listener address; empty disables the listener"`
	HTTPS string `yaml:"https" help:"HTTPS listener address; empty disables the listener"`
}

// TLS points the HTTPS listener at a PEM certificate/key pair. Both empty
// means an ephemeral self-signed certificate is generated at startup.
type TLS struct {
	Certificate string `yaml:"certificate" help:"PEM certificate for the HTTPS listener, set together with tls.private_key"`
	PrivateKey  string `yaml:"private_key" help:"PEM private key for the HTTPS listener"`
}

// Log selects the log output shape.
type Log struct {
	Level  string `yaml:"level" help:"debug | info | warn | error"`
	Format string `yaml:"format" help:"json | console"`
}

// Cluster binds the process to one cluster: where it is, its public key
// file, the REST API's own user (the user security file carrying username
// and secret key; empty means anonymous), and the per-session C API
// knobs. A zero knob means the C API default. Key material comes from
// files only, the form QuasarDB's tooling produces, so no secret sits in
// the YAML; callers' credentials arrive through the API.
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
		},
		Pool: Pool{
			MaxSessions: 64,
			PerUserMax:  8,
			IdleTimeout: 5 * time.Minute,
			MaxLifetime: 15 * time.Minute,
			Breaker:     Breaker{Failures: 3, OpenFor: 10 * time.Second},
		},
		Status: Status{ReadinessQuery: "SELECT 1"},
	}
}

// vocab lists the words each enumerated key accepts; validate checks
// against it and the tests draw from it.
var vocab = map[string][]string{
	"log.level":           {"debug", "info", "warn", "error"},
	"log.format":          {"json", "console"},
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
// The struct is the single definition of every key; the environment
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

// envPrefix and the naming rules: the environment variable is the path
// upper-cased with dots as underscores (cluster.user_security_file is
// QDB_REST_CLUSTER_USER_SECURITY_FILE); the flag is the path with dots
// and underscores as hyphens (--cluster-user-security-file).
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

// parseValue parses text, as the environment and the command line carry
// it, into the key's Go type, so a malformed value is refused with the
// name of the variable or flag that carried it.
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
	}
	return "VALUE"
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

// flagValues holds one parsed command line: the meta flags, and the
// value of every key flag that was actually passed, by key path, so only
// those override the file and the environment (an explicitly empty flag
// value, e.g. --listen-https=, is an override too).
type flagValues struct {
	configPath  string
	showVersion bool
	passed      map[string]string
}

// usageText renders the options GNU-style (--name VALUE), the spelling
// every QuasarDB binary uses; the flag package parses one or two dashes
// alike. Keys come in declaration order, with their default when it is
// not empty; an alias is listed under its target.
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

// parseFlags parses args: one string flag per key, the qdbsh aliases, and
// the two meta flags. Every flag starts empty; what the user passed is
// what fs.Visit reports.
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

// layer turns the values one source carries -- text keyed by path, as the
// environment and the command line deliver them -- into a typed map for
// the loader; label names the source in errors.
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

// envLayer reads every key's environment variable through lookup.
func envLayer(lookup func(string) (string, bool)) (map[string]any, error) {
	values := map[string]string{}
	for _, k := range keys() {
		if text, ok := lookup(envName(k.path)); ok {
			values[k.path] = text
		}
	}
	return layer(values, envName)
}

// interpolate expands ${VAR} in every string value the file loaded.
// Values only, never the raw file: comments may mention the syntax freely.
func interpolate(k *koanf.Koanf, lookup func(string) (string, bool)) error {
	for path, value := range k.All() {
		text, ok := value.(string)
		if !ok {
			continue
		}
		expanded, err := interpolateEnv(text, lookup)
		if err != nil {
			return err
		}
		if err := k.Set(path, expanded); err != nil {
			return err
		}
	}
	return nil
}

// loadFile decodes the YAML file at path over k, then interpolates the
// environment into its values.
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
// error so a typo cannot silently fall back to a default; durations
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

// validateCluster checks only what the binding does not: the vocabulary
// this config maps onto the binding's enums, the socket timeout (zero
// leaves the Go API's default; otherwise whole seconds, at least one, the
// C API's own granularity), and the buffer size (an int64 here, a uint
// there). The
// URI scheme and the C API knob ranges are judged by the C API when a
// session is dialed; a key or a user given both inline and as a file is
// read from the file.
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

// validatePool: every count at least one, the per-user cap within the
// budget, every age positive.
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

// validate rejects impossible configurations before the server starts.
func validate(cfg Config) error {
	if cfg.Listen.HTTP == "" && cfg.Listen.HTTPS == "" {
		return errors.New("both listeners disabled: set listen.http or listen.https")
	}
	if err := oneOf("log.level", cfg.Log.Level); err != nil {
		return err
	}
	if err := oneOf("log.format", cfg.Log.Format); err != nil {
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
