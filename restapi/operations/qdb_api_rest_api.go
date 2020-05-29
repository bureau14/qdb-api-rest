// Code generated by go-swagger; DO NOT EDIT.

package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/loads"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/runtime/security"
	"github.com/go-openapi/spec"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"

	"github.com/bureau14/qdb-api-rest/models"
	"github.com/bureau14/qdb-api-rest/restapi/operations/cluster"
	"github.com/bureau14/qdb-api-rest/restapi/operations/query"
)

// NewQdbAPIRestAPI creates a new QdbAPIRest instance
func NewQdbAPIRestAPI(spec *loads.Document) *QdbAPIRestAPI {
	return &QdbAPIRestAPI{
		handlers:            make(map[string]map[string]http.Handler),
		formats:             strfmt.Default,
		defaultConsumes:     "application/json",
		defaultProduces:     "application/json",
		customConsumers:     make(map[string]runtime.Consumer),
		customProducers:     make(map[string]runtime.Producer),
		PreServerShutdown:   func() {},
		ServerShutdown:      func() {},
		spec:                spec,
		ServeError:          errors.ServeError,
		BasicAuthenticator:  security.BasicAuth,
		APIKeyAuthenticator: security.APIKeyAuth,
		BearerAuthenticator: security.BearerAuth,

		JSONConsumer: runtime.JSONConsumer(),
		ProtobufConsumer: runtime.ConsumerFunc(func(r io.Reader, target interface{}) error {
			return errors.NotImplemented("protobuf consumer has not yet been implemented")
		}),

		CsvProducer: runtime.ProducerFunc(func(w io.Writer, data interface{}) error {
			return errors.NotImplemented("csv producer has not yet been implemented")
		}),
		JSONProducer: runtime.JSONProducer(),
		ProtobufProducer: runtime.ProducerFunc(func(w io.Writer, data interface{}) error {
			return errors.NotImplemented("protobuf producer has not yet been implemented")
		}),

		ClusterGetClusterHandler: cluster.GetClusterHandlerFunc(func(params cluster.GetClusterParams, principal *models.Principal) middleware.Responder {
			return middleware.NotImplemented("operation cluster.GetCluster has not yet been implemented")
		}),
		ClusterGetNodeHandler: cluster.GetNodeHandlerFunc(func(params cluster.GetNodeParams, principal *models.Principal) middleware.Responder {
			return middleware.NotImplemented("operation cluster.GetNode has not yet been implemented")
		}),
		GetTableCsvHandler: GetTableCsvHandlerFunc(func(params GetTableCsvParams, principal *models.Principal) middleware.Responder {
			return middleware.NotImplemented("operation GetTableCsv has not yet been implemented")
		}),
		LoginHandler: LoginHandlerFunc(func(params LoginParams) middleware.Responder {
			return middleware.NotImplemented("operation Login has not yet been implemented")
		}),
		QueryPostQueryHandler: query.PostQueryHandlerFunc(func(params query.PostQueryParams, principal *models.Principal) middleware.Responder {
			return middleware.NotImplemented("operation query.PostQuery has not yet been implemented")
		}),
		PrometheusReadHandler: PrometheusReadHandlerFunc(func(params PrometheusReadParams) middleware.Responder {
			return middleware.NotImplemented("operation PrometheusRead has not yet been implemented")
		}),
		PrometheusWriteHandler: PrometheusWriteHandlerFunc(func(params PrometheusWriteParams) middleware.Responder {
			return middleware.NotImplemented("operation PrometheusWrite has not yet been implemented")
		}),

		// Applies when the "Authorization" header is set
		BearerAuth: func(token string) (*models.Principal, error) {
			return nil, errors.NotImplemented("api key auth (Bearer) Authorization from header param [Authorization] has not yet been implemented")
		},
		// Applies when the "token" query is set
		URLParamAuth: func(token string) (*models.Principal, error) {
			return nil, errors.NotImplemented("api key auth (UrlParam) token from query param [token] has not yet been implemented")
		},
		// default authorizer is authorized meaning no requests are blocked
		APIAuthorizer: security.Authorized(),
	}
}

