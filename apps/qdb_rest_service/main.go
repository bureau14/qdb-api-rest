package main

import (
	"log"
	"os"
	"time"

	"github.com/bureau14/qdb-api-rest/restapi"
	"github.com/bureau14/qdb-api-rest/restapi/operations"
	"github.com/kardianos/service"
	"golang.org/x/sys/windows/registry"

	loads "github.com/go-openapi/loads"
	flags "github.com/jessevdk/go-flags"
)

var logger service.Logger

type program struct {
	server *restapi.Server
}

func (p *program) Start(s service.Service) error {
	// Start should not block. Do the actual work async.
	go p.run(s.String())
	return nil
}

func (prg *program) run(name string) {

	swaggerSpec, err := loads.Embedded(restapi.SwaggerJSON, restapi.FlatSwaggerJSON)
	if err != nil {
		log.Fatalln(err)
	}

	api := operations.NewQdbAPIRestAPI(swaggerSpec)
	prg.server = restapi.NewServer(api)

	parser := flags.NewParser(prg.server, flags.Default)
	parser.ShortDescription = "QuasarDB API"
	parser.LongDescription = "Find out more at https://doc.quasardb.net"

	prg.server.ConfigureFlags()
	for _, optsGroup := range api.CommandLineOptionsGroups {
		_, err := parser.AddGroup(optsGroup.ShortDescription, optsGroup.LongDescription, optsGroup.Options)
		if err != nil {
			log.Fatalln(err)
		}
	}

	registryPath := `SYSTEM\CurrentControlSet\Services` + name
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, registryPath, registry.QUERY_VALUE)
	if err != nil {
		log.Fatalln(err)
	}
	configFile, _, err := k.GetStringValue("ConfigFile")
	if err != nil {
		log.Fatalln(err)
	}
	if _, err := parser.ParseArgs([]string{"--config-file", configFile}); err != nil {
		code := 1
		if fe, ok := err.(*flags.Error); ok {
			if fe.Type == flags.ErrHelp {
				code = 0
			}
		}
		os.Exit(code)
	}

	prg.server.ConfigureAPI()
	// this must be done after the api has been configured
	prg.server.TLSHost = restapi.APIConfig.TLSHost
	prg.server.TLSPort = restapi.APIConfig.TLSPort

	if err := prg.server.Serve(); err != nil {
		log.Fatalln(err)
	}
}

func (prg *program) Stop(s service.Service) error {
	go prg.shutdown()
	time.Sleep(1 * time.Second)
	return nil
}

func (prg *program) shutdown() {
	prg.server.Shutdown()
}

func main() {
	svcConfig := &service.Config{
		Name:        "qdb_rest_service",
		DisplayName: "Quasardb rest service",
		Description: "This is quasardb rest service.",
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}
	logger, err = s.Logger(nil)
	if err != nil {
		log.Fatal(err)
	}
	err = s.Run()
	if err != nil {
		logger.Error(err)
	}
}
