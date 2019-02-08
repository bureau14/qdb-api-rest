package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"reflect"
)

// Config : A configuration file for the rest api
type Config struct {
	AllowedOrigins       []string `json:"allowed_origins" required:"true"`
	ClusterURI           string   `json:"cluster_uri" required:"true"`
	ClusterPublicKeyFile string   `json:"cluster_public_key_file" required:"true"`
	TLSCertificate       string   `json:"tls_certificate" required:"true"`
	TLSKey               string   `json:"tls_key" required:"true"`
	Host                 string   `json:"host" required:"true"`
	Port                 int      `json:"port" required:"true"`
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

func (c *Config) statFiles() (bool, bool, bool) {
	clusterKeyFile := false
	if _, err := os.Stat(c.ClusterPublicKeyFile); !os.IsNotExist(err) {
		log.Printf("Warning: cannot find cluster public key file at location %s , assuming non-secure cluster configuration.\n", c.ClusterPublicKeyFile)
		clusterKeyFile = true
	}

	tlsCert := false
	if _, err := os.Stat(c.TLSCertificate); !os.IsNotExist(err) {
		log.Printf("Warning: cannot find tls certificate at location %s , assuming http configuration.\n", c.TLSCertificate)
		tlsCert = true
	}

	tlsKey := false
	if _, err := os.Stat(c.TLSKey); !os.IsNotExist(err) {
		log.Printf("Warning: cannot find tls key at location %s , assuming http configuration.\n", c.TLSKey)
		tlsKey = true
	}

	return clusterKeyFile, tlsCert, tlsKey
}

func (c Config) validate() error {
	c.statFiles()
	var err error
	if c.ClusterPublicKeyFile != "" && (c.TLSCertificate == "" || c.TLSKey == "") {
		err = fmt.Errorf("a secured cluster configuration cannot be valid without a proper tls configuration")
	} else if (c.TLSCertificate == "" && c.TLSKey != "") || (c.TLSCertificate != "" && c.TLSKey == "") {
		err = fmt.Errorf("you need both tls key and certificate for tls configuration")
	} else {
		err = nil
	}

	if err != nil && c.TLSCertificate == "" {
		err = fmt.Errorf("%s\n%s", err.Error(), "Please enter a tls certificate path")
	}
	if err != nil && c.TLSKey == "" {
		err = fmt.Errorf("%s\n%s", err.Error(), "Please enter a tls key path")
	}
	return err
}

// Check : check the configuration to test for basic security features
func (c *Config) Check() error {
	clusterKeyFile, tlsCert, tlsKey := c.statFiles()

	if clusterKeyFile && (!tlsCert || !tlsKey) {
		log.Fatalln("Error: cannot find TLS certificate while creating secured cluster.")
		return fmt.Errorf("Error: cannot find TLS certificate while creating secured cluster")
	}

	if !clusterKeyFile {
		c.ClusterPublicKeyFile = ""
	}

	if !tlsCert {
		c.TLSCertificate = ""
	}

	if !tlsKey {
		c.TLSKey = ""
	}

	return nil
}

// Print the configuration
func (c Config) Print() {
	v := reflect.ValueOf(c)

	fmt.Println("Configuration:")
	for i := 0; i < v.NumField(); i++ {
		fmt.Printf(" - %s: %v\n", v.Type().Field(i).Name, v.Field(i))
	}
}
