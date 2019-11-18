// Code generated by go-swagger; DO NOT EDIT.

package query

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the generate command

import (
	"net/http"

	middleware "github.com/go-openapi/runtime/middleware"

	models "github.com/bureau14/qdb-api-rest/models"
)

// PostQueryHandlerFunc turns a function with the right signature into a post query handler
type PostQueryHandlerFunc func(PostQueryParams, *models.Principal) middleware.Responder

// Handle executing the request and returning a response
func (fn PostQueryHandlerFunc) Handle(params PostQueryParams, principal *models.Principal) middleware.Responder {
	return fn(params, principal)
}

// PostQueryHandler interface for that can handle valid post query params
type PostQueryHandler interface {
	Handle(PostQueryParams, *models.Principal) middleware.Responder
}

// NewPostQuery creates a new http.Handler for the post query operation
func NewPostQuery(ctx *middleware.Context, handler PostQueryHandler) *PostQuery {
	return &PostQuery{Context: ctx, Handler: handler}
}

/*PostQuery swagger:route POST /query query postQuery

Query the database

*/
type PostQuery struct {
	Context *middleware.Context
	Handler PostQueryHandler
}

func (o *PostQuery) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	route, rCtx, _ := o.Context.RouteInfo(r)
	if rCtx != nil {
		r = rCtx
	}
	var Params = NewPostQueryParams()

	uprinc, aCtx, err := o.Context.Authorize(r, route)
	if err != nil {
		o.Context.Respond(rw, r, route.Produces, route, err)
		return
	}
	if aCtx != nil {
		r = aCtx
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