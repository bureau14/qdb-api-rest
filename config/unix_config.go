// +build !windows

package config

// Secured config
var Secured = Config{
	ClusterPublicKeyFile: "/usr/share/qdb/cluster_public.key",
	TLSCertificate:       "/etc/qdb/qdb_rest.cert.pem",
	TLSCertificateKey:    "/etc/qdb/qdb_rest.key.pem",
	TLSPort:              40443,
}

// FilledDefaultConfig for unix
var FilledDefaultConfig = Config{
	AllowedOrigins:       []string{},
	ClusterURI:           "qdb://127.0.0.1:2836",
	ClusterPublicKeyFile: "",
	TLSCertificate:       "",
	TLSCertificateKey:    "",
	TLSPort:              40443,
	Host:                 "localhost",
	Port:                 40080,
	Log:                  "/var/log/qdb/qdb_rest.log",
	Assets:               "/var/lib/qdb/assets",
	MaxInBufferSize:      131072000,
}
