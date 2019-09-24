// This file is safe to edit. Once it exists it will not be overwritten

package restapi

import (
	"bytes"
	"crypto/rsa"
	"crypto/tls"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"reflect"
	"strings"
	"time"

	errors "github.com/go-openapi/errors"
	runtime "github.com/go-openapi/runtime"
	middleware "github.com/go-openapi/runtime/middleware"
	cmap "github.com/orcaman/concurrent-map"
	cors "github.com/rs/cors"

	"github.com/gogo/protobuf/proto"
	"github.com/golang/snappy"

	"github.com/prometheus/prometheus/prompb"

	qdb "github.com/bureau14/qdb-api-go"
	"github.com/bureau14/qdb-api-rest/config"
	"github.com/bureau14/qdb-api-rest/jwt"
	"github.com/bureau14/qdb-api-rest/models"
	"github.com/bureau14/qdb-api-rest/prometheus"
	"github.com/bureau14/qdb-api-rest/qdbinterface"
	"github.com/bureau14/qdb-api-rest/restapi/operations"
	"github.com/bureau14/qdb-api-rest/restapi/operations/cluster"
	"github.com/bureau14/qdb-api-rest/restapi/operations/query"
)

//go:generate swagger generate server --target .. --name qdb-api-rest --spec ../swagger.json

// APIConfig : api config
// TODO(vianney): find another way to manage the lifetime of the config
var APIConfig = config.FilledDefaultConfig

func configureFlags(api *operations.QdbAPIRestAPI) {
}

var secret *rsa.PrivateKey

const version string = "3.5.0master"

func dummyConsumer() runtime.Consumer {
	return runtime.ConsumerFunc(func(reader io.Reader, data interface{}) error {
		return nil
	})
}

func dummyProducer() runtime.Producer {
	return runtime.ProducerFunc(func(writer io.Writer, data interface{}) error {
		return nil
	})
}