/*QdbAPIRestAPI Find out more at https://doc.quasardb.net */
type QdbAPIRestAPI struct {
	spec            *loads.Document
	context         *middleware.Context
	handlers        map[string]map[string]http.Handler
	formats         strfmt.Registry
	customConsumers map[string]runtime.Consumer
	customProducers map[string]runtime.Producer
	defaultConsumes string
	defaultProduces string
	Middleware      func(middleware.Builder) http.Handler

	// BasicAuthenticator generates a runtime.Authenticator from the supplied basic auth function.
	// It has a default implementation in the security package, however you can replace it for your particular usage.
	BasicAuthenticator func(security.UserPassAuthentication) runtime.Authenticator
	// APIKeyAuthenticator generates a runtime.Authenticator from the supplied token auth function.
	// It has a default implementation in the security package, however you can replace it for your particular usage.
	APIKeyAuthenticator func(string, string, security.TokenAuthentication) runtime.Authenticator
	// BearerAuthenticator generates a runtime.Authenticator from the supplied bearer token auth function.
	// It has a default implementation in the security package, however you can replace it for your particular usage.
	BearerAuthenticator func(string, security.ScopedTokenAuthentication) runtime.Authenticator

	// JSONConsumer registers a consumer for the following mime types:
	//   - application/json
	JSONConsumer runtime.Consumer
	// ProtobufConsumer registers a consumer for the following mime types:
	//   - application/x-protobuf
	ProtobufConsumer runtime.Consumer

	// CsvProducer registers a producer for the following mime types:
	//   - text/csv
	CsvProducer runtime.Producer
	// JSONProducer registers a producer for the following mime types:
	//   - application/json
	JSONProducer runtime.Producer
	// ProtobufProducer registers a producer for the following mime types:
	//   - application/x-protobuf
	ProtobufProducer runtime.Producer

	// BearerAuth registers a function that takes a token and returns a principal
	// it performs authentication based on an api key Authorization provided in the header
	BearerAuth func(string) (*models.Principal, error)

	// URLParamAuth registers a function that takes a token and returns a principal
	// it performs authentication based on an api key token provided in the query
	URLParamAuth func(string) (*models.Principal, error)

	// APIAuthorizer provides access control (ACL/RBAC/ABAC) by providing access to the request and authenticated principal
	APIAuthorizer runtime.Authorizer

	// ClusterGetClusterHandler sets the operation handler for the get cluster operation
	ClusterGetClusterHandler cluster.GetClusterHandler
	// ClusterGetNodeHandler sets the operation handler for the get node operation
	ClusterGetNodeHandler cluster.GetNodeHandler
	// GetTableCsvHandler sets the operation handler for the get table csv operation
	GetTableCsvHandler GetTableCsvHandler
	// LoginHandler sets the operation handler for the login operation
	LoginHandler LoginHandler
	// QueryPostQueryHandler sets the operation handler for the post query operation
	QueryPostQueryHandler query.PostQueryHandler
	// PrometheusReadHandler sets the operation handler for the prometheus read operation
	PrometheusReadHandler PrometheusReadHandler
	// PrometheusWriteHandler sets the operation handler for the prometheus write operation
	PrometheusWriteHandler PrometheusWriteHandler
	// ServeError is called when an error is received, there is a default handler
	// but you can set your own with this
	ServeError func(http.ResponseWriter, *http.Request, error)

	// PreServerShutdown is called before the HTTP(S) server is shutdown
	// This allows for custom functions to get executed before the HTTP(S) server stops accepting traffic
	PreServerShutdown func()

	// ServerShutdown is called when the HTTP(S) server is shut down and done
	// handling all active connections and does not accept connections any more
	ServerShutdown func()

	// Custom command line argument groups with their descriptions
	CommandLineOptionsGroups []swag.CommandLineOptionsGroup

	// User defined logger function.
	Logger func(string, ...interface{})
}

// SetDefaultProduces sets the default produces media type
func (o *QdbAPIRestAPI) SetDefaultProduces(mediaType string) {
	o.defaultProduces = mediaType
}

// SetDefaultConsumes returns the default consumes media type
func (o *QdbAPIRestAPI) SetDefaultConsumes(mediaType string) {
	o.defaultConsumes = mediaType
}

// SetSpec sets a spec that will be served for the clients.
func (o *QdbAPIRestAPI) SetSpec(spec *loads.Document) {
	o.spec = spec
}

// DefaultProduces returns the default produces media type
func (o *QdbAPIRestAPI) DefaultProduces() string {
	return o.defaultProduces
}

// DefaultConsumes returns the default consumes media type
func (o *QdbAPIRestAPI) DefaultConsumes() string {
	return o.defaultConsumes
}

// Formats returns the registered string formats
func (o *QdbAPIRestAPI) Formats() strfmt.Registry {
	return o.formats
}

// RegisterFormat registers a custom format validator
func (o *QdbAPIRestAPI) RegisterFormat(name string, format strfmt.Format, validator strfmt.Validator) {
	o.formats.Add(name, format, validator)
}

// Validate validates the registrations in the QdbAPIRestAPI
func (o *QdbAPIRestAPI) Validate() error {
	var unregistered []string

	if o.JSONConsumer == nil {
		unregistered = append(unregistered, "JSONConsumer")
	}
	if o.ProtobufConsumer == nil {
		unregistered = append(unregistered, "ProtobufConsumer")
	}

	if o.CsvProducer == nil {
		unregistered = append(unregistered, "CsvProducer")
	}
	if o.JSONProducer == nil {
		unregistered = append(unregistered, "JSONProducer")
	}
	if o.ProtobufProducer == nil {
		unregistered = append(unregistered, "ProtobufProducer")
	}

	if o.BearerAuth == nil {
		unregistered = append(unregistered, "AuthorizationAuth")
	}
	if o.URLParamAuth == nil {
		unregistered = append(unregistered, "TokenAuth")
	}

	if o.ClusterGetClusterHandler == nil {
		unregistered = append(unregistered, "cluster.GetClusterHandler")
	}
	if o.ClusterGetNodeHandler == nil {
		unregistered = append(unregistered, "cluster.GetNodeHandler")
	}
	if o.GetTableCsvHandler == nil {
		unregistered = append(unregistered, "GetTableCsvHandler")
	}
	if o.LoginHandler == nil {
		unregistered = append(unregistered, "LoginHandler")
	}
	if o.QueryPostQueryHandler == nil {
		unregistered = append(unregistered, "query.PostQueryHandler")
	}
	if o.PrometheusReadHandler == nil {
		unregistered = append(unregistered, "PrometheusReadHandler")
	}
	if o.PrometheusWriteHandler == nil {
		unregistered = append(unregistered, "PrometheusWriteHandler")
	}

	if len(unregistered) > 0 {
		return fmt.Errorf("missing registration: %s", strings.Join(unregistered, ", "))
	}

	return nil
}

// ServeErrorFor gets a error handler for a given operation id
func (o *QdbAPIRestAPI) ServeErrorFor(operationID string) func(http.ResponseWriter, *http.Request, error) {
	return o.ServeError
}

// AuthenticatorsFor gets the authenticators for the specified security schemes
func (o *QdbAPIRestAPI) AuthenticatorsFor(schemes map[string]spec.SecurityScheme) map[string]runtime.Authenticator {
	result := make(map[string]runtime.Authenticator)
	for name := range schemes {
		switch name {
		case "Bearer":
			scheme := schemes[name]
			result[name] = o.APIKeyAuthenticator(scheme.Name, scheme.In, func(token string) (interface{}, error) {
				return o.BearerAuth(token)
			})

		case "UrlParam":
			scheme := schemes[name]
			result[name] = o.APIKeyAuthenticator(scheme.Name, scheme.In, func(token string) (interface{}, error) {
				return o.URLParamAuth(token)
			})

		}
	}
	return result
}

// Authorizer returns the registered authorizer
func (o *QdbAPIRestAPI) Authorizer() runtime.Authorizer {
	return o.APIAuthorizer
}

// ConsumersFor gets the consumers for the specified media types.
// MIME type parameters are ignored here.
func (o *QdbAPIRestAPI) ConsumersFor(mediaTypes []string) map[string]runtime.Consumer {
	result := make(map[string]runtime.Consumer, len(mediaTypes))
	for _, mt := range mediaTypes {
		switch mt {
		case "application/json":
			result["application/json"] = o.JSONConsumer
		case "application/x-protobuf":
			result["application/x-protobuf"] = o.ProtobufConsumer
		}

		if c, ok := o.customConsumers[mt]; ok {
			result[mt] = c
		}
	}
	return result
}

