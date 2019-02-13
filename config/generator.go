package config

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"reflect"
	"strconv"
	"strings"
)

func getQuestion(field string) string {
	fieldToQuestion := map[string]string{
		"ClusterURI":           "Enter the location of the QuasarDB cluster",
		"ClusterPublicKeyFile": "Enter the path of the QuasarDB cluster public key file",
		"TLSCertificate":       "Enter the path of the tls certificate",
		"TLSKey":               "Enter the path of the tls key",
		"TLSPort":              "Enter a port for the tls connection",
		"Host":                 "Enter an host name or address",
		"Port":                 "Enter a port",
		"Log":                  "Enter the path of the log file",
		"Assets":               "Enter the path of the asset directory",
	}
	question, ok := fieldToQuestion[field]
	if !ok {
		panic(fmt.Errorf("field not found"))
	}
	return question
}

func readLine() string {
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return line[:len(line)-1]
}

func getIntOrDefault(def int64) (int64, bool) {
	line := readLine()
	if line == "" {
		return def, true
	}
	value, err := strconv.ParseInt(line, 10, 64)
	if err == nil {
		return value, true
	}
	fmt.Println("     Error, only integer values are accepted.")
	return -1, false
}

func getStringOrDefault(def string) string {
	line := readLine()
	var value string
	if line == "" {
		value = def
	} else {
		value = strings.TrimSpace(line)
		if value == "" {
			fmt.Println("(empty)")
		}
	}

	return value
}

func ask(field string, conf *Config, defConfig Config) bool {
	val := reflect.ValueOf(*conf).FieldByName(field)
	element := reflect.ValueOf(conf).Elem().FieldByName(field)
	def := reflect.ValueOf(defConfig).FieldByName(field)
	fmt.Printf(" - %s [%v]: ", getQuestion(field), def)

	if val.Kind() == reflect.Int {
		value, ok := getIntOrDefault(def.Int())
		if !ok {
			return false
		}
		element.SetInt(value)
	} else if val.Kind() == reflect.String {
		element.SetString(getStringOrDefault(def.String()))
	}
	return true
}

func askYesNo(question string) bool {
	fmt.Printf("%s? [Yn]: ", question)
	agreed := strings.ToLower(readLine())
	if agreed == "n" || agreed == "no" {
		fmt.Println("")
		return false
	} else if agreed == "y" || agreed == "yes" || agreed == "" {
		return true
	}
	fmt.Println("Please answer with Y(es) or n(o)")
	return askYesNo(question)
}

func writeConfig(configPath string, conf Config) error {

	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		if !askYesNo(fmt.Sprintf("Do you want to overwrite %s", configPath)) {
			return fmt.Errorf("Will not overwrite")
		}
	}
	confJSON, err := json.MarshalIndent(conf, "", "    ")
	if err != nil {
		return err
	}
	return ioutil.WriteFile(configPath, confJSON, 0644)
}

func askQuestions(defConfig Config) Config {
	conf := Config{}
	fmt.Println("Press enter to keep the [default] value: ")
	ask("ClusterURI", &conf, defConfig)
	ask("ClusterPublicKeyFile", &conf, defConfig)
	ask("TLSCertificate", &conf, defConfig)
	ask("TLSKey", &conf, defConfig)
	ask("TLSPort", &conf, defConfig)
	ask("Host", &conf, defConfig)

	ok := false
	for !ok {
		ok = ask("Port", &conf, defConfig)
	}

	ask("Log", &conf, defConfig)
	ask("Assets", &conf, defConfig)
	return conf
}

// Generate a configuration file if needed
func Generate(defConfig Config) bool {
	conf := askQuestions(defConfig)

	if err := conf.validate(); err != nil {
		fmt.Println("")
		fmt.Printf("Error: %s\n", err.Error())
		fmt.Println("")
		return Generate(conf)
	}
	fmt.Println("Configuration is valid.")

	fmt.Println("")

	conf.Print()
	if !askYesNo("Do you agree with the above") {
		return Generate(conf)
	}

	fmt.Println("")

	defaultConfigPath := "qdb_rest.conf"
	fmt.Printf("Please enter the path where you wish to store the configuration [%s]: ", defaultConfigPath)
	configPath := getStringOrDefault(defaultConfigPath)
	if err := writeConfig(configPath, conf); err != nil {
		fmt.Printf("Could not create file at %s: %s\n", configPath, err.Error())
	} else {
		fmt.Printf("File successfully written at %s\n", configPath)
	}

	return true
}
