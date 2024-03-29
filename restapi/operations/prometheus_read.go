// Code generated by go-swagger; DO NOT EDIT.

package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the generate command

import (
	"net/http"

	"github.com/go-openapi/runtime/middleware"
)

// PrometheusReadHandlerFunc turns a function with the right signature into a prometheus read handler
type PrometheusReadHandlerFunc func(PrometheusReadParams) middleware.Responder

// Handle executing the request and returning a response
func (fn PrometheusReadHandlerFunc) Handle(params PrometheusReadParams) middleware.Responder {
	return fn(params)
}

// PrometheusReadHandler interface for that can handle valid prometheus read params
type PrometheusReadHandler interface {
	Handle(PrometheusReadParams) middleware.Responder
}

// NewPrometheusRead creates a new http.Handler for the prometheus read operation
func NewPrometheusRead(ctx *middleware.Context, handler PrometheusReadHandler) *PrometheusRead {
	return &PrometheusRead{Context: ctx, Handler: handler}
}

/* PrometheusRead swagger:route POST /prometheus/read prometheusRead

The read endpoint for remote Prometheus storage

*/
type PrometheusRead struct {
	Context *middleware.Context
	Handler PrometheusReadHandler
}

func (o *PrometheusRead) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, rCtx, _ := o.Context.RouteInfo(r)
	if rCtx != nil {
		*r = *rCtx
	}
	var Params = NewPrometheusReadParams()
	if err := o.Context.BindValidRequest(r, route, &Params); err != nil { // bind params
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}

	res := o.Handler.Handle(Params) // actually handle the request
	o.Context.Respond(rw, r, route.Produces, route, res)

}
