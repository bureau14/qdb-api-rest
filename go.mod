module github.com/bureau14/qdb-api-rest

go 1.27

toolchain go1.27.0

require (
	github.com/apache/arrow-go/v18 v18.8.0
	github.com/bureau14/qdb-api-go/v3 v3.9.1-0.20261014065943-5b88cb766ef3
	github.com/duckdb/duckdb-go/v2 v2.10504.0
	github.com/go-jose/go-jose/v4 v4.1.4
	github.com/go-viper/mapstructure/v2 v2.5.0
	github.com/klauspost/compress v1.19.2
	github.com/knadh/koanf/parsers/yaml v1.1.1
	github.com/knadh/koanf/providers/confmap v1.0.1
	github.com/knadh/koanf/providers/rawbytes v1.0.1
	github.com/knadh/koanf/v2 v2.3.6
	golang.org/x/crypto v0.55.0
	gopkg.in/yaml.v3 v3.0.1
	pgregory.net/rapid v1.3.0
)

require (
	github.com/duckdb/duckdb-go-bindings v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-amd64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-arm64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-amd64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-arm64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/windows-amd64 v0.10504.0 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/pierrec/lz4/v4 v4.1.29 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

// The prebuilt DuckDB libraries are never compiled, so a stub replaces
// each (third_party/AGENTS.md).
replace github.com/duckdb/duckdb-go-bindings/lib/darwin-amd64 => ./third_party/duckdb-nolib/darwin-amd64

replace github.com/duckdb/duckdb-go-bindings/lib/darwin-arm64 => ./third_party/duckdb-nolib/darwin-arm64

replace github.com/duckdb/duckdb-go-bindings/lib/linux-amd64 => ./third_party/duckdb-nolib/linux-amd64

replace github.com/duckdb/duckdb-go-bindings/lib/linux-arm64 => ./third_party/duckdb-nolib/linux-arm64

replace github.com/duckdb/duckdb-go-bindings/lib/windows-amd64 => ./third_party/duckdb-nolib/windows-amd64
