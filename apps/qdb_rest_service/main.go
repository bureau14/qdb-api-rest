// +build windows

package main

import (
	"log"
	"os"

	"github.com/bureau14/qdb-api-rest/restapi"
	"github.com/bureau14/qdb-api-rest/restapi/operations"
	"github.com/go-openapi/loads"
	flags "github.com/jessevdk/go-flags"
	"github.com/kardianos/service"

	"golang.org/x/sys/windows/registry"
)

var logger service.Logger

type program struct {
	server *restapi.Server
}

const serviceName string = "qdb_rest"

func (p *program) Start(s service.Service) error {
	go p.run()
	return nil
}

func (prg *program) run() {
	swaggerSpec, err := loads.Embedded(restapi.SwaggerJSON, restapi.FlatSwaggerJSON)
	if err != nil {
		log.Fatalf("Failed to load swagger config: %s\n", err)
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
			log.Fatalf("Failed to add option group: %s\n", err)
		}
	}

	registryPath := `SYSTEM\CurrentControlSet\Services\qdb_rest`
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, registryPath, registry.QUERY_VALUE)
	if err != nil {
		log.Fatalf("Failed to retrieve registry key: %s\n", err)
	}
	configFile, _, err := k.GetStringValue("ConfigFile")
	if err != nil {
		log.Fatalf("Failed to retrieve value from registry key (%s): %s\n", registryPath, err)
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
	// because restapi.APIConfig is configured there
	prg.server.Host = restapi.APIConfig.Host
	prg.server.Port = restapi.APIConfig.Port
	prg.server.EnabledListeners = []string{"http"}
	if restapi.APIConfig.TLSCertificate != "" && restapi.APIConfig.TLSKey != "" {
		prg.server.TLSHost = restapi.APIConfig.Host
		prg.server.TLSPort = restapi.APIConfig.TLSPort
		prg.server.EnabledListeners = append(prg.server.EnabledListeners, "https")
	}

	if err := prg.server.Serve(); err != nil {
		log.Fatalln(err)
	}
}

func (prg *program) Stop(s service.Service) error {
	go prg.shutdown()
	return nil
}

func (prg *program) shutdown() {
	prg.server.Shutdown()
}

func main() {
	svcConfig := &service.Config{
		Name:        serviceName,
		DisplayName: "quasardb rest service",
		Description: "This is quasardb rest service.",
	}

	s, err := service.New(&program{}, svcConfig)
	if err != nil {
		log.Fatal(err)
	}

	logger, err = s.Logger(nil)
	if err != nil {
		log.Fatal(err)
	}
	if len(os.Args) > 1 {
		var err error
		verb := os.Args[1]
		if verb[0] == '/' {
			verb = os.Args[1][1:]
		}
		switch verb {
		case "install":
			err = s.Install()
			if err != nil {
				log.Fatalf("Failed to install: %s\n", err)
			}
			log.Printf("Service \"%s\" installed.\n", serviceName)
		case "uninstall":
			err = s.Uninstall()
			if err != nil {
				log.Fatalf("Failed to remove: %s\n", err)
			}
			log.Printf("Service \"%s\" removed.\n", serviceName)
		case "remove":
			err = s.Uninstall()
			if err != nil {
				log.Fatalf("Failed to remove: %s\n", err)
			}
			log.Printf("Service \"%s\" removed.\n", serviceName)
		case "start":
			log.Printf("Service \"%s\" starting...\n", serviceName)
			err = s.Start()
			if err != nil {
				log.Fatalf("Failed to start: %s\n", err)
			}
			log.Printf("Service \"%s\" started.\n", serviceName)
		case "status":
			status, err := s.Status()
			if err != nil {
				log.Fatalf("Failed to report status: %s\n", err)
			}
			log.Printf("Service \"%s\" status report: %s\n", serviceName, status)
		case "stop":
			err = s.Stop()
			if err != nil {
				log.Fatalf("Failed to stop: %s\n", err)
			}
			log.Printf("Service \"%s\" stopped.\n", serviceName)
		}
		return
	}

	err = s.Run()
	if err != nil {
		log.Fatalf("Failed to run: %s\n", err)
	}
}
