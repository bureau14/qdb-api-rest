// Code generated by go-swagger; DO NOT EDIT.

package tags

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"net/http"

	"github.com/go-openapi/runtime"

	"github.com/bureau14/qdb-api-rest/models"
)

// GetTagsOKCode is the HTTP code returned for type GetTagsOK
const GetTagsOKCode int = 200

/*GetTagsOK Successful Operation

swagger:response getTagsOK
*/
type GetTagsOK struct {

	/*
	  In: Body
	*/
	Payload *models.QueryResult `json:"body,omitempty"`
}

// NewGetTagsOK creates GetTagsOK with default headers values
func NewGetTagsOK() *GetTagsOK {

	return &GetTagsOK{}
}

// WithPayload adds the payload to the get tags o k response
func (o *GetTagsOK) WithPayload(payload *models.QueryResult) *GetTagsOK {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get tags o k response
func (o *GetTagsOK) SetPayload(payload *models.QueryResult) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetTagsOK) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(200)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetTagsBadRequestCode is the HTTP code returned for type GetTagsBadRequest
const GetTagsBadRequestCode int = 400

/*GetTagsBadRequest Bad Request.

swagger:response getTagsBadRequest
*/
type GetTagsBadRequest struct {

	/*
	  In: Body
	*/
	Payload *models.QdbError `json:"body,omitempty"`
}

// NewGetTagsBadRequest creates GetTagsBadRequest with default headers values
func NewGetTagsBadRequest() *GetTagsBadRequest {

	return &GetTagsBadRequest{}
}

// WithPayload adds the payload to the get tags bad request response
func (o *GetTagsBadRequest) WithPayload(payload *models.QdbError) *GetTagsBadRequest {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get tags bad request response
func (o *GetTagsBadRequest) SetPayload(payload *models.QdbError) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetTagsBadRequest) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(400)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}

// GetTagsInternalServerErrorCode is the HTTP code returned for type GetTagsInternalServerError
const GetTagsInternalServerErrorCode int = 500

/*GetTagsInternalServerError Internal Error.

swagger:response getTagsInternalServerError
*/
type GetTagsInternalServerError struct {

	/*
	  In: Body
	*/
	Payload *models.QdbError `json:"body,omitempty"`
}

// NewGetTagsInternalServerError creates GetTagsInternalServerError with default headers values
func NewGetTagsInternalServerError() *GetTagsInternalServerError {

	return &GetTagsInternalServerError{}
}

// WithPayload adds the payload to the get tags internal server error response
func (o *GetTagsInternalServerError) WithPayload(payload *models.QdbError) *GetTagsInternalServerError {
	o.Payload = payload
	return o
}

// SetPayload sets the payload to the get tags internal server error response
func (o *GetTagsInternalServerError) SetPayload(payload *models.QdbError) {
	o.Payload = payload
}

// WriteResponse to the client
func (o *GetTagsInternalServerError) WriteResponse(rw http.ResponseWriter, producer runtime.Producer) {

	rw.WriteHeader(500)
	if o.Payload != nil {
		payload := o.Payload
		if err := producer.Produce(rw, payload); err != nil {
			panic(err) // let the recovery middleware deal with this
		}
	}
}
