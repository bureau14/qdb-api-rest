// +build !windows

package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
)

// Config : A configuration file for the rest api
type Config struct {
	AllowedOrigins       []string `json:"allowed_origins" required:"true"`
	ClusterURI           string   `json:"cluster_uri" required:"true"`
	ClusterPublicKeyFile string   `json:"cluster_public_key_file" required:"true"`
	TLSCertificate       string   `json:"tls_certificate" required:"true"`
	TLSKey               string   `json:"tls_key" required:"true"`
	Host                 string   `json:"tls_host" required:"true"`
	Port                 int      `json:"tls_port" required:"true"`
	Log                  string   `json:"log"`
	Assets               string   `json:"assets"`
}

var defaultConfig = Config{
	AllowedOrigins:       []string{},
	ClusterURI:           "qdb://127.0.0.1:2836",
	ClusterPublicKeyFile: "",
	TLSCertificate:       "",
	TLSKey:               "",
	Host:                 "0.0.0.0",
	Port:                 40000,
	Log:                  "",
	Assets:               "",
}

// SetDefaults : set defaults values if there are no config values
func SetDefaults(filename string) Config {
	if filename == "" {
		return defaultConfig
	}
	content, err := ioutil.ReadFile(filename)
	if err != nil {
		panic(err)
	}

	var config Config
	err = json.Unmarshal(content, &config)
	if err != nil {
		panic(err)
	}

	return config
}

// Check : check the configuration to test for basic security features
func (c *Config) Check() error {
	clusterKeyFile := false
	if _, err := os.Stat(c.ClusterPublicKeyFile); !os.IsNotExist(err) {
		clusterKeyFile = true
	}

	tlsCert := false
	if _, err := os.Stat(c.TLSCertificate); !os.IsNotExist(err) {
		tlsCert = true
	}

	tlsKey := false
	if _, err := os.Stat(c.TLSKey); !os.IsNotExist(err) {
		tlsKey = true
	}

	if clusterKeyFile && (!tlsCert || !tlsKey) {
		log.Fatalln("Error: cannot find TLS certificate while creating secured cluster.")
		return fmt.Errorf("Error: cannot find TLS certificate while creating secured cluster")
	}

	if !clusterKeyFile {
		log.Printf("Warning: cannot find cluster public key file at location %s , assuming non-secure cluster configuration.\n", c.ClusterPublicKeyFile)
		c.ClusterPublicKeyFile = ""
	}

	if !tlsCert {
		log.Printf("Warning: cannot find tls certificate at location %s , assuming http configuration.\n", c.TLSCertificate)
		c.TLSCertificate = ""
	}

	if !tlsKey {
		log.Printf("Warning: cannot find tls key at location %s , assuming http configuration.\n", c.TLSKey)
		c.TLSKey = ""
	}

	return nil
}
