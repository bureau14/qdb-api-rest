// Code generated by go-swagger; DO NOT EDIT.

package cluster

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"

	"github.com/go-openapi/runtime"

	"github.com/bureau14/qdb-api-rest/models"
)

// GetClusterOKCode is the HTTP code returned for type GetClusterOK
const GetClusterOKCode int = 200

/*GetClusterOK Successful operation

swagger:response getClusterOK
*/
type GetClusterOK struct {

	/*
	  In: Body
	*/
	Payload *models.Cluster `json:"body,omitempty"`
}

// NewGetClusterOK creates GetClusterOK with default headers values
func NewGetClusterOK() *GetClusterOK {

	return &GetClusterOK{}
}

// WithPayload adds the payload to the get cluster o k response
func (o *GetClusterOK) WithPayload(payload *models.Cluster) *GetClusterOK {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get cluster o k response
func (o *GetClusterOK) SetPayload(payload *models.Cluster) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetClusterOK) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(200)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetClusterBadRequestCode is the HTTP code returned for type GetClusterBadRequest
const GetClusterBadRequestCode int = 400

/*GetClusterBadRequest Bad Request.

swagger:response getClusterBadRequest
*/
type GetClusterBadRequest struct {

	/*
	  In: Body
	*/
	Payload *models.QdbError `json:"body,omitempty"`
}

// NewGetClusterBadRequest creates GetClusterBadRequest with default headers values
func NewGetClusterBadRequest() *GetClusterBadRequest {

	return &GetClusterBadRequest{}
}

// WithPayload adds the payload to the get cluster bad request response
func (o *GetClusterBadRequest) WithPayload(payload *models.QdbError) *GetClusterBadRequest {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get cluster bad request response
func (o *GetClusterBadRequest) SetPayload(payload *models.QdbError) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetClusterBadRequest) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(400)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetClusterInternalServerErrorCode is the HTTP code returned for type GetClusterInternalServerError
const GetClusterInternalServerErrorCode int = 500

/*GetClusterInternalServerError Internal Error.

swagger:response getClusterInternalServerError
*/
type GetClusterInternalServerError struct {

	/*
	  In: Body
	*/
	Payload *models.QdbError `json:"body,omitempty"`
}

// NewGetClusterInternalServerError creates GetClusterInternalServerError with default headers values
func NewGetClusterInternalServerError() *GetClusterInternalServerError {

	return &GetClusterInternalServerError{}
}

// WithPayload adds the payload to the get cluster internal server error response
func (o *GetClusterInternalServerError) WithPayload(payload *models.QdbError) *GetClusterInternalServerError {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get cluster internal server error response
func (o *GetClusterInternalServerError) SetPayload(payload *models.QdbError) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetClusterInternalServerError) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(500)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}
