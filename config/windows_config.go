// +build windows

package config

// Secured config
var Secured = Config{
	ClusterPublicKeyFile: "C:/Program Files/quasardb/share/cluster_public.key",
	TLSCertificate:       "C:/Program Files/quasardb/conf/qdb_rest.cert.pem",
	TLSCertificateKey:    "C:/Program Files/quasardb/conf/qdb_rest.key.pem",
	TLSPort:              40493,
}

// FilledDefaultConfig for windows
var FilledDefaultConfig = Config{
	AllowedOrigins:       []string{},
	ClusterURI:           "qdb://127.0.0.1:2836",
	ClusterPublicKeyFile: "",
	TLSCertificate:       "",
	TLSCertificateKey:    "",
	TLSPort:              40493,
	Host:                 "localhost",
	Port:                 40080,
	Log:                  "C:/Program Files/quasardb/log/qdb_rest.log",
	Assets:               "C:/Program Files/quasardb//assets",
}
