package config

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
	"pgregory.net/rapid"
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
func load(args []string, vars map[string]string) (Config, error) {
	return Load("qdb_rest", args, envFrom(vars), io.Discard)
}

// A slot is one configurable thing in its spellings: YAML key paths (the
// environment variable is QDB_REST_ plus the path upper-cased with
// underscores) and, when it has them, flags; plus the vocabulary drawn
// for it, invalid words included so validation is exercised too. TLS is
// one slot for both files because validate wants them together. Typed
// slots (ints, durations) draw parseable words only: a word that does
// not parse is a load error regardless of the layer, pinned separately.
type slot struct {
	paths  [][]string
	flags  []string
	typed  bool // written into the file unquoted, as the literal it is
	values *rapid.Generator[string]
}

func words(w ...string) *rapid.Generator[string] { return rapid.SampledFrom(w) }

func key(path string) [][]string { return [][]string{strings.Split(path, ".")} }

func slots() []slot {
	addresses := words("", ":1", ":40080", "127.0.0.1:8080")
	paths := words("", "/etc/qdb/rest", "rel/pair")
	return []slot{
		{key("listen.http"), []string{"listen"}, false, addresses},
		{key("listen.https"), []string{"listen-tls"}, false, addresses},
		{[][]string{{"tls", "certificate"}, {"tls", "private_key"}}, []string{"tls-cert", "tls-key"}, false, paths},
		{key("log.level"), []string{"log-level"}, false, words("debug", "info", "warn", "error", "verbose")},
		{key("log.format"), []string{"log-format"}, false, words("json", "console", "xml")},
		{key("cluster.uri"), []string{"cluster"}, false, words("qdb://127.0.0.1:2836", "qdb://a:1,b:2", "", "http://x")},
		{key("cluster.public_key"), nil, false, words("", "PK")},
		{key("cluster.public_key_file"), []string{"cluster-public-key-file"}, false, words("", "/etc/qdb/cluster_public.key")},
		{key("cluster.username"), nil, false, words("", "qdb_rest")},
		{key("cluster.secret_key"), nil, false, words("", "S3CRET")},
		{key("cluster.user_security_file"), nil, false, words("", "/etc/qdb/user_private.key")},
		{key("cluster.compression"), nil, false, words("none", "balanced", "lz4")},
		{key("cluster.encryption"), nil, false, words("none", "aes", "rot13")},
		{key("cluster.timeout"), nil, true, words("1s", "60s", "0s", "500ms", "1500ms", "-1s")},
		{key("cluster.max_in_buffer_size"), nil, true, words("0", "8589934592", "-1")},
		{key("cluster.parallelism"), nil, true, words("0", "4", "-1")},
		{key("cluster.connections_per_address"), nil, true, words("0", "1", "2", "100000", "100001")},
		{key("pool.max_sessions"), nil, true, words("0", "1", "64")},
		{key("pool.per_user_max"), nil, true, words("0", "1", "8", "65")},
		{key("pool.idle_timeout"), nil, true, words("5m", "0s", "-1s")},
		{key("pool.max_lifetime"), nil, true, words("15m", "0s")},
		{key("pool.call_timeout"), nil, true, words("60s", "100ms", "0s")},
		{key("pool.breaker.failures"), nil, true, words("0", "1", "3")},
		{key("pool.breaker.open_for"), nil, true, words("10s", "0s")},
		{key("status.readiness_query"), nil, false, words("SELECT 1", "")},
	}
}

func envName(path []string) string {
	return envPrefix + strings.ToUpper(strings.Join(path, "_"))
}

// apply writes vals into cfg through the setters applyEnv itself uses.
func apply(t *rapid.T, cfg *Config, s slot, vals []string) {
	setters := envOverrides(cfg)
	for i, path := range s.paths {
		if err := setters[envName(path)](vals[i]); err != nil {
			t.Fatalf("%s: %v", envName(path), err)
		}
	}
}

// draw returns one value per path of s: the drawn word, suffixed per key
// for multi-key slots so the pair is distinguishable yet both-or-neither.
func draw(rt *rapid.T, s slot, layer string) []string {
	word := s.values.Draw(rt, strings.Join(s.paths[0], ".")+" "+layer)
	out := make([]string, len(s.paths))
	for i, path := range s.paths {
		out[i] = word
		if word != "" && len(s.paths) > 1 {
			out[i] = word + "." + path[len(path)-1]
		}
	}
	return out
}

// fileLayer is the YAML document under construction, as nested maps.
type fileLayer map[string]any

// put writes value at path, creating sections on the way.
func (f fileLayer) put(path []string, value any) {
	section := f
	for _, name := range path[:len(path)-1] {
		child, ok := section[name].(fileLayer)
		if !ok {
			child = fileLayer{}
			section[name] = child
		}
		section = child
	}
	section[path[len(path)-1]] = value
}

