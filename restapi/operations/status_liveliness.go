// Code generated by go-swagger; DO NOT EDIT.

package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the generate command

import (
	"net/http"

	"github.com/go-openapi/runtime/middleware"
)

// StatusLivelinessHandlerFunc turns a function with the right signature into a status liveliness handler
type StatusLivelinessHandlerFunc func(StatusLivelinessParams) middleware.Responder

// Handle executing the request and returning a response
func (fn StatusLivelinessHandlerFunc) Handle(params StatusLivelinessParams) middleware.Responder {
	return fn(params)
}

// StatusLivelinessHandler interface for that can handle valid status liveliness params
type StatusLivelinessHandler interface {
	Handle(StatusLivelinessParams) middleware.Responder
}

// NewStatusLiveliness creates a new http.Handler for the status liveliness operation
func NewStatusLiveliness(ctx *middleware.Context, handler StatusLivelinessHandler) *StatusLiveliness {
	return &StatusLiveliness{Context: ctx, Handler: handler}
}

/* StatusLiveliness swagger:route GET /status/liveliness statusLiveliness

StatusLiveliness status liveliness API

*/
type StatusLiveliness struct {
	Context *middleware.Context
	Handler StatusLivelinessHandler
}

func (o *StatusLiveliness) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, rCtx, _ := o.Context.RouteInfo(r)
	if rCtx != nil {
		*r = *rCtx
	}
	var Params = NewStatusLivelinessParams()
	if err := o.Context.BindValidRequest(r, route, &Params); err != nil { // bind params
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}

	res := o.Handler.Handle(Params) // actually handle the request
	o.Context.Respond(rw, r, route.Produces, route, res)

}
