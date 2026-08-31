package config

import (
	"bytes"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

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

// values returns the generator for one key: its vocabulary plus one word
// outside it for the enumerated keys, and a spread of parseable words per
// type otherwise. Values that fail validation are drawn on purpose, so
// the fold is checked on the reject side as well.
func values(k key) *rapid.Generator[string] {
	if words, ok := vocab[k.path]; ok {
		return rapid.SampledFrom(append(append([]string{}, words...), "bogus"))
	}
	switch k.typ {
	case reflect.TypeFor[time.Duration]():
		return rapid.SampledFrom([]string{"0s", "1s", "60s", "500ms", "1500ms", "5m", "15m", "-1s"})
	case reflect.TypeFor[int]():
		return rapid.Map(rapid.IntRange(-1, 100001), strconv.Itoa)
	case reflect.TypeFor[int64]():
		return rapid.SampledFrom([]string{"0", "8589934592", "-1"})
	}
	return rapid.SampledFrom([]string{"", "x", "/etc/qdb/rest", "qdb://127.0.0.1:2836", "SELECT 1"})
}

// set writes text into cfg at path through the same parser the layers
// use, following the yaml tags; the expected side of the fold.
func set(t *rapid.T, cfg *Config, k key, text string) {
	t.Helper()
	value, err := parseValue(k, text)
	if err != nil {
		t.Fatalf("%s: %v", k.path, err)
	}
	v := reflect.ValueOf(cfg).Elem()
	for _, name := range strings.Split(k.path, ".") {
		for i := range v.NumField() {
			if strings.Split(v.Type().Field(i).Tag.Get("yaml"), ",")[0] == name {
				v = v.Field(i)
				break
			}
		}
	}
	v.Set(reflect.ValueOf(value))
}

// fileLayer is the YAML document under construction, as nested maps.
type fileLayer map[string]any

// put writes value at path, creating sections on the way.
func (f fileLayer) put(path string, value any) {
	parts := strings.Split(path, ".")
	section := f
	for _, name := range parts[:len(parts)-1] {
		child, ok := section[name].(fileLayer)
		if !ok {
			child = fileLayer{}
			section[name] = child
		}
		section = child
	}
	section[parts[len(parts)-1]] = value
}

// Load is the fold defaults < file < env < flags followed by validate:
// for any drawn combination of layers over every key, it returns the
// folded config exactly when validate accepts it. File values are written
// literally or as ${VAR} references, flags by their derived name or a
// qdbsh alias, and the file path arrives by flag or by QDB_REST_CONFIG.
func TestLoadIsTheLayerFold(t *testing.T) {
	byTarget := map[string]string{}
	for alias, path := range aliases {
		byTarget[path] = alias
	}
	rapid.Check(t, func(rt *rapid.T) {
		expected := Default()
		file := fileLayer{}
		vars := map[string]string{}
		var args []string
		for _, k := range keys() {
			if rapid.Bool().Draw(rt, k.path+" in file") {
				text := values(k).Draw(rt, k.path+" file")
				set(rt, &expected, k, text)
				if k.typ == reflect.TypeFor[string]() && rapid.Bool().Draw(rt, k.path+" by reference") {
					ref := "REF_" + strings.ToUpper(strings.ReplaceAll(k.path, ".", "_"))
					vars[ref], text = text, "${"+ref+"}"
				}
				file.put(k.path, text)
			}
			if rapid.Bool().Draw(rt, k.path+" in env") {
				text := values(k).Draw(rt, k.path+" env")
				set(rt, &expected, k, text)
				vars[envName(k.path)] = text
			}
			if rapid.Bool().Draw(rt, k.path+" in flags") {
				text := values(k).Draw(rt, k.path+" flag")
				set(rt, &expected, k, text)
				name := flagName(k.path)
				if alias, ok := byTarget[k.path]; ok && rapid.Bool().Draw(rt, k.path+" by alias") {
					name = alias
				}
				args = append(args, "--"+name+"="+text)
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

// A typed value that does not parse refuses the start and names the
// variable or the flag that carried it.
func TestMalformedValueIsNamed(t *testing.T) {
	for name, value := range map[string]string{
		envPrefix + "POOL_MAX_SESSIONS": "many",
		envPrefix + "POOL_IDLE_TIMEOUT": "soon",
	} {
		_, err := load(nil, map[string]string{name: value})
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("%s=%s: want an error naming it, got %v", name, value, err)
		}
	}
	_, err := load([]string{"--pool-max-sessions=many"}, nil)
	if err == nil || !strings.Contains(err.Error(), "--pool-max-sessions") {
		t.Errorf("--pool-max-sessions=many: want an error naming the flag, got %v", err)
	}
}

func TestUnknownKeyRejected(t *testing.T) {
	path := writeConfig(t, "listne:\n  http: \":1111\"\n")
	if _, err := load([]string{"--config", path}, nil); err == nil {
		t.Fatal("want error for unknown key, got nil")
	}
}

// Every key is a GNU-style long option, the two qdbsh aliases included.
func TestHelpIsGNUStyle(t *testing.T) {
	var out bytes.Buffer
	_, err := Load("qdb_rest", []string{"--help"}, envFrom(nil), &out)
	if !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("want flag.ErrHelp, got %v", err)
	}
	for _, want := range []string{"  --listen-https VALUE", "  --pool-max-sessions N", "  --cluster VALUE", "alias of --cluster-uri"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("usage lacks %q:\n%s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "\n  -listen") {
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
