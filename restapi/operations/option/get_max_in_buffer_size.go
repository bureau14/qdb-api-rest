// Code generated by go-swagger; DO NOT EDIT.

package option

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the generate command

import (
	"net/http"

	"github.com/go-openapi/runtime/middleware"

	"github.com/bureau14/qdb-api-rest/models"
)

// GetMaxInBufferSizeHandlerFunc turns a function with the right signature into a get max in buffer size handler
type GetMaxInBufferSizeHandlerFunc func(GetMaxInBufferSizeParams, *models.Principal) middleware.Responder

// Handle executing the request and returning a response
func (fn GetMaxInBufferSizeHandlerFunc) Handle(params GetMaxInBufferSizeParams, principal *models.Principal) middleware.Responder {
	return fn(params, principal)
}

// GetMaxInBufferSizeHandler interface for that can handle valid get max in buffer size params
type GetMaxInBufferSizeHandler interface {
	Handle(GetMaxInBufferSizeParams, *models.Principal) middleware.Responder
}

// NewGetMaxInBufferSize creates a new http.Handler for the get max in buffer size operation
func NewGetMaxInBufferSize(ctx *middleware.Context, handler GetMaxInBufferSizeHandler) *GetMaxInBufferSize {
	return &GetMaxInBufferSize{Context: ctx, Handler: handler}
}

/* GetMaxInBufferSize swagger:route GET /option/max-in-buffer-size option max-in-buffer-size getMaxInBufferSize

Get the client max in buffer size

*/
type GetMaxInBufferSize struct {
	Context *middleware.Context
	Handler GetMaxInBufferSizeHandler
}

func (o *GetMaxInBufferSize) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, rCtx, _ := o.Context.RouteInfo(r)
	if rCtx != nil {
		*r = *rCtx
	}
	var Params = NewGetMaxInBufferSizeParams()
	uprinc, aCtx, err := o.Context.Authorize(r, route)
	if err != nil {
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}
	if aCtx != nil {
		*r = *aCtx
	}
	var principal *models.Principal
	if uprinc != nil {
		principal = uprinc.(*models.Principal) // this is really a models.Principal, I promise
	}

	if err := o.Context.BindValidRequest(r, route, &Params); err != nil { // bind params
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}

	res := o.Handler.Handle(Params, principal) // actually handle the request
	o.Context.Respond(rw, r, route.Produces, route, res)

}