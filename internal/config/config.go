// Package config loads the server configuration. Precedence, lowest to
// highest: built-in defaults, the YAML file, environment variables,
// command-line flags. The YAML file supports ${VAR} environment
// interpolation so secrets can be injected without living on disk.
package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

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

// Config is the full server configuration.
type Config struct {
	Listen Listen `yaml:"listen"`
	TLS    TLS    `yaml:"tls"`
	Log    Log    `yaml:"log"`
}

// Default returns the configuration used when nothing is specified: both
// listeners on their customary ports, JSON logs at info.
func Default() Config {
	return Config{
		Listen: Listen{HTTP: ":40080", HTTPS: ":40443"},
		Log:    Log{Level: "info", Format: "json"},
	}
}

// envReference matches a ${VAR} reference inside the YAML text.
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

// loadFile decodes the YAML file at path over cfg, after environment
// interpolation. Unknown keys are an error so a typo cannot silently fall
// back to a default.
func loadFile(path string, lookup func(string) (string, bool), cfg *Config) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text, err := interpolateEnv(string(raw), lookup)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	decoder := yaml.NewDecoder(strings.NewReader(text))
	decoder.KnownFields(true)
	if err := decoder.Decode(cfg); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("%s: %w", path, err)
	}
	return nil
}

// envOverrides maps environment variables onto the fields they override.
func envOverrides(cfg *Config) map[string]*string {
	return map[string]*string{
		"QDB_REST_LISTEN_HTTP":     &cfg.Listen.HTTP,
		"QDB_REST_LISTEN_HTTPS":    &cfg.Listen.HTTPS,
		"QDB_REST_TLS_CERTIFICATE": &cfg.TLS.Certificate,
		"QDB_REST_TLS_PRIVATE_KEY": &cfg.TLS.PrivateKey,
		"QDB_REST_LOG_LEVEL":       &cfg.Log.Level,
		"QDB_REST_LOG_FORMAT":      &cfg.Log.Format,
	}
}

// applyEnv writes every set environment variable over its field.
func applyEnv(cfg *Config, lookup func(string) (string, bool)) {
	for name, field := range envOverrides(cfg) {
		if value, ok := lookup(name); ok {
			*field = value
		}
	}
}

// flagValues holds one parsed command line; set records which flags were
// actually passed, so only those override the file and the environment
// (an explicitly empty flag value, e.g. -listen-tls=, is an override too).
type flagValues struct {
	configPath string
	values     Config
	set        map[string]bool
}

// parseFlags parses args. The flag defaults shown by -h come from
// Default().
func parseFlags(name string, args []string, output io.Writer) (flagValues, error) {
	parsed := flagValues{values: Default(), set: map[string]bool{}}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(output)
	fs.StringVar(&parsed.configPath, "config", "", "path to YAML config file")
	fs.StringVar(&parsed.values.Listen.HTTP, "listen", parsed.values.Listen.HTTP, "HTTP listen address; empty disables")
	fs.StringVar(&parsed.values.Listen.HTTPS, "listen-tls", parsed.values.Listen.HTTPS, "HTTPS listen address; empty disables")
	fs.StringVar(&parsed.values.TLS.Certificate, "tls-cert", "", "PEM certificate file for the HTTPS listener")
	fs.StringVar(&parsed.values.TLS.PrivateKey, "tls-key", "", "PEM private key file for the HTTPS listener")
	fs.StringVar(&parsed.values.Log.Level, "log-level", parsed.values.Log.Level, "debug | info | warn | error")
	fs.StringVar(&parsed.values.Log.Format, "log-format", parsed.values.Log.Format, "json | console")
	if err := fs.Parse(args); err != nil {
		return flagValues{}, err
	}
	fs.Visit(func(f *flag.Flag) { parsed.set[f.Name] = true })
	return parsed, nil
}

// applyFlags copies every explicitly passed flag over its field.
func applyFlags(cfg *Config, parsed flagValues) {
	if parsed.set["listen"] {
		cfg.Listen.HTTP = parsed.values.Listen.HTTP
	}
	if parsed.set["listen-tls"] {
		cfg.Listen.HTTPS = parsed.values.Listen.HTTPS
	}
	if parsed.set["tls-cert"] {
		cfg.TLS.Certificate = parsed.values.TLS.Certificate
	}
	if parsed.set["tls-key"] {
		cfg.TLS.PrivateKey = parsed.values.TLS.PrivateKey
	}
	if parsed.set["log-level"] {
		cfg.Log.Level = parsed.values.Log.Level
	}
	if parsed.set["log-format"] {
		cfg.Log.Format = parsed.values.Log.Format
	}
}

// configPath resolves the YAML file location: the -config flag, else the
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

// validate rejects impossible configurations before the server starts.
func validate(cfg Config) error {
	if cfg.Listen.HTTP == "" && cfg.Listen.HTTPS == "" {
		return errors.New("both listeners disabled: set listen.http or listen.https")
	}
	if (cfg.TLS.Certificate == "") != (cfg.TLS.PrivateKey == "") {
		return errors.New("tls.certificate and tls.private_key must be set together")
	}
	switch cfg.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unknown log.level %q (debug | info | warn | error)", cfg.Log.Level)
	}
	switch cfg.Log.Format {
	case "json", "console":
	default:
		return fmt.Errorf("unknown log.format %q (json | console)", cfg.Log.Format)
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
	cfg := Default()
	if path := configPath(parsed, lookup); path != "" {
		if err := loadFile(path, lookup, &cfg); err != nil {
			return Config{}, err
		}
	}
	applyEnv(&cfg, lookup)
	applyFlags(&cfg, parsed)
	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
