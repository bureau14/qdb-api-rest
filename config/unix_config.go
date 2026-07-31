//go:build !windows
// +build !windows

package config

// Secured config
var Secured = Config{
	ClusterPublicKeyFile: "/usr/share/qdb/cluster_public.key",
	RestPrivateKeyFile:   "/etc/qdb/qdb_rest_private.key",
	TLSCertificate:       "/etc/qdb/qdb_rest.cert.pem",
	TLSCertificateKey:    "/etc/qdb/qdb_rest.key.pem",
	TLSPort:              40443,
}

// FilledDefaultConfig for unix
var FilledDefaultConfig = Config{
	AllowedOrigins:       []string{},
	ClusterURI:           "qdb://127.0.0.1:2836",
	ClusterPublicKeyFile: "",
	RestPrivateKeyFile:   "",
	ReadinessQuery:       "",
	TLSCertificate:       "",
	TLSCertificateKey:    "",
	TLSPort:              40443,
	Host:                 "localhost",
	Port:                 40080,
	Log:                  "/var/log/qdb/qdb_rest.log",
	LogMaxSizeBytes:      100 * 1024 * 1024,
	LogMaxRetention:      5,
	LogMaxAgeSeconds:     8 * 60 * 60,
	LogCompress:          false,
	Assets:               "/var/lib/qdb/assets",
	MaxInBufferSize:      131072000,
	ParallelismCount:     1,
	PoolSize:             1,
}
