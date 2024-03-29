// Code generated by go-swagger; DO NOT EDIT.

package operations

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"io"
	"net/http"

	"github.com/go-openapi/runtime"

	"github.com/bureau14/qdb-api-rest/models"
)

// PrometheusReadOKCode is the HTTP code returned for type PrometheusReadOK
const PrometheusReadOKCode int = 200

/*PrometheusReadOK OK

swagger:response prometheusReadOK
*/
type PrometheusReadOK struct {

	/*
	  In: Body
	*/
	Payload io.ReadCloser `json:"body,omitempty"`
}

// NewPrometheusReadOK creates PrometheusReadOK with default headers values
func NewPrometheusReadOK() *PrometheusReadOK {

	return &PrometheusReadOK{}
}

// WithPayload adds the payload to the prometheus read o k response
func (o *PrometheusReadOK) WithPayload(payload io.ReadCloser) *PrometheusReadOK {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the prometheus read o k response
func (o *PrometheusReadOK) SetPayload(payload io.ReadCloser) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *PrometheusReadOK) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(200)
	payload := o.Payload
	if err := producer.Produce(rw, payload); err != nil {
		panic(err) // let the recovery middleware deal with this
	}
}

// PrometheusReadInternalServerErrorCode is the HTTP code returned for type PrometheusReadInternalServerError
const PrometheusReadInternalServerErrorCode int = 500

/*PrometheusReadInternalServerError Internal Error.

swagger:response prometheusReadInternalServerError
*/
type PrometheusReadInternalServerError struct {

	/*
	  In: Body
	*/
	Payload *models.QdbError `json:"body,omitempty"`
}

// NewPrometheusReadInternalServerError creates PrometheusReadInternalServerError with default headers values
func NewPrometheusReadInternalServerError() *PrometheusReadInternalServerError {

	return &PrometheusReadInternalServerError{}
}

// WithPayload adds the payload to the prometheus read internal server error response
func (o *PrometheusReadInternalServerError) WithPayload(payload *models.QdbError) *PrometheusReadInternalServerError {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the prometheus read internal server error response
func (o *PrometheusReadInternalServerError) SetPayload(payload *models.QdbError) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *PrometheusReadInternalServerError) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(500)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}
