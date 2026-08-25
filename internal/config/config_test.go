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

// A slot is one configurable thing in its three spellings: YAML keys
// under a section (the environment variable is QDB_REST_<SECTION>_<KEY>),
// and flags; plus the vocabulary drawn for it, invalid words included so
// validation is exercised too. TLS is one slot for both files because
// validate wants them together.
type slot struct {
	section string
	keys    []string
	flags   []string
	values  *rapid.Generator[string]
}

func slots() []slot {
	addresses := rapid.SampledFrom([]string{"", ":1", ":40080", "127.0.0.1:8080"})
	paths := rapid.SampledFrom([]string{"", "/etc/qdb/rest", "rel/pair"})
	levels := rapid.SampledFrom([]string{"debug", "info", "warn", "error", "verbose"})
	formats := rapid.SampledFrom([]string{"json", "console", "xml"})
	return []slot{
		{"listen", []string{"http"}, []string{"listen"}, addresses},
		{"listen", []string{"https"}, []string{"listen-tls"}, addresses},
		{"tls", []string{"certificate", "private_key"}, []string{"tls-cert", "tls-key"}, paths},
		{"log", []string{"level"}, []string{"log-level"}, levels},
		{"log", []string{"format"}, []string{"log-format"}, formats},
	}
}

func envName(section, key string) string {
	return "QDB_REST_" + strings.ToUpper(section+"_"+key)
}

// apply writes vals into cfg through the field map applyEnv itself uses.
func apply(cfg *Config, s slot, vals []string) {
	fields := envOverrides(cfg)
	for i, key := range s.keys {
		*fields[envName(s.section, key)] = vals[i]
	}
}

// draw returns one value per key of s: the drawn word, suffixed per key
// for multi-key slots so the pair is distinguishable yet both-or-neither.
func draw(rt *rapid.T, s slot, layer string) []string {
	word := s.values.Draw(rt, s.flags[0]+" "+layer)
	out := make([]string, len(s.keys))
	for i, key := range s.keys {
		out[i] = word
		if word != "" && len(s.keys) > 1 {
			out[i] = word + "." + key
		}
	}
	return out
}

// renderYAML lays the file layer out as sections of quoted scalars.
func renderYAML(file map[string]map[string]string) string {
	var b strings.Builder
	for _, section := range []string{"listen", "tls", "log"} {
		if len(file[section]) == 0 {
			continue
		}
		fmt.Fprintf(&b, "%s:\n", section)
		for key, text := range file[section] {
			fmt.Fprintf(&b, "  %s: %q\n", key, text)
		}
	}
	return b.String()
}

// Load is the fold defaults < file < env < flags followed by validate:
// for any drawn combination of layers, it returns the folded config
// exactly when validate accepts it. File values are written literally or
// as ${VAR} references, and the file path arrives by flag or by
// QDB_REST_CONFIG.
func TestLoadIsTheLayerFold(t *testing.T) {
	rapid.Check(t, func(rt *rapid.T) {
		expected := Default()
		file := map[string]map[string]string{"listen": {}, "tls": {}, "log": {}}
		vars := map[string]string{}
		var args []string
		for _, s := range slots() {
			if vals := draw(rt, s, "file"); rapid.Bool().Draw(rt, s.flags[0]+" in file") {
				for i, key := range s.keys {
					text := vals[i]
					if rapid.Bool().Draw(rt, key+" by reference") {
						name := "REF_" + strings.ToUpper(key)
						vars[name], text = vals[i], "${"+name+"}"
					}
					file[s.section][key] = text
				}
				apply(&expected, s, vals)
			}
			if vals := draw(rt, s, "env"); rapid.Bool().Draw(rt, s.flags[0]+" in env") {
				for i, key := range s.keys {
					vars[envName(s.section, key)] = vals[i]
				}
				apply(&expected, s, vals)
			}
			if vals := draw(rt, s, "flag"); rapid.Bool().Draw(rt, s.flags[0]+" in flags") {
				for i, name := range s.flags {
					args = append(args, "--"+name+"="+vals[i])
				}
				apply(&expected, s, vals)
			}
		}
		path := writeConfig(t, renderYAML(file))
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
