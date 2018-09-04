package main

import (
	"log"
	"os"
	"time"

	"github.com/bureau14/qdb-api-rest/restapi"
	"github.com/bureau14/qdb-api-rest/restapi/operations"
	"github.com/kardianos/service"

	loads "github.com/go-openapi/loads"
	flags "github.com/jessevdk/go-flags"
)

var logger service.Logger

type program struct {
	server *restapi.Server
}

func (p *program) Start(s service.Service) error {
	// Start should not block. Do the actual work async.
	go p.run()
	return nil
}

func (p *program) run() {
	swaggerSpec, err := loads.Embedded(restapi.SwaggerJSON, restapi.FlatSwaggerJSON)
	if err != nil {
		log.Fatalln(err)
	}

	api := operations.NewQdbAPIRestAPI(swaggerSpec)
	p.server = restapi.NewServer(api)

	parser := flags.NewParser(p.server, flags.Default)
	parser.ShortDescription = "QuasarDB API"
	parser.LongDescription = "Find out more at https://doc.quasardb.net"

	p.server.ConfigureFlags()
	for _, optsGroup := range api.CommandLineOptionsGroups {
		_, err := parser.AddGroup(optsGroup.ShortDescription, optsGroup.LongDescription, optsGroup.Options)
		if err != nil {
			log.Fatalln(err)
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

	p.server.ConfigureAPI()
	// this must be done after the api has been configured
	p.server.TLSHost = restapi.APIConfig.TLSHost
	p.server.TLSPort = restapi.APIConfig.TLSPort

	if err := p.server.Serve(); err != nil {
		log.Fatalln(err)
	}
}

func (p *program) Stop(s service.Service) error {
	go p.shutdown()
	time.Sleep(1 * time.Second)
	return nil
}

func (p *program) shutdown() {
	p.server.Shutdown()
}

func main() {
	svcConfig := &service.Config{
		Name:        "QdbRestService",
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
