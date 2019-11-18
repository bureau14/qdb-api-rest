// Code generated by go-swagger; DO NOT EDIT.

package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"io"
	"net/http"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
)

// NewPrometheusReadParams creates a new PrometheusReadParams object
// no default values defined in spec.
func NewPrometheusReadParams() PrometheusReadParams {

	return PrometheusReadParams{}
}

// PrometheusReadParams contains all the bound params for the prometheus read operation
// typically these are obtained from a http.Request
//
// swagger:parameters prometheusRead
type PrometheusReadParams struct {

	// HTTP Request Object
	HTTPRequest *http.Request `json:"-"`

	/*The samples in snappy-encoded protocol buffer format sent from Prometheus
	  Required: true
	  In: body
	*/
	Query io.ReadCloser
}

// BindRequest both binds and validates a request, it assumes that complex things implement a Validatable(strfmt.Registry) error interface
// for simple values it will use straight method calls.
//
// To ensure default values, the struct must have been initialized with NewPrometheusReadParams() beforehand.
func (o *PrometheusReadParams) BindRequest(r *http.Request, route *middleware.MatchedRoute) error {
	var res []error

	o.HTTPRequest = r

	if runtime.HasBody(r) {
		o.Query = r.Body
	} else {
		res = append(res, errors.Required("query", "body"))
	}
	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
