// +build !windows

package config

var defaultConfig = Config{
	AllowedOrigins:       []string{},
	ClusterURI:           "qdb://127.0.0.1:2836",
	ClusterPublicKeyFile: "/usr/share/qdb/cluster_public.key",
	TLSCertificate:       "/etc/qdb/qdb_rest.cert.pem",
	TLSKey:               "/etc/qdb/qdb_rest.key.pem",
	Host:                 "0.0.0.0",
	Port:                 40000,
	Log:                  "/var/log/qdb/qdb_rest.log",
	Assets:               "/var/lib/qdb/assets",
}
