//go:build windows
// +build windows

package config

// Secured config
var Secured = Config{
	ClusterPublicKeyFile: "C:/Program Files/quasardb/share/cluster_public.key",
	RestPrivateKeyFile:   "C:/Program Files/quasardb/conf/qdb_rest_private.key",
	TLSCertificate:       "C:/Program Files/quasardb/conf/qdb_rest.cert.pem",
	TLSCertificateKey:    "C:/Program Files/quasardb/conf/qdb_rest.key.pem",
	TLSPort:              40443,
}

// FilledDefaultConfig for windows
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
	Log:                  "C:/Program Files/quasardb/log/qdb_rest.log",
	LogMaxSize:           100 * 1024 * 1024,
	LogMaxRetention:      5,
	LogMaxAge:            8 * 60 * 60,
	LogCompress:          false,
	Assets:               "C:/Program Files/quasardb//assets",
	MaxInBufferSize:      131072000,
	ParallelismCount:     1,
	PoolSize:             1,
}
