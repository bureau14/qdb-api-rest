package config

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"reflect"
	"strings"

	"github.com/jessevdk/go-flags"
)

// Config : A configuration file for the rest api
type Config struct {
	AllowedOrigins       []string       `json:"allowed_origins" long:"allowed-origins" description:"Allowed origins for cross origins"`
	ClusterURI           string         `json:"cluster_uri" short:"c" long:"cluster" description:"URI of the cluster we connect to" env:"QDB_CLUSTER_URI"`
	ClusterPublicKeyFile flags.Filename `json:"cluster_public_key_file" long:"cluster-public-key-file" description:"Key file used for cluster security" env:"QDB_CLUSTER_PUBLIC_KEY_FILE"`
	TLSCertificate       flags.Filename `json:"tls_certificate" long:"tls-certificate" description:"The certificate to use for secure connections" env:"TLS_CERTIFICATE"`
	TLSCertificateKey    flags.Filename `json:"tls_key" long:"tls-key" description:"The private key to use for secure conections" env:"TLS_PRIVATE_KEY"`
	TLSPort              int            `json:"tls_port" long:"tls-port" description:"The port to listen on for secure connections" env:"TLS_PORT"`
	Host                 string         `json:"host" long:"host" description:"The IP to listen on" default:"localhost" env:"HOST"`
	Port                 int            `json:"port" long:"port" description:"The port to listen on for insecure connections, defaults to a random value" env:"PORT"`
	Log                  flags.Filename `json:"log" long:"log-file" description:"The path to the log file" env:"QDB_REST_LOG_FILE"`
	Assets               string         `json:"assets" long:"assets-dir" description:"The path to the assets directory you want to be published alongside the rest api" env:"QDB_REST_ASSETS_DIR"`
	MaxInBufferSize      uint           `json:"max_in_buffer_size" long:"max-in-buffer-size" description:"The maximum input buffer size coming from the server" env:"QDB_MAX_IN_BUFFER_SIZE"`

	ConfigFile  flags.Filename `json:"-" long:"config-file" description:"Config file to setup the rest api"`
	GenConfig   bool           `json:"-" long:"gen-config" description:"Generate a config"`
	Interactive bool           `json:"-" short:"i" long:"interactive" description:"Switch on interactive mode for gen-config, does nothing if gen-config is not set"`
	Local       bool           `json:"-" short:"l" long:"local" description:"Switch on local mode"`
	Secure      bool           `json:"-" short:"s" long:"secure" description:"Switch on security default parameters (tls + cluster security)"`
}

// SetSecured set config to secured mode
func (c *Config) SetSecured() {
	if c.ClusterPublicKeyFile == FilledDefaultConfig.ClusterPublicKeyFile {
		c.ClusterPublicKeyFile = Secured.ClusterPublicKeyFile
	}
	if c.TLSCertificate == FilledDefaultConfig.TLSCertificate {
		c.TLSCertificate = Secured.TLSCertificate
	}
	if c.TLSCertificateKey == FilledDefaultConfig.TLSCertificateKey {
		c.TLSCertificateKey = Secured.TLSCertificateKey
	}
	if c.TLSPort == FilledDefaultConfig.TLSPort {
		c.TLSPort = Secured.TLSPort
	}
}

// Local config
var Local = Config{
	Host:   "localhost",
	Port:   40080,
	Log:    "qdb_rest.log",
	Assets: "assets",
}

// SetLocal set config to local mode
func (c *Config) SetLocal() {
	if c.Host == FilledDefaultConfig.Host {
		c.Host = Local.Host
	}
	if c.Port == FilledDefaultConfig.Port {
		c.Port = Local.Port
	}
	if c.Log == FilledDefaultConfig.Log {
		c.Log = Local.Log
	}
	if c.Assets == FilledDefaultConfig.Assets {
		c.Assets = Local.Assets
	}
}

