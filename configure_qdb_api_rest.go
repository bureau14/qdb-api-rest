// This file is safe to edit. Once it exists it will not be overwritten

package restapi

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	jwt "github.com/dgrijalva/jwt-go"
	errors "github.com/go-openapi/errors"
	runtime "github.com/go-openapi/runtime"
	middleware "github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/swag"
	cors "github.com/rs/cors"
	xid "github.com/rs/xid"

	qdb "github.com/bureau14/qdb-api-go"
	"github.com/bureau14/qdb-api-rest/models"
	"github.com/bureau14/qdb-api-rest/qdbinterface"
	"github.com/bureau14/qdb-api-rest/restapi/operations"
	"github.com/bureau14/qdb-api-rest/restapi/operations/cluster"
	"github.com/bureau14/qdb-api-rest/restapi/operations/query"
)

//go:generate swagger generate server --target .. --name qdb-api-rest --spec ../swagger.json

// Config : A configuration file for the rest api
type Config struct {
	AllowedOrigins       []string `json:"allowed_origins" required:"true"`
	ClusterURI           string   `json:"cluster_uri" required:"true"`
	ClusterPublicKeyFile string   `json:"cluster_public_key_file" required:"true"`
	TLSCertificate       string   `json:"tls_certificate" required:"true"`
	TLSKey               string   `json:"tls_key" required:"true"`
	TLSHost              string   `json:"tls_host" required:"true"`
	TLSPort              int      `json:"tls_port" required:"true"`
	Assets               string   `json:"assets"`
}

// APIConfig : api config
// TODO(vianney): find another way to manage the lifetime of the config
var APIConfig Config

// ApplicationFlags : Additionl flags to setup the rest-api
type ApplicationFlags struct {
	ConfigFile string `long:"config-file" required:"true" description:"Config file to setup the rest-api"`
}

var applicationFlags ApplicationFlags

func configureFlags(api *operations.QdbAPIRestAPI) {
	api.CommandLineOptionsGroups = []swag.CommandLineOptionsGroup{
		{ShortDescription: "Application Flags", LongDescription: "Application Configuration Flags", Options: &applicationFlags},
	}
}

// FileServerMiddleWare : middleware for fileserver handler
func FileServerMiddleWare(next http.Handler, assets string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api") {
			next.ServeHTTP(w, r)
		} else {
			http.FileServer(http.Dir(assets)).ServeHTTP(w, r)
		}
	})
}