func configureAPI(api *operations.QdbAPIRestAPI) http.Handler {
	handleCache := cmap.New()

	GetHandle := func(principal *models.Principal) (*qdb.HandleType, error) {
		var handle *qdb.HandleType

		// This is always a username:secret_key pair, validated in BearerAuth
		credentials := strings.Split(string(*principal), ":")

		if len(credentials) < 2 {
			api.Logger("Error: invalid principal key. This should never happen because it's checked in BearerAuth")
			return nil, errors.New(500, "Invalid principal")
		}
		username := credentials[0]
		secretKey := credentials[1]

		if tmp, handleFound := handleCache.Get(username); handleFound {
			if handle, ok := tmp.(*qdb.HandleType); ok {
				api.Logger("Got handle from cache")
				return handle, nil
			}
			api.Logger("Warning: expected handle type from cache to be *qdb.HandleType but got %s", reflect.TypeOf(tmp))
		}

		handle, err := qdbinterface.CreateHandle(username, secretKey, APIConfig.ClusterURI, string(APIConfig.ClusterPublicKeyFile))
		if err != nil {
			return nil, err
		}

		api.Logger("Handle cache miss, got handle from credentials")
		handleCache.Set(username, handle)

		return handle, nil
	}

	api.Logger = log.Printf

	APIConfig.SetDefaults()

	if APIConfig.Log != "" {
		f, err := os.OpenFile(string(APIConfig.Log), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0644)
		if err != nil {
			api.Logger("Warning: cannot create log file at location %s , logging to console.\n", APIConfig.Log)
			APIConfig.Log = ""
		} else {
			log.SetOutput(f)
		}
	}

	err := APIConfig.Check()
	if err != nil {
		panic(err)
	}

	if APIConfig.IsSecurityEnabled() {
		secret = qdbinterface.MustUnmarshalRSAKeyFromFile(string(APIConfig.TLSCertificateKey))
	} else {
		secret = qdbinterface.DefaultPrivateKey
	}

	api.Logger("version: %s", version)

	clusterURI := APIConfig.ClusterURI

	// configure the api here
	api.ServeError = errors.ServeError

	api.JSONConsumer = runtime.JSONConsumer()
	api.JSONProducer = runtime.JSONProducer()

	// Keep go-swagger happy for now
	api.ProtobufConsumer = dummyConsumer()
	api.ProtobufProducer = dummyProducer()

	api.LoginHandler = operations.LoginHandlerFunc(func(params operations.LoginParams) middleware.Responder {
		_, err := qdbinterface.CreateHandle(params.Credential.Username, params.Credential.SecretKey, clusterURI, string(APIConfig.ClusterPublicKeyFile))
		if err != nil {
			api.Logger("Failed to login user %s: %s", params.Credential.Username, err.Error())
			return operations.NewLoginBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
		}

		token, err := jwt.Build(secret, params.Credential.Username, params.Credential.SecretKey)
		if err != nil {
			api.Logger("Warning: %s", err.Error())
			return operations.NewLoginBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
		}

		if params.Credential.Username != "" {
			api.Logger("Logged in user %s", params.Credential.Username)
		} else {
			api.Logger("Logged anonymous user")
		}

		return operations.NewLoginOK().WithPayload(&models.Token{Token: token})
	})

	api.BearerAuth = func(token string) (*models.Principal, error) {
		if strings.HasPrefix(token, "Bearer ") {
			token = strings.Replace(token, "Bearer ", "", 1)
		}

		credentials, err := jwt.Parse(secret, token)
		if err != nil {
			api.Logger("Access attempt with invalid auth token: %s", token)
			return nil, errors.New(401, "Invalid authentication token")
		}

		cacheKey := credentials.Username
		principle := models.Principal(credentials.Username + ":" + credentials.SecretKey)

		now := time.Now()

		if credentials.NotBefore.After(now) {
			api.Logger("token used before it was valid")
			return nil, errors.New(401, "Token used before it is active. Please try again later")
		}

		if now.After(credentials.Expiry) {
			api.Logger("Token has expired")
			handleCache.Remove(cacheKey)
			return nil, errors.New(401, "Token has expired. Please login again")
		}

		if _, handleFound := handleCache.Get(cacheKey); !handleFound {
			handle, err := qdbinterface.CreateHandle(credentials.Username, credentials.SecretKey, APIConfig.ClusterURI, string(APIConfig.ClusterPublicKeyFile))
			if err != nil {
				api.Logger("Invalid username and secret key pair for user %s with token %s", credentials.Username, token)
				return nil, errors.New(401, "Incorrect api key auth")
			}
			handleCache.Set(cacheKey, handle)
		}

		return &principle, nil
	}

	api.QueryPostQueryHandler = query.PostQueryHandlerFunc(func(params query.PostQueryParams, principal *models.Principal) middleware.Responder {
		handle, err := GetHandle(principal)
		if err != nil {
			return query.NewPostQueryInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		result, err := qdbinterface.QueryData(*handle, params.Query.Query)
		if err != nil {
			if err != qdb.ErrConnectionRefused && err != qdb.ErrUnstableCluster {
				api.Logger("Failed to query: %s", err.Error())
				return query.NewPostQueryBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
			}

			if err == qdb.ErrAccessDenied || err == qdb.ErrConnectionRefused {
				credentials := strings.Split(string(*principal), ":")
				handleCache.Remove(credentials[0])
			}

			api.Logger("Failed to query: %s", err.Error())
			return query.NewPostQueryInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}
		return query.NewPostQueryOK().WithPayload(result)
	})

	api.ClusterGetClusterHandler = cluster.GetClusterHandlerFunc(func(params cluster.GetClusterParams, principal *models.Principal) middleware.Responder {
		handle, err := GetHandle(principal)
		if err != nil {
			return query.NewPostQueryInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		err = qdbinterface.RetrieveInformation(*handle)
		if err != nil && err != qdb.ErrUnstableCluster && err != qdb.ErrConnectionRefused {
			api.Logger("Failed to access cluster status: %s", err.Error())
			return cluster.NewGetClusterBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
		}
		return cluster.NewGetClusterOK().WithPayload(&qdbinterface.ClusterInformation)
	})

	api.ClusterGetNodeHandler = cluster.GetNodeHandlerFunc(func(params cluster.GetNodeParams, principal *models.Principal) middleware.Responder {
		handle, err := GetHandle(principal)
		if err != nil {
			return query.NewPostQueryInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		err = qdbinterface.RetrieveInformation(*handle)
		if err != nil {
			if err == qdb.ErrAccessDenied || err == qdb.ErrConnectionRefused {
				credentials := strings.Split(string(*principal), ":")
				handleCache.Remove(credentials[0])
			}

			api.Logger("Failed to access %s node status: %s", params.ID, err.Error())
			return cluster.NewGetNodeBadRequest().WithPayload(&models.QdbError{Message: err.Error()})
		}
		if val, ok := qdbinterface.NodesInformation[params.ID]; ok {
			return cluster.NewGetNodeOK().WithPayload(&val)
		}
		api.Logger("Failed to access %s node status: %s", params.ID, err.Error())
		return cluster.NewGetNodeNotFound()
	})

	// Prometheus Integration
	client := prometheus.Client{ClusterURI: clusterURI}

	api.PrometheusWriteHandler = operations.PrometheusWriteHandlerFunc(func(params operations.PrometheusWriteParams) middleware.Responder {
		compressed, err := ioutil.ReadAll(params.Timeseries)
		if err != nil {
			api.Logger("Failed to read payload: %s", err.Error())
			return operations.NewPrometheusWriteInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		reqBuf, err := snappy.Decode(nil, compressed)
		if err != nil {
			api.Logger("Failed to snappy decode payload: %s", err.Error())
			return operations.NewPrometheusWriteInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		var req prompb.WriteRequest
		if err := proto.Unmarshal(reqBuf, &req); err != nil {
			api.Logger("Failed to decompress snappy payload: %s", err.Error())
			return operations.NewPrometheusWriteInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		err = client.Write(req.Timeseries)

		if err != nil {
			return operations.NewPrometheusWriteInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		return operations.NewPrometheusWriteOK()
	})

	api.PrometheusReadHandler = operations.PrometheusReadHandlerFunc(func(params operations.PrometheusReadParams) middleware.Responder {
		compressed, err := ioutil.ReadAll(params.HTTPRequest.Body)
		if err != nil {
			api.Logger("Failed to read payload: %s", err.Error())
			return operations.NewPrometheusReadInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		reqBuf, err := snappy.Decode(nil, compressed)
		if err != nil {
			api.Logger("Failed to snappy decode payload: %s", err.Error())
			return operations.NewPrometheusReadInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		var req prompb.ReadRequest
		if err := proto.Unmarshal(reqBuf, &req); err != nil {
			api.Logger("Failed to decompress snappy payload: %s", err.Error())
			return operations.NewPrometheusReadInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		var resp *prompb.ReadResponse
		resp, err = client.Read(&req)
		if err != nil {
			api.Logger("Failed to read samples: %s", err.Error())
			return operations.NewPrometheusReadInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		data, err := proto.Marshal(resp)
		if err != nil {
			api.Logger("Failed to marshal protocol buffer response: %s", err.Error())
			return operations.NewPrometheusReadInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		compressed = snappy.Encode(nil, data)
		if err != nil {
			api.Logger("Failed to snappy compress data: %s", err.Error())
			return operations.NewPrometheusReadInternalServerError().WithPayload(&models.QdbError{Message: err.Error()})
		}

		readCloser := ioutil.NopCloser(bytes.NewReader(compressed))

		return operations.NewPrometheusReadOK().WithPayload(readCloser)
	})

	api.ServerShutdown = func() {}

	return setupGlobalMiddleware(api.Serve(setupMiddlewares), APIConfig.AllowedOrigins, APIConfig.Assets)
}

// The TLS configuration before HTTPS server starts.
func configureTLS(tlsConfig *tls.Config) {
	if APIConfig.TLSCertificate == "" || APIConfig.TLSCertificateKey == "" {
		return
	}
	tlsConfig.Certificates = make([]tls.Certificate, 1)
	var err error
	tlsConfig.Certificates[0], err = tls.LoadX509KeyPair(string(APIConfig.TLSCertificate), string(APIConfig.TLSCertificateKey))
	if err != nil {
		panic(err)
	}
	tlsConfig.ServerName = APIConfig.Host
	tlsConfig.MinVersion = tls.VersionTLS12
}

var httpRedirectHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	redirection := fmt.Sprintf("https://%s:%d%s", APIConfig.Host, APIConfig.TLSPort, r.RequestURI)
	log.Printf("Redirecting to %s", redirection)
	http.Redirect(w, r, redirection, http.StatusPermanentRedirect)
})

var hd http.Handler

// As soon as server is initialized but not run yet, this function will be called.
// If you need to modify a config, store server instance to stop it individually later, this is the place.
// This function can be called multiple times, depending on the number of serving schemes.
// scheme value will be set accordingly: "http", "https" or "unix"
func configureServer(s *http.Server, scheme, addr string) {
	if APIConfig.TLSCertificate != "" && APIConfig.TLSCertificateKey != "" && scheme == "http" {
		s.Handler = httpRedirectHandler
	}
}

// The middleware configuration is for the handler executors. These do not apply to the swagger.json document.
// The middleware executes after routing but before authentication, binding and validation
func setupMiddlewares(handler http.Handler) http.Handler {
	return handler
}

// HTTPSwitchMiddleWare : middleware switch between normal and fileserver handler
func HTTPSwitchMiddleWare(next http.Handler, assets string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("Serving %s request: %s", r.Method, r.URL.Path[1:])
		if APIConfig.Assets != "" && !strings.HasPrefix(r.URL.Path, "/api") && !strings.HasSuffix(r.URL.Path, "/swagger.json") {
			http.FileServer(http.Dir(assets)).ServeHTTP(w, r)
		} else {
			next.ServeHTTP(w, r)
		}
	})
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

	return corsHandler(HTTPSwitchMiddleWare(handler, assets))

}
