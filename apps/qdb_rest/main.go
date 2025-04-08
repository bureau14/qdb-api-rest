package main

import (
	"github.com/bureau14/qdb-api-rest/meta"
	"log"
	"os"

	"github.com/bureau14/qdb-api-rest/restapi"
	"github.com/bureau14/qdb-api-rest/restapi/operations"
	loads "github.com/go-openapi/loads"
	flags "github.com/jessevdk/go-flags"
)

func main() {
	swaggerSpec, err := loads.Embedded(restapi.SwaggerJSON, restapi.FlatSwaggerJSON)
	if err != nil {
		panic(err)
	}

	api := operations.NewQdbAPIRestAPI(swaggerSpec)
	server := restapi.NewServer(api)
	defer server.Shutdown()

	parser := flags.NewParser(&restapi.APIConfig, flags.Default)
	parser.ShortDescription = "QuasarDB API"
	parser.LongDescription = `Find out more at https://doc.quasardb.net

	The corresponding [$VAR] variable can be set in the environment.`

	server.ConfigureFlags()
	for _, optsGroup := range api.CommandLineOptionsGroups {
		_, err := parser.AddGroup(optsGroup.ShortDescription, optsGroup.LongDescription, optsGroup.Options)
		if err != nil {
			panic(err)
		}
	}

	if _, err := parser.Parse(); err != nil {
		code := 1
		if fe, ok := err.(*flags.Error); ok {
			if fe.Type == flags.ErrHelp {
				code = 0
			}
		}
		os.Exit(code)
	}

	if restapi.APIConfig.Version {
		meta.GetVersionInfo()
	}

	server.ConfigureAPI()
	// this must be done after the api has been configured
	// because restapi.APIConfig is configured there
	server.Host = restapi.APIConfig.Host
	server.Port = restapi.APIConfig.Port
	server.EnabledListeners = []string{"http"}
	if restapi.APIConfig.TLSCertificate != "" && restapi.APIConfig.TLSCertificateKey != "" {
		server.TLSHost = restapi.APIConfig.Host
		server.TLSPort = restapi.APIConfig.TLSPort
		server.EnabledListeners = append(server.EnabledListeners, "https")
	}

	if err := server.Serve(); err != nil {
		log.Fatalln(err)
	}
}
