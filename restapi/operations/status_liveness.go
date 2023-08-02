// Code generated by go-swagger; DO NOT EDIT.

package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the generate command

import (
	"net/http"

	"github.com/go-openapi/runtime/middleware"
)

// StatusLivenessHandlerFunc turns a function with the right signature into a status liveness handler
type StatusLivenessHandlerFunc func(StatusLivenessParams) middleware.Responder

// Handle executing the request and returning a response
func (fn StatusLivenessHandlerFunc) Handle(params StatusLivenessParams) middleware.Responder {
	return fn(params)
}

// StatusLivenessHandler interface for that can handle valid status liveness params
type StatusLivenessHandler interface {
	Handle(StatusLivenessParams) middleware.Responder
}

// NewStatusLiveness creates a new http.Handler for the status liveness operation
func NewStatusLiveness(ctx *middleware.Context, handler StatusLivenessHandler) *StatusLiveness {
	return &StatusLiveness{Context: ctx, Handler: handler}
}

/* StatusLiveness swagger:route GET /status/liveness statusLiveness

StatusLiveness status liveness API

*/
type StatusLiveness struct {
	Context *middleware.Context
	Handler StatusLivenessHandler
}

func (o *StatusLiveness) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, rCtx, _ := o.Context.RouteInfo(r)
	if rCtx != nil {
		*r = *rCtx
	}
	var Params = NewStatusLivenessParams()
	if err := o.Context.BindValidRequest(r, route, &Params); err != nil { // bind params
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}

	res := o.Handler.Handle(Params) // actually handle the request
	o.Context.Respond(rw, r, route.Produces, route, res)

}