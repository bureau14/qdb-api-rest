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
	if restapi.APIConfig.Log != "" {
		f, err := os.OpenFile(restapi.APIConfig.Log, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			panic(err)
		}
		log.SetOutput(f)
	}
	server.TLSHost = restapi.APIConfig.TLSHost
	server.TLSPort = restapi.APIConfig.TLSPort

	if err := server.Serve(); err != nil {
		log.Fatalln(err)
	}
}
