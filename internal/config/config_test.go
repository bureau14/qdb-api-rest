package config

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// envFrom builds a lookup over a fixed map, so tests never touch the real
// environment.
func envFrom(vars map[string]string) func(string) (string, bool) {
	return func(name string) (string, bool) {
		value, ok := vars[name]
		return value, ok
	}
}

// writeConfig writes yaml into a temp file and returns its path.
func writeConfig(t *testing.T, yaml string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "qdb_rest.yaml")
	if err := os.WriteFile(path, []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// load is Load with the noise arguments fixed for tests.
func load(t *testing.T, args []string, vars map[string]string) (Config, error) {
	t.Helper()
	return Load("qdb_rest", args, envFrom(vars), io.Discard)
}

func TestDefaults(t *testing.T) {
	cfg, err := load(t, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := Default()
	if cfg != want {
		t.Fatalf("got %+v, want %+v", cfg, want)
	}
}

func TestPrecedenceFileEnvFlag(t *testing.T) {
	path := writeConfig(t, "listen:\n  http: \":1111\"\n  https: \":2222\"\nlog:\n  level: debug\n")
	vars := map[string]string{
		"QDB_REST_LISTEN_HTTPS": ":3333",
		"QDB_REST_LOG_LEVEL":    "warn",
	}
	cfg, err := load(t, []string{"--config", path, "--log-level", "error"}, vars)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.HTTP != ":1111" {
		t.Errorf("file value lost: %q", cfg.Listen.HTTP)
	}
	if cfg.Listen.HTTPS != ":3333" {
		t.Errorf("env did not override file: %q", cfg.Listen.HTTPS)
	}
	if cfg.Log.Level != "error" {
		t.Errorf("flag did not override env: %q", cfg.Log.Level)
	}
}

func TestEnvInterpolation(t *testing.T) {
	path := writeConfig(t, "tls:\n  certificate: \"${CERT_PATH}\"\n  private_key: \"${KEY_PATH}\"\n")
	vars := map[string]string{"CERT_PATH": "/etc/cert.pem", "KEY_PATH": "/etc/key.pem"}
	cfg, err := load(t, []string{"--config", path}, vars)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLS.Certificate != "/etc/cert.pem" || cfg.TLS.PrivateKey != "/etc/key.pem" {
		t.Errorf("interpolation lost: %+v", cfg.TLS)
	}
}

func TestEnvInterpolationUnsetIsError(t *testing.T) {
	path := writeConfig(t, "tls:\n  certificate: \"${MISSING_VAR}\"\n")
	_, err := load(t, []string{"--config", path}, nil)
	if err == nil || !strings.Contains(err.Error(), "MISSING_VAR") {
		t.Fatalf("want unset-variable error naming MISSING_VAR, got %v", err)
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	path := writeConfig(t, "listne:\n  http: \":1111\"\n")
	if _, err := load(t, []string{"--config", path}, nil); err == nil {
		t.Fatal("want error for unknown key, got nil")
	}
}

func TestConfigPathFromEnv(t *testing.T) {
	path := writeConfig(t, "listen:\n  http: \":4444\"\n")
	cfg, err := load(t, nil, map[string]string{"QDB_REST_CONFIG": path})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.HTTP != ":4444" {
		t.Errorf("QDB_REST_CONFIG ignored: %q", cfg.Listen.HTTP)
	}
}

func TestEmptyFlagValueDisablesListener(t *testing.T) {
	cfg, err := load(t, []string{"--listen-tls="}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen.HTTPS != "" {
		t.Errorf("explicit empty flag did not disable HTTPS: %q", cfg.Listen.HTTPS)
	}
}

func TestBothListenersDisabledIsError(t *testing.T) {
	if _, err := load(t, []string{"--listen=", "--listen-tls="}, nil); err == nil {
		t.Fatal("want error with both listeners disabled, got nil")
	}
}

func TestTLSPairMustBeComplete(t *testing.T) {
	if _, err := load(t, []string{"--tls-cert", "/etc/cert.pem"}, nil); err == nil {
		t.Fatal("want error for certificate without key, got nil")
	}
}

func TestBadLogVocabulary(t *testing.T) {
	if _, err := load(t, []string{"--log-level", "verbose"}, nil); err == nil {
		t.Fatal("want error for unknown log level, got nil")
	}
	if _, err := load(t, []string{"--log-format", "xml"}, nil); err == nil {
		t.Fatal("want error for unknown log format, got nil")
	}
}
