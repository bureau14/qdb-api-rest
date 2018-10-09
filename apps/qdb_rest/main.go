package main

import (
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

	parser := flags.NewParser(server, flags.Default)
	parser.ShortDescription = "QuasarDB API"
	parser.LongDescription = "Find out more at https://doc.quasardb.net"

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

	server.ConfigureAPI()
	// this must be done after the api has been configured
	// because restapi.APIConfig is configured there
	if restapi.APIConfig.TLSCertificate != "" && restapi.APIConfig.TLSKey != "" {
		if server.TLSHost == "" {
			server.TLSHost = restapi.APIConfig.Host
		}
		if server.TLSPort == 0 {
			server.TLSPort = restapi.APIConfig.Port
		}
		server.EnabledListeners = []string{"https"}
	} else {
		if server.Host == "localhost" && restapi.APIConfig.Host != "" {
			server.Host = restapi.APIConfig.Host
		}
		if server.Port == 0 {
			server.Port = restapi.APIConfig.Port
		}
		server.EnabledListeners = []string{"http"}
	}

	if err := server.Serve(); err != nil {
		log.Fatalln(err)
	}
}