// literal is the YAML value for text: typed slots carry their literal
// (ints unquoted, durations as plain words), strings are quoted.
func literal(s slot, text string) any {
	if !s.typed {
		return text
	}
	var n int64
	if _, err := fmt.Sscan(text, &n); err == nil && !strings.ContainsAny(text, "smh") {
		return n
	}
	return text
}

// Load is the fold defaults < file < env < flags followed by validate:
// for any drawn combination of layers, it returns the folded config
// exactly when validate accepts it. String values in the file are
// written literally or as ${VAR} references, and the file path arrives
// by flag or by QDB_REST_CONFIG.
func TestLoadIsTheLayerFold(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		expected := Default()
		file := fileLayer{}
		vars := map[string]string{}
		var args []string
		for _, s := range slots() {
			name := strings.Join(s.paths[0], ".")
			if vals := draw(rt, s, "file"); rapid.Bool().Draw(rt, name+" in file") {
				for i, path := range s.paths {
					text := vals[i]
					if !s.typed && rapid.Bool().Draw(rt, name+" by reference") {
						ref := "REF_" + strings.ToUpper(strings.Join(path, "_"))
						vars[ref], text = vals[i], "${"+ref+"}"
					}
					file.put(path, literal(s, text))
				}
				apply(rt, &expected, s, vals)
			}
			if vals := draw(rt, s, "env"); rapid.Bool().Draw(rt, name+" in env") {
				for i, path := range s.paths {
					vars[envName(path)] = vals[i]
				}
				apply(rt, &expected, s, vals)
			}
			if vals := draw(rt, s, "flag"); len(s.flags) > 0 && rapid.Bool().Draw(rt, name+" in flags") {
				for i, flag := range s.flags {
					args = append(args, "--"+flag+"="+vals[i])
				}
				apply(rt, &expected, s, vals)
			}
		}
		raw, err := yaml.Marshal(file)
		if err != nil {
			rt.Fatal(err)
		}
		path := writeConfig(t, string(raw))
		if rapid.Bool().Draw(rt, "path by env") {
			vars["QDB_REST_CONFIG"] = path
		} else {
			args = append(args, "--config", path)
		}

		cfg, err := load(args, vars)
		if want := validate(expected); (err == nil) != (want == nil) {
			rt.Fatalf("Load error = %v, validate(expected) = %v", err, want)
		}
		if err == nil && cfg != expected {
			rt.Fatalf("got %+v, want %+v", cfg, expected)
		}
	})
}

// An unset ${VAR} refuses the start and names the variable.
func TestUnsetReferenceIsNamed(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		name := rapid.StringMatching(`[A-Z][A-Z0-9_]{0,11}`).Draw(rt, "name")
		path := writeConfig(t, "listen:\n  http: \"${"+name+"}\"\n")
		_, err := load([]string{"--config", path}, nil)
		if err == nil || !strings.Contains(err.Error(), name) {
			rt.Fatalf("want an error naming %s, got %v", name, err)
		}
	})
}

// A typed environment value that does not parse refuses the start and
// names the variable.
func TestMalformedEnvValueIsNamed(t *testing.T) {
	for name, value := range map[string]string{
		envPrefix + "POOL_MAX_SESSIONS": "many",
		envPrefix + "POOL_IDLE_TIMEOUT": "soon",
	} {
		_, err := load(nil, map[string]string{name: value})
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s=%s: want an error naming it, got %v", name, value, err)
		}
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	path := writeConfig(t, "listne:\n  http: \":1111\"\n")
	if _, err := load([]string{"--config", path}, nil); err == nil {
		t.Fatal("want error for unknown key, got nil")
	}
}

func TestHelpIsGNUStyle(t *testing.T) {
	var out bytes.Buffer
	_, err := Load("qdb_rest", []string{"--help"}, envFrom(nil), &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("want flag.ErrHelp, got %v", err)
	}
	if !strings.Contains(out.String(), "  --listen-tls ADDR") || strings.Contains(out.String(), "\n  -listen") {
		t.Fatalf("usage is not GNU-style:\n%s", out.String())
	}
}

func TestExampleIsTheDefaults(t *testing.T) {
	cfg, err := load([]string{"--config", "../../examples/qdb_rest.yaml"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != Default() {
		t.Fatalf("examples/qdb_rest.yaml = %+v, want %+v", cfg, Default())
	}
}

// Secret-bearing config types never render their secret.
func TestSecretsNeverLogged(t *testing.T) {
	c := Cluster{PublicKey: "PUBKEY", Username: "u", SecretKey: "S3CRET"}
	rendered := fmt.Sprint(c.LogValue())
	for _, leak := range []string{"S3CRET", "PUBKEY"} {
		if strings.Contains(rendered, leak) {
			t.Errorf("LogValue leaks %s: %s", leak, rendered)
		}
	}
}
