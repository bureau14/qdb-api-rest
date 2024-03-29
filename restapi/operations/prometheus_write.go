// Code generated by go-swagger; DO NOT EDIT.

package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the generate command

import (
	"net/http"

	"github.com/go-openapi/runtime/middleware"
)

// PrometheusWriteHandlerFunc turns a function with the right signature into a prometheus write handler
type PrometheusWriteHandlerFunc func(PrometheusWriteParams) middleware.Responder

// Handle executing the request and returning a response
func (fn PrometheusWriteHandlerFunc) Handle(params PrometheusWriteParams) middleware.Responder {
	return fn(params)
}

// PrometheusWriteHandler interface for that can handle valid prometheus write params
type PrometheusWriteHandler interface {
	Handle(PrometheusWriteParams) middleware.Responder
}

// NewPrometheusWrite creates a new http.Handler for the prometheus write operation
func NewPrometheusWrite(ctx *middleware.Context, handler PrometheusWriteHandler) *PrometheusWrite {
	return &PrometheusWrite{Context: ctx, Handler: handler}
}

/* PrometheusWrite swagger:route POST /prometheus/write prometheusWrite

The write endpoint for remote Prometheus storage

*/
type PrometheusWrite struct {
	Context *middleware.Context
	Handler PrometheusWriteHandler
}

func (o *PrometheusWrite) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, rCtx, _ := o.Context.RouteInfo(r)
	if rCtx != nil {
		*r = *rCtx
	}
	var Params = NewPrometheusWriteParams()
	if err := o.Context.BindValidRequest(r, route, &Params); err != nil { // bind params
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}

	res := o.Handler.Handle(Params) // actually handle the request
	o.Context.Respond(rw, r, route.Produces, route, res)

}
