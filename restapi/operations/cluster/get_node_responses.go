// Code generated by go-swagger; DO NOT EDIT.

package cluster

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"

	"github.com/go-openapi/runtime"

	"github.com/bureau14/qdb-api-rest/models"
)

// GetNodeOKCode is the HTTP code returned for type GetNodeOK
const GetNodeOKCode int = 200

/*GetNodeOK Successful operation

swagger:response getNodeOK
*/
type GetNodeOK struct {

	/*
	  In: Body
	*/
	Payload *models.Node `json:"body,omitempty"`
}

// NewGetNodeOK creates GetNodeOK with default headers values
func NewGetNodeOK() *GetNodeOK {

	return &GetNodeOK{}
}

// WithPayload adds the payload to the get node o k response
func (o *GetNodeOK) WithPayload(payload *models.Node) *GetNodeOK {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get node o k response
func (o *GetNodeOK) SetPayload(payload *models.Node) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetNodeOK) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(200)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetNodeBadRequestCode is the HTTP code returned for type GetNodeBadRequest
const GetNodeBadRequestCode int = 400

/*GetNodeBadRequest Bad Request.

swagger:response getNodeBadRequest
*/
type GetNodeBadRequest struct {

	/*
	  In: Body
	*/
	Payload *models.QdbError `json:"body,omitempty"`
}

// NewGetNodeBadRequest creates GetNodeBadRequest with default headers values
func NewGetNodeBadRequest() *GetNodeBadRequest {

	return &GetNodeBadRequest{}
}

// WithPayload adds the payload to the get node bad request response
func (o *GetNodeBadRequest) WithPayload(payload *models.QdbError) *GetNodeBadRequest {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get node bad request response
func (o *GetNodeBadRequest) SetPayload(payload *models.QdbError) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetNodeBadRequest) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(400)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetNodeNotFoundCode is the HTTP code returned for type GetNodeNotFound
const GetNodeNotFoundCode int = 404

/*GetNodeNotFound The requested resource could not be found but may be available again in the future.

swagger:response getNodeNotFound
*/
type GetNodeNotFound struct {

	/*
	  In: Body
	*/
	Payload string `json:"body,omitempty"`
}

// NewGetNodeNotFound creates GetNodeNotFound with default headers values
func NewGetNodeNotFound() *GetNodeNotFound {

	return &GetNodeNotFound{}
}

// WithPayload adds the payload to the get node not found response
func (o *GetNodeNotFound) WithPayload(payload string) *GetNodeNotFound {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get node not found response
func (o *GetNodeNotFound) SetPayload(payload string) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetNodeNotFound) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(404)
	payload := o.Payload
	if err := producer.Produce(rw, payload); err != nil {
		panic(err) // let the recovery middleware deal with this
	}
}

// GetNodeInternalServerErrorCode is the HTTP code returned for type GetNodeInternalServerError
const GetNodeInternalServerErrorCode int = 500

/*GetNodeInternalServerError Internal Error.

swagger:response getNodeInternalServerError
*/
type GetNodeInternalServerError struct {

	/*
	  In: Body
	*/
	Payload *models.QdbError `json:"body,omitempty"`
}

// NewGetNodeInternalServerError creates GetNodeInternalServerError with default headers values
func NewGetNodeInternalServerError() *GetNodeInternalServerError {

	return &GetNodeInternalServerError{}
}

// WithPayload adds the payload to the get node internal server error response
func (o *GetNodeInternalServerError) WithPayload(payload *models.QdbError) *GetNodeInternalServerError {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get node internal server error response
func (o *GetNodeInternalServerError) SetPayload(payload *models.QdbError) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetNodeInternalServerError) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(500)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}
