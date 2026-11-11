module github.com/bureau14/qdb-api-rest

go 1.27

toolchain go1.27.0

require (
	github.com/apache/arrow-adbc/go/adbc v1.13.0
	github.com/apache/arrow-adbc/go/driver v1.13.0
	github.com/apache/arrow-go/v18 v18.8.0
	github.com/bureau14/qdb-api-go/v3 v3.14.4-0.20261105121308-54040c8e91d0
	github.com/duckdb/duckdb-go/v2 v2.20000.0
	github.com/go-jose/go-jose/v4 v4.1.9
	github.com/go-viper/mapstructure/v2 v2.5.0
	github.com/klauspost/compress v1.20.4
	github.com/knadh/koanf/parsers/yaml v1.1.1
	github.com/knadh/koanf/providers/confmap v1.0.1
	github.com/knadh/koanf/providers/rawbytes v1.0.1
	github.com/knadh/koanf/v2 v2.4.1
	github.com/prometheus/client_golang v1.25.0
	golang.org/x/crypto v0.58.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261110000943-09e21b237f2c
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
	gopkg.in/yaml.v3 v3.0.1
	pgregory.net/rapid v1.3.0
)

require (
	cloud.google.com/go/compute/metadata v0.10.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/bluele/gcache v0.0.2 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/duckdb/duckdb-go-bindings v0.20000.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/goccy/go-json v0.12.0 // indirect
	github.com/google/flatbuffers v25.12.19+incompatible // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.33.0 // indirect
	github.com/klauspost/cpuid/v2 v2.4.2 // indirect
	github.com/knadh/koanf/maps v0.1.3 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pandatix/go-abnf v0.5.5 // indirect
	github.com/pierrec/lz4/v4 v4.1.33 // indirect
	github.com/prometheus/client_model v0.6.3 // indirect
	github.com/prometheus/common v0.73.1 // indirect
	github.com/prometheus/procfs v0.23.0 // indirect
	github.com/stoewer/go-strcase v1.3.1 // indirect
	github.com/stretchr/testify v1.12.1 // indirect
	github.com/zeebo/xxh3 v1.1.0 // indirect
	go.duckdb.org/libduckdb/darwin-amd64 v0.20000.0 // indirect
	go.duckdb.org/libduckdb/darwin-arm64 v0.20000.0 // indirect
	go.duckdb.org/libduckdb/linux-amd64 v0.20000.0 // indirect
	go.duckdb.org/libduckdb/linux-arm64 v0.20000.0 // indirect
	go.duckdb.org/libduckdb/windows-amd64 v0.20000.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.72.0 // indirect
	go.opentelemetry.io/otel v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.47.0 // indirect
	go.opentelemetry.io/otel/exporters/stdout/stdouttrace v1.47.0 // indirect
	go.opentelemetry.io/otel/log v1.47.0 // indirect
	go.opentelemetry.io/otel/metric v1.47.0 // indirect
	go.opentelemetry.io/otel/sdk v1.47.0 // indirect
	go.opentelemetry.io/otel/trace v1.47.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.1 // indirect
	go.yaml.in/yaml/v3 v3.0.6 // indirect
	golang.org/x/exp v0.0.0-20261109185158-a52e3aef5920 // indirect
	golang.org/x/mod v0.43.0 // indirect
	golang.org/x/net v0.62.0 // indirect
	golang.org/x/oauth2 v0.38.0 // indirect
	golang.org/x/sync v0.25.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
	golang.org/x/text v0.44.0 // indirect
	golang.org/x/tools v0.52.1-0.20261106145633-d407941a339a // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20261110000943-09e21b237f2c // indirect
)

// The prebuilt DuckDB libraries are never compiled, so a stub replaces
// each (third_party/AGENTS.md).
replace go.duckdb.org/libduckdb/darwin-amd64 => ./third_party/duckdb-nolib/darwin-amd64

replace go.duckdb.org/libduckdb/darwin-arm64 => ./third_party/duckdb-nolib/darwin-arm64

replace go.duckdb.org/libduckdb/linux-amd64 => ./third_party/duckdb-nolib/linux-amd64

replace go.duckdb.org/libduckdb/linux-arm64 => ./third_party/duckdb-nolib/linux-arm64

replace go.duckdb.org/libduckdb/windows-amd64 => ./third_party/duckdb-nolib/windows-amd64
