// Code generated by go-swagger; DO NOT EDIT.

package models

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"encoding/json"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
	"github.com/go-openapi/validate"
)

// Cluster cluster
//
// swagger:model Cluster
type Cluster struct {

	// disk total
	// Required: true
	// Minimum: 0
	DiskTotal *int64 `json:"diskTotal"`

	// disk used
	// Required: true
	// Minimum: 0
	DiskUsed *int64 `json:"diskUsed"`

	// memory total
	// Required: true
	// Minimum: 0
	MemoryTotal *int64 `json:"memoryTotal"`

	// memory used
	// Required: true
	// Minimum: 0
	MemoryUsed *int64 `json:"memoryUsed"`

	// nodes
	// Required: true
	Nodes []string `json:"nodes"`

	// status
	// Required: true
	// Enum: [stable unstable unreachable]
	Status *string `json:"status"`
}

// Validate validates this cluster
func (m *Cluster) Validate(formats strfmt.Registry) error {
	var res []error

	if err := m.validateDiskTotal(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateDiskUsed(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateMemoryTotal(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateMemoryUsed(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateNodes(formats); err != nil {
		res = append(res, err)
	}

	if err := m.validateStatus(formats); err != nil {
		res = append(res, err)
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}

func (m *Cluster) validateDiskTotal(formats strfmt.Registry) error {

	if err := validate.Required("diskTotal", "body", m.DiskTotal); err != nil {
		return err
	}

	if err := validate.MinimumInt("diskTotal", "body", *m.DiskTotal, 0, false); err != nil {
		return err
	}

	return nil
}

func (m *Cluster) validateDiskUsed(formats strfmt.Registry) error {

	if err := validate.Required("diskUsed", "body", m.DiskUsed); err != nil {
		return err
	}

	if err := validate.MinimumInt("diskUsed", "body", *m.DiskUsed, 0, false); err != nil {
		return err
	}

	return nil
}

func (m *Cluster) validateMemoryTotal(formats strfmt.Registry) error {

	if err := validate.Required("memoryTotal", "body", m.MemoryTotal); err != nil {
		return err
	}

	if err := validate.MinimumInt("memoryTotal", "body", *m.MemoryTotal, 0, false); err != nil {
		return err
	}

	return nil
}

func (m *Cluster) validateMemoryUsed(formats strfmt.Registry) error {

	if err := validate.Required("memoryUsed", "body", m.MemoryUsed); err != nil {
		return err
	}

	if err := validate.MinimumInt("memoryUsed", "body", *m.MemoryUsed, 0, false); err != nil {
		return err
	}

	return nil
}

func (m *Cluster) validateNodes(formats strfmt.Registry) error {

	if err := validate.Required("nodes", "body", m.Nodes); err != nil {
		return err
	}

	return nil
}

var clusterTypeStatusPropEnum []interface{}

func init() {
	var res []string
	if err := json.Unmarshal([]byte(`["stable","unstable","unreachable"]`), &res); err != nil {
		panic(err)
	}
	for _, v := range res {
		clusterTypeStatusPropEnum = append(clusterTypeStatusPropEnum, v)
	}
}

const (

	// ClusterStatusStable captures enum value "stable"
	ClusterStatusStable string = "stable"

	// ClusterStatusUnstable captures enum value "unstable"
	ClusterStatusUnstable string = "unstable"

	// ClusterStatusUnreachable captures enum value "unreachable"
	ClusterStatusUnreachable string = "unreachable"
)

// prop value enum
func (m *Cluster) validateStatusEnum(path, location string, value string) error {
	if err := validate.EnumCase(path, location, value, clusterTypeStatusPropEnum, true); err != nil {
		return err
	}
	return nil
}

func (m *Cluster) validateStatus(formats strfmt.Registry) error {

	if err := validate.Required("status", "body", m.Status); err != nil {
		return err
	}

	// value enum
	if err := m.validateStatusEnum("status", "body", *m.Status); err != nil {
		return err
	}

	return nil
}

// ContextValidate validates this cluster based on context it is used
func (m *Cluster) ContextValidate(ctx context.Context, formats strfmt.Registry) error {
	return nil
}

// MarshalBinary interface implementation
func (m *Cluster) MarshalBinary() ([]byte, error) {
	if m == nil {
		return nil, nil
	}
	return swag.WriteJSON(m)
}

// UnmarshalBinary interface implementation
func (m *Cluster) UnmarshalBinary(b []byte) error {
	var res Cluster
	if err := swag.ReadJSON(b, &res); err != nil {
		return err
	}
	*m = res
	return nil
}
