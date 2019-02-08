// +build windows

package config

// FilledDefaultConfig for windows
var FilledDefaultConfig = Config{
	AllowedOrigins:       []string{},
	ClusterURI:           "qdb://127.0.0.1:2836",
	ClusterPublicKeyFile: "C:/Program Files/quasardb/share/cluster_public.key",
	TLSCertificate:       "C:/Program Files/quasardb/conf/qdb_rest.cert.pem",
	TLSKey:               "C:/Program Files/quasardb/conf/qdb_rest.key.pem",
	Host:                 "localhost",
	Port:                 40000,
	Log:                  "C:/Program Files/quasardb/log/qdb_rest.log",
	Assets:               "C:/Program Files/quasardb//assets",
}