// SetDefaults : set defaults values if there are no config values
func (c *Config) SetDefaults() {
	if c.Local {
		c.SetLocal()
	}
	if c.Secure {
		c.SetSecured()
	}
	if c.GenConfig {
		if c.Interactive {
			Generate(*c)
		} else {
			confJSON, err := json.MarshalIndent(*c, "", "    ")
			if err != nil {
				fmt.Println(err)
				os.Exit(0)
			}
			fmt.Print(string(confJSON))
		}
		os.Exit(0)
	}
	filename := string(c.ConfigFile)

	if filename == "" {
		return
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

	// if c.AllowedOrigins == FilledDefaultConfig.AllowedOrigins && config.AllowedOrigins != nil {
	// 	c.AllowedOrigins = config.AllowedOrigins
	// }
	if c.ClusterURI == FilledDefaultConfig.ClusterURI {
		c.ClusterURI = config.ClusterURI
	}
	if c.ClusterPublicKeyFile == FilledDefaultConfig.ClusterPublicKeyFile {
		c.ClusterPublicKeyFile = config.ClusterPublicKeyFile
	}
	if c.TLSCertificate == FilledDefaultConfig.TLSCertificate {
		c.TLSCertificate = config.TLSCertificate
	}
	if c.TLSCertificateKey == FilledDefaultConfig.TLSCertificateKey {
		c.TLSCertificateKey = config.TLSCertificateKey
	}
	if c.TLSPort == FilledDefaultConfig.TLSPort {
		c.TLSPort = config.TLSPort
	}
	if c.Host == FilledDefaultConfig.Host {
		c.Host = config.Host
	}
	if c.Port == FilledDefaultConfig.Port {
		c.Port = config.Port
	}
	if c.Log == FilledDefaultConfig.Log {
		c.Log = config.Log
	}
	if c.Assets == FilledDefaultConfig.Assets {
		c.Assets = config.Assets
	}
	if c.MaxInBufferSize == FilledDefaultConfig.MaxInBufferSize && config.MaxInBufferSize != 0 {
		c.MaxInBufferSize = config.MaxInBufferSize
	}

}

func (c *Config) statFiles() (bool, bool, bool) {
	clusterKeyFile := false
	if c.ClusterPublicKeyFile != "" {
		if _, err := os.Stat(string(c.ClusterPublicKeyFile)); os.IsNotExist(err) {
			log.Printf("Warning: cannot find cluster public key file at location %s , assuming non-secure cluster configuration.\n", c.ClusterPublicKeyFile)
		} else {
			clusterKeyFile = true
		}
	}

	tlsCert := false
	if c.TLSCertificate != "" {
		if _, err := os.Stat(string(c.TLSCertificate)); os.IsNotExist(err) {
			log.Printf("Warning: cannot find tls certificate at location %s , assuming http configuration.\n", c.TLSCertificate)
		} else {
			tlsCert = true
		}
	}

	tlsKey := false
	if c.TLSCertificateKey != "" {
		if _, err := os.Stat(string(c.TLSCertificateKey)); os.IsNotExist(err) {
			log.Printf("Warning: cannot find tls key at location %s , assuming http configuration.\n", c.TLSCertificateKey)
		} else {
			tlsKey = true
		}
	}

	return clusterKeyFile, tlsCert, tlsKey
}

func addError(err error, msg string) error {
	if err != nil {
		return fmt.Errorf("%s\n%s", err.Error(), msg)
	}
	return fmt.Errorf("%s", msg)
}

func (c Config) validate() error {
	c.statFiles()
	var err error
	if c.ClusterPublicKeyFile != "" && (c.TLSCertificate == "" || c.TLSCertificateKey == "") {
		err = addError(err, "a secured cluster configuration cannot be valid without a proper tls configuration")
	} else if (c.TLSCertificate == "" && c.TLSCertificateKey != "") || (c.TLSCertificate != "" && c.TLSCertificateKey == "") {
		err = addError(err, "you need both tls key and certificate for tls configuration")
	} else {
		err = nil
	}

	if err != nil && c.TLSCertificate == "" {
		err = addError(err, "Please enter a tls certificate path")
	}
	if err != nil && c.TLSCertificateKey == "" {
		err = addError(err, "Please enter a tls key path")
	}
	if c.MaxInBufferSize < 1500 {
		err = addError(err, "MaxInBufferSize too small")
	}
	return err
}

// IsSecurityEnabled returns true when the cluster security is enabled
func (c *Config) IsSecurityEnabled() bool {
	return strings.TrimSpace(string(c.ClusterPublicKeyFile)) != ""
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
		c.TLSCertificateKey = ""
	}

	return nil
}

// Print the configuration
func (c Config) Print() {
	v := reflect.ValueOf(c)

	fmt.Println("Configuration:")
	for i := 0; i < v.NumField(); i++ {
		if v.Type().Field(i).Tag.Get("json") != "-" {
			fmt.Printf(" - %s: %v\n", v.Type().Field(i).Name, v.Field(i))
		}
	}
}
