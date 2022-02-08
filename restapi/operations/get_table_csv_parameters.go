// Code generated by go-swagger; DO NOT EDIT.

package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/runtime/middleware"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/validate"
)

// NewGetTableCsvParams creates a new GetTableCsvParams object
//
// There are no default values defined in the spec.
func NewGetTableCsvParams() GetTableCsvParams {

	return GetTableCsvParams{}
}

// GetTableCsvParams contains all the bound params for the get table csv operation
// typically these are obtained from a http.Request
//
// swagger:parameters get-table-csv
type GetTableCsvParams struct {

	// HTTP Request Object
	HTTPRequest *http.Request `json:"-"`

	/*
	  Required: true
	  In: query
	*/
	End string
	/*
	  Required: true
	  In: path
	*/
	Name string
	/*
	  Required: true
	  In: query
	*/
	Start string
}

// BindRequest both binds and validates a request, it assumes that complex things implement a Validatable(strfmt.Registry) error interface
// for simple values it will use straight method calls.
//
// To ensure default values, the struct must have been initialized with NewGetTableCsvParams() beforehand.
func (o *GetTableCsvParams) BindRequest(r *http.Request, route *middleware.MatchedRoute) error {
	var res []error

	o.HTTPRequest = r

	qs := runtime.Values(r.URL.Query())

	qEnd, qhkEnd, _ := qs.GetOK("end")
	if err := o.bindEnd(qEnd, qhkEnd, route.Formats); err != nil {
		res = append(res, err)
	}

	rName, rhkName, _ := route.Params.GetOK("name")
	if err := o.bindName(rName, rhkName, route.Formats); err != nil {
		res = append(res, err)
	}

	qStart, qhkStart, _ := qs.GetOK("start")
	if err := o.bindStart(qStart, qhkStart, route.Formats); err != nil {
		res = append(res, err)
	}
	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

// bindEnd binds and validates parameter End from query.
func (o *GetTableCsvParams) bindEnd(rawData []string, hasKey bool, formats strfmt.Registry) error {
	if !hasKey {
		return errors.Required("end", "query", rawData)
	}
	var raw string
	if len(rawData) > 0 {
		raw = rawData[len(rawData)-1]
	}

	// Required: true
	// AllowEmptyValue: false

	if err := validate.RequiredString("end", "query", raw); err != nil {
		return err
	}
	o.End = raw

	return nil
}

// bindName binds and validates parameter Name from path.
func (o *GetTableCsvParams) bindName(rawData []string, hasKey bool, formats strfmt.Registry) error {
	var raw string
	if len(rawData) > 0 {
		raw = rawData[len(rawData)-1]
	}

	// Required: true
	// Parameter is provided by construction from the route
	o.Name = raw

	return nil
}

// bindStart binds and validates parameter Start from query.
func (o *GetTableCsvParams) bindStart(rawData []string, hasKey bool, formats strfmt.Registry) error {
	if !hasKey {
		return errors.Required("start", "query", rawData)
	}
	var raw string
	if len(rawData) > 0 {
		raw = rawData[len(rawData)-1]
	}

	// Required: true
	// AllowEmptyValue: false

	if err := validate.RequiredString("start", "query", raw); err != nil {
		return err
	}
	o.Start = raw

	return nil
}
