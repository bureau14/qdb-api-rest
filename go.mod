module github.com/bureau14/qdb-api-rest

go 1.27

toolchain go1.27.0

require (
	github.com/apache/arrow-adbc/go/adbc v1.12.0
	github.com/apache/arrow-go/v18 v18.8.0
	github.com/bureau14/qdb-api-go/v3 v3.9.1-0.20261025052437-6afd5e8cae12
	github.com/duckdb/duckdb-go/v2 v2.10504.0
	github.com/go-jose/go-jose/v4 v4.1.4
	github.com/go-viper/mapstructure/v2 v2.5.0
	github.com/klauspost/compress v1.19.2
	github.com/knadh/koanf/parsers/yaml v1.1.1
	github.com/knadh/koanf/providers/confmap v1.0.1
	github.com/knadh/koanf/providers/rawbytes v1.0.1
	github.com/knadh/koanf/v2 v2.3.6
	golang.org/x/crypto v0.55.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa
	google.golang.org/grpc v1.83.2
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
	pgregory.net/rapid v1.3.0
)

require (
	cloud.google.com/go/compute/metadata v0.9.0 // indirect
	github.com/bluele/gcache v0.0.2 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/duckdb/duckdb-go-bindings v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-amd64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/darwin-arm64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-amd64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/linux-arm64 v0.10504.0 // indirect
	github.com/duckdb/duckdb-go-bindings/lib/windows-amd64 v0.10504.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/goccy/go-json v0.10.6 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.29.0 // indirect
	github.com/klauspost/cpuid/v2 v2.4.0 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/pierrec/lz4/v4 v4.1.29 // indirect
	github.com/stoewer/go-strcase v1.3.1 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.44.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/sdk v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	go.opentelemetry.io/proto/otlp v1.10.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/exp v0.0.0-20260112195511-716be5621a96 // indirect
	golang.org/x/mod v0.38.0 // indirect
	golang.org/x/net v0.58.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.22.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
	golang.org/x/text v0.41.0 // indirect
	golang.org/x/tools v0.48.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260526163538-3dc84a4a5aaa // indirect
)

// The prebuilt DuckDB libraries are never compiled, so a stub replaces
// each (third_party/AGENTS.md).
replace github.com/duckdb/duckdb-go-bindings/lib/darwin-amd64 => ./third_party/duckdb-nolib/darwin-amd64

replace github.com/duckdb/duckdb-go-bindings/lib/darwin-arm64 => ./third_party/duckdb-nolib/darwin-arm64

replace github.com/duckdb/duckdb-go-bindings/lib/linux-amd64 => ./third_party/duckdb-nolib/linux-amd64

replace github.com/duckdb/duckdb-go-bindings/lib/linux-arm64 => ./third_party/duckdb-nolib/linux-arm64

replace github.com/duckdb/duckdb-go-bindings/lib/windows-amd64 => ./third_party/duckdb-nolib/windows-amd64
