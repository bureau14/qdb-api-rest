// This file is safe to edit. Once it exists it will not be overwritten

package restapi

import (
	"crypto/tls"
	"net/http"

	errors "github.com/go-openapi/errors"
	runtime "github.com/go-openapi/runtime"
	middleware "github.com/go-openapi/runtime/middleware"
	cors "github.com/rs/cors"

	qdb "github.com/bureau14/qdb-api-go"
	"github.com/bureau14/qdb-api-rest/models"
	"github.com/bureau14/qdb-api-rest/qdbinterface"
	"github.com/bureau14/qdb-api-rest/restapi/operations"
	"github.com/bureau14/qdb-api-rest/restapi/operations/cluster"
	"github.com/bureau14/qdb-api-rest/restapi/operations/query"
)

//go:generate swagger generate server --target .. --name qdb-api-rest --spec ../swagger.json

func configureFlags(api *operations.QdbAPIRestAPI) {
	// api.CommandLineOptionsGroups = []swag.CommandLineOptionsGroup{ ... }
}

func configureAPI(api *operations.QdbAPIRestAPI) http.Handler {
	// configure the api here
	api.ServeError = errors.ServeError

	// Set your custom logger if needed. Default one is log.Printf
	// Expected interface func(string, ...interface{})
	//
	// Example:
	// api.Logger = log.Printf

	api.JSONConsumer = runtime.JSONConsumer()

	api.JSONProducer = runtime.JSONProducer()

	api.QueryPostQueryHandler = query.PostQueryHandlerFunc(func(params query.PostQueryParams) middleware.Responder {
		result, err := qdbinterface.QueryData(params.Query)
		if err != nil {
			if err != qdb.ErrConnectionRefused && err != qdb.ErrUnstableCluster {
				return query.NewPostQueryBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
			}
			return query.NewPostQueryInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}
		return query.NewPostQueryOK().WithPayload(result)
	})

	api.ClusterGetClusterHandler = cluster.GetClusterHandlerFunc(func(params cluster.GetClusterParams) middleware.Responder {
		err := qdbinterface.RetrieveInformation()
		if err != nil && err != qdb.ErrUnstableCluster && err != qdb.ErrConnectionRefused {
			return cluster.NewGetClusterBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
		}
		return cluster.NewGetClusterOK().WithPayload(&qdbinterface.ClusterInformation)
	})

	api.ClusterGetNodeHandler = cluster.GetNodeHandlerFunc(func(params cluster.GetNodeParams) middleware.Responder {
		err := qdbinterface.RetrieveInformation()
		if err != nil {
			if err != qdb.ErrConnectionRefused && err != qdb.ErrUnstableCluster {
				return cluster.NewGetNodeBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
			}
			return cluster.NewGetNodeBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
		}
		if val, ok := qdbinterface.NodesInformation[params.ID]; ok {
			return cluster.NewGetNodeOK().WithPayload(&val)
		}
		return cluster.NewGetNodeNotFound()
	})

	api.LoginHandler = operations.LoginHandlerFunc(func(params operations.LoginParams) middleware.Responder {
		return middleware.NotImplemented("operation .Login has not yet been implemented")
	})

	api.ServerShutdown = func() {}

	return setupGlobalMiddleware(api.Serve(setupMiddlewares))
}

// The TLS configuration before HTTPS server starts.
func configureTLS(tlsConfig *tls.Config) {
	// Make all necessary changes to the TLS configuration here.
}

// As soon as server is initialized but not run yet, this function will be called.
// If you need to modify a config, store server instance to stop it individually later, this is the place.
// This function can be called multiple times, depending on the number of serving schemes.
// scheme value will be set accordingly: "http", "https" or "unix"
func configureServer(s *http.Server, scheme, addr string) {
}

// The middleware configuration is for the handler executors. These do not apply to the swagger.json document.
// The middleware executes after routing but before authentication, binding and validation
func setupMiddlewares(handler http.Handler) http.Handler {
	return handler
}

// The middleware configuration happens before anything, this middleware also applies to serving the swagger.json document.
// So this is a good place to plug in a panic handling middleware, logging and metrics
func setupGlobalMiddleware(handler http.Handler) http.Handler {
	corsHandler := cors.Default()
	return corsHandler.Handler(handler)
}