func configureAPI(api *operations.QdbAPIRestAPI) http.Handler {

	tokenToHandle := map[string]*qdb.HandleType{}

	content, err := ioutil.ReadFile(applicationFlags.ConfigFile)
	if err != nil {
		panic(err)
	}
	err = json.Unmarshal(content, &APIConfig)
	if err != nil {
		panic(err)
	}

	clusterURI := APIConfig.ClusterURI

	tokenParser := func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("Unexpected signing method: %v", t.Header["alg"])
		}

		secret, err := qdbinterface.CredentialsFromTLS(APIConfig.TLSCertificate, APIConfig.TLSKey)
		if err != nil {
			return nil, err
		}

		return secret, nil
	}

	// Will delete unvalid keys every hour to avoid growing the map too much
	clearHandles := func() {
		for range time.Tick(time.Hour * 1) {
			for token := range tokenToHandle {
				parsedToken, _ := jwt.Parse(token, tokenParser)
				if _, ok := parsedToken.Claims.(jwt.MapClaims); !ok || (ok && !parsedToken.Valid) {
					delete(tokenToHandle, token)
				}
			}
		}
	}
	go clearHandles()

	// configure the api here
	api.ServeError = errors.ServeError

	// Set your custom logger if needed. Default one is log.Printf
	// Expected interface func(string, ...interface{})
	//
	// Example:
	// api.Logger = log.Printf

	api.JSONConsumer = runtime.JSONConsumer()

	api.JSONProducer = runtime.JSONProducer()

	api.BearerAuth = func(token string) (*models.Principal, error) {
		if strings.HasPrefix(token, "Bearer ") {
			token = strings.Replace(token, "Bearer ", "", 1)
			parsedToken, err := jwt.Parse(token, tokenParser)
			if _, ok := parsedToken.Claims.(jwt.MapClaims); !ok || (ok && !parsedToken.Valid) {
				return nil, err
			}
			t := models.Principal(token)
			return &t, nil
		}
		// api.Logger("Access attempt with incorrect api key auth: %s", token)
		return nil, errors.New(401, "incorrect api key auth")
	}

	api.QueryPostQueryHandler = query.PostQueryHandlerFunc(func(params query.PostQueryParams, principal *models.Principal) middleware.Responder {
		signedString := string(*principal)
		handle := tokenToHandle[signedString]
		result, err := qdbinterface.QueryData(*handle, params.Query)
		if err != nil {
			if err != qdb.ErrConnectionRefused && err != qdb.ErrUnstableCluster {
				return query.NewPostQueryBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
			}
			return query.NewPostQueryInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}
		return query.NewPostQueryOK().WithPayload(result)
	})

	api.ClusterGetClusterHandler = cluster.GetClusterHandlerFunc(func(params cluster.GetClusterParams, principal *models.Principal) middleware.Responder {
		signedString := string(*principal)
		handle := tokenToHandle[signedString]

		err := qdbinterface.RetrieveInformation(*handle)
		if err != nil && err != qdb.ErrUnstableCluster && err != qdb.ErrConnectionRefused {
			return cluster.NewGetClusterBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
		}
		return cluster.NewGetClusterOK().WithPayload(&qdbinterface.ClusterInformation)
	})

	api.ClusterGetNodeHandler = cluster.GetNodeHandlerFunc(func(params cluster.GetNodeParams, principal *models.Principal) middleware.Responder {
		signedString := string(*principal)
		handle := tokenToHandle[signedString]

		err := qdbinterface.RetrieveInformation(*handle)
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
		handle, err := qdbinterface.CreateHandle(params.Credential.Username, params.Credential.SecretKey, clusterURI, APIConfig.ClusterPublicKeyFile)
		if err != nil {
			return operations.NewLoginBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
		}

		expiresAt := time.Now().Add(time.Duration(12) * time.Hour).Unix()
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, &jwt.StandardClaims{
			Id:        xid.New().String(),
			ExpiresAt: int64(expiresAt),
		})

		secret, err := qdbinterface.CredentialsFromTLS(APIConfig.TLSCertificate, APIConfig.TLSKey)
		if err != nil {
			err = fmt.Errorf("Could not retrieve tls key from file:%s", APIConfig.TLSKey)
			return operations.NewLoginBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
		}

		signedString, err := token.SignedString(secret)
		if err != nil {
			return operations.NewLoginBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
		}
		tokenToHandle[signedString] = handle
		t := models.Token(signedString)
		return operations.NewLoginOK().WithPayload(t)
	})

	api.ServerShutdown = func() {}

	return setupGlobalMiddleware(api.Serve(setupMiddlewares), APIConfig.AllowedOrigins, APIConfig.Assets)
}

// The TLS configuration before HTTPS server starts.
func configureTLS(tlsConfig *tls.Config) {
	tlsConfig.Certificates = make([]tls.Certificate, 1)
	var err error
	tlsConfig.Certificates[0], err = tls.LoadX509KeyPair(APIConfig.TLSCertificate, APIConfig.TLSKey)
	if err != nil {
		panic(err)
	}
	tlsConfig.ServerName = APIConfig.TLSHost
	tlsConfig.MinVersion = tls.VersionTLS12
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
func setupGlobalMiddleware(handler http.Handler, allowedOrigins []string, assets string) http.Handler {
	corsHandler := cors.New(cors.Options{
		AllowedOrigins:   allowedOrigins,
		AllowedHeaders:   []string{"*"},
		AllowedMethods:   []string{"GET", "POST"},
		AllowCredentials: true,
	}).Handler

	if assets == "" {
		return corsHandler(handler)
	}
	return FileServerMiddleWare(corsHandler(handler), assets)
}