// ProducersFor gets the producers for the specified media types.
// MIME type parameters are ignored here.
func (o *QdbAPIRestAPI) ProducersFor(mediaTypes []string) map[string]runtime.Producer {
	result := make(map[string]runtime.Producer, len(mediaTypes))
	for _, mt := range mediaTypes {
		switch mt {
		case "text/csv":
			result["text/csv"] = o.CsvProducer
		case "application/json":
			result["application/json"] = o.JSONProducer
		case "application/x-protobuf":
			result["application/x-protobuf"] = o.ProtobufProducer
		}

		if p, ok := o.customProducers[mt]; ok {
			result[mt] = p
		}
	}
	return result
}

// HandlerFor gets a http.Handler for the provided operation method and path
func (o *QdbAPIRestAPI) HandlerFor(method, path string) (http.Handler, bool) {
	if o.handlers == nil {
		return nil, false
	}
	um := strings.ToUpper(method)
	if _, ok := o.handlers[um]; !ok {
		return nil, false
	}
	if path == "/" {
		path = ""
	}
	h, ok := o.handlers[um][path]
	return h, ok
}

// Context returns the middleware context for the qdb API rest API
func (o *QdbAPIRestAPI) Context() *middleware.Context {
	if o.context == nil {
		o.context = middleware.NewRoutableContext(o.spec, o, nil)
	}

	return o.context
}

func (o *QdbAPIRestAPI) initHandlerCache() {
	o.Context() // don't care about the result, just that the initialization happened
	if o.handlers == nil {
		o.handlers = make(map[string]map[string]http.Handler)
	}

	if o.handlers["GET"] == nil {
		o.handlers["GET"] = make(map[string]http.Handler)
	}
	o.handlers["GET"]["/cluster"] = cluster.NewGetCluster(o.context, o.ClusterGetClusterHandler)
	if o.handlers["GET"] == nil {
		o.handlers["GET"] = make(map[string]http.Handler)
	}
	o.handlers["GET"]["/cluster/nodes/{id}"] = cluster.NewGetNode(o.context, o.ClusterGetNodeHandler)
	if o.handlers["GET"] == nil {
		o.handlers["GET"] = make(map[string]http.Handler)
	}
	o.handlers["GET"]["/tables/{name}.csv"] = NewGetTableCsv(o.context, o.GetTableCsvHandler)
	if o.handlers["POST"] == nil {
		o.handlers["POST"] = make(map[string]http.Handler)
	}
	o.handlers["POST"]["/login"] = NewLogin(o.context, o.LoginHandler)
	if o.handlers["POST"] == nil {
		o.handlers["POST"] = make(map[string]http.Handler)
	}
	o.handlers["POST"]["/query"] = query.NewPostQuery(o.context, o.QueryPostQueryHandler)
	if o.handlers["POST"] == nil {
		o.handlers["POST"] = make(map[string]http.Handler)
	}
	o.handlers["POST"]["/prometheus/read"] = NewPrometheusRead(o.context, o.PrometheusReadHandler)
	if o.handlers["POST"] == nil {
		o.handlers["POST"] = make(map[string]http.Handler)
	}
	o.handlers["POST"]["/prometheus/write"] = NewPrometheusWrite(o.context, o.PrometheusWriteHandler)
}

// Serve creates a http handler to serve the API over HTTP
// can be used directly in http.ListenAndServe(":8000", api.Serve(nil))
func (o *QdbAPIRestAPI) Serve(builder middleware.Builder) http.Handler {
	o.Init()

	if o.Middleware != nil {
		return o.Middleware(builder)
	}
	return o.context.APIHandler(builder)
}

// Init allows you to just initialize the handler cache, you can then recompose the middleware as you see fit
func (o *QdbAPIRestAPI) Init() {
	if len(o.handlers) == 0 {
		o.initHandlerCache()
	}
}

// RegisterConsumer allows you to add (or override) a consumer for a media type.
func (o *QdbAPIRestAPI) RegisterConsumer(mediaType string, consumer runtime.Consumer) {
	o.customConsumers[mediaType] = consumer
}

// RegisterProducer allows you to add (or override) a producer for a media type.
func (o *QdbAPIRestAPI) RegisterProducer(mediaType string, producer runtime.Producer) {
	o.customProducers[mediaType] = producer
}

// AddMiddlewareFor adds a http middleware to existing handler
func (o *QdbAPIRestAPI) AddMiddlewareFor(method, path string, builder middleware.Builder) {
	um := strings.ToUpper(method)
	if path == "/" {
		path = ""
	}
	o.Init()
	if h, ok := o.handlers[um][path]; ok {
		o.handlers[method][path] = builder(h)
	}
}
