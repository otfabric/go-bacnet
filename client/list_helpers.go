// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// ReadObjectList reads the Object_List of a Device object.
// maxItems caps the returned count (0 uses decode MaxElements).
//
// The whole array is requested first. Devices that cannot return it in one
// reply (no segmentation, small buffer) abort that request; the list is then
// read element by element: the count from array index 0, the entries with
// ReadPropertyMultiple, or with one ReadProperty each when the device does
// not support ReadPropertyMultiple.
func (c *Client) ReadObjectList(ctx context.Context, target Target, object bacnet.ObjectIdentifier, maxItems int) ([]bacnet.ObjectIdentifier, error) {
	val, err := c.ReadProperty(ctx, target, object, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList})
	if err == nil {
		return DecodeObjectIdentifierList(val, maxItems, c.limits)
	}
	var abort *bacnet.AbortError
	if !errors.As(err, &abort) || !responseTooLarge(abort.Reason) {
		return nil, err
	}
	return c.readObjectListByIndex(ctx, target, object, maxItems)
}

func (c *Client) readObjectListByIndex(ctx context.Context, target Target, object bacnet.ObjectIdentifier, maxItems int) ([]bacnet.ObjectIdentifier, error) {
	if maxItems <= 0 {
		maxItems = c.limits.MaxElements
	}
	indexed := func(i uint32) bacnet.PropertyReference {
		return bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList, ArrayIndex: &i}
	}
	countVal, err := c.ReadProperty(ctx, target, object, indexed(0))
	if err != nil {
		return nil, fmt.Errorf("object-list length: %w", err)
	}
	count, err := bacnet.AsUnsigned(countVal)
	if err != nil {
		return nil, fmt.Errorf("%w: object-list length: %v", bacnet.ErrMalformed, err)
	}
	if count > uint64(maxItems) {
		count = uint64(maxItems)
	}
	out := make([]bacnet.ObjectIdentifier, 0, count)
	if count == 0 {
		return out, nil
	}

	spec := service.ReadAccessSpecification{Object: object}
	for i := uint32(1); uint64(i) <= count; i++ {
		spec.Properties = append(spec.Properties, indexed(i))
	}
	results, err := c.ReadPropertyMultipleBatched(ctx, target, []service.ReadAccessSpecification{spec}, RPMBatchOptions{
		MaxBatches: int(count), // worst case: one entry per transaction
	})
	var reject *bacnet.RejectError
	switch {
	case err == nil:
		for _, pr := range results[0].Properties {
			if pr.Err != nil {
				return out, fmt.Errorf("object-list element %d: %w", len(out)+1, pr.Err)
			}
			id, e := bacnet.AsObjectID(pr.Value)
			if e != nil {
				return out, fmt.Errorf("%w: object-list element %d: %v", bacnet.ErrMalformed, len(out)+1, e)
			}
			out = append(out, id)
		}
		return out, nil
	case errors.As(err, &reject) && reject.Reason == bacnet.RejectReasonUnrecognizedService:
		// No ReadPropertyMultiple on this device: one ReadProperty per entry.
	default:
		return out, err
	}
	for i := uint32(1); uint64(i) <= count; i++ {
		v, e := c.ReadProperty(ctx, target, object, indexed(i))
		if e != nil {
			return out, fmt.Errorf("object-list element %d: %w", i, e)
		}
		id, e := bacnet.AsObjectID(v)
		if e != nil {
			return out, fmt.Errorf("%w: object-list element %d: %v", bacnet.ErrMalformed, i, e)
		}
		out = append(out, id)
	}
	return out, nil
}

// ReadPropertyList reads the Property_List property.
func (c *Client) ReadPropertyList(ctx context.Context, target Target, object bacnet.ObjectIdentifier, maxItems int) ([]bacnet.PropertyIdentifier, error) {
	val, err := c.ReadProperty(ctx, target, object, bacnet.PropertyReference{Identifier: bacnet.PropertyPropertyList})
	if err != nil {
		return nil, err
	}
	return DecodePropertyIdentifierList(val, maxItems, c.limits)
}

// WritePriority writes Present_Value at priority (1–16).
func (c *Client) WritePriority(ctx context.Context, target Target, object bacnet.ObjectIdentifier, priority uint8, value bacnet.ApplicationValue) error {
	if priority < 1 || priority > 16 {
		return fmt.Errorf("%w: priority %d", bacnet.ErrMalformed, priority)
	}
	p := priority
	return c.WriteProperty(ctx, target, object,
		bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		value, &p)
}

// RelinquishPriority writes NULL to Present_Value at priority (1–16).
func (c *Client) RelinquishPriority(ctx context.Context, target Target, object bacnet.ObjectIdentifier, priority uint8) error {
	return c.WritePriority(ctx, target, object, priority, bacnet.NullValue())
}

// DecodeObjectIdentifierList extracts object identifiers from a property value.
func DecodeObjectIdentifierList(val bacnet.ApplicationValue, maxItems int, limits bacnet.DecodeLimits) ([]bacnet.ObjectIdentifier, error) {
	limits = limits.Normalize()
	if maxItems <= 0 {
		maxItems = limits.MaxElements
	}
	if val.Kind == bacnet.ValueObjectID {
		return []bacnet.ObjectIdentifier{val.ObjectID}, nil
	}
	els := val.Elements
	if len(els) == 0 && val.Kind != bacnet.ValueConstructed && val.Kind != bacnet.ValueContext {
		return nil, fmt.Errorf("%w: object-list value kind %d", bacnet.ErrUnsupported, val.Kind)
	}
	if len(els) > maxItems {
		els = els[:maxItems]
	}
	out := make([]bacnet.ObjectIdentifier, 0, len(els))
	for i, el := range els {
		id, err := bacnet.AsObjectID(el.Value)
		if err != nil {
			return nil, fmt.Errorf("%w: object-list element %d: %v", bacnet.ErrMalformed, i, err)
		}
		out = append(out, id)
	}
	return out, nil
}

// DecodePropertyIdentifierList extracts property identifiers from a property value.
func DecodePropertyIdentifierList(val bacnet.ApplicationValue, maxItems int, limits bacnet.DecodeLimits) ([]bacnet.PropertyIdentifier, error) {
	limits = limits.Normalize()
	if maxItems <= 0 {
		maxItems = limits.MaxElements
	}
	if val.Kind == bacnet.ValueEnumerated {
		return []bacnet.PropertyIdentifier{bacnet.PropertyIdentifier(val.Enumerated)}, nil
	}
	els := val.Elements
	if len(els) == 0 && val.Kind != bacnet.ValueConstructed && val.Kind != bacnet.ValueContext {
		return nil, fmt.Errorf("%w: property-list value kind %d", bacnet.ErrUnsupported, val.Kind)
	}
	if len(els) > maxItems {
		els = els[:maxItems]
	}
	out := make([]bacnet.PropertyIdentifier, 0, len(els))
	for i, el := range els {
		n, err := bacnet.AsEnumerated(el.Value)
		if err != nil {
			return nil, fmt.Errorf("%w: property-list element %d: %v", bacnet.ErrMalformed, i, err)
		}
		out = append(out, bacnet.PropertyIdentifier(n))
	}
	return out, nil
}
