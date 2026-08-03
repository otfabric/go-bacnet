// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// ReadObjectList reads BACnetARRAY/LIST of ObjectIdentifier from an object.
// maxItems caps the returned count (0 uses decode MaxElements).
func (c *Client) ReadObjectList(ctx context.Context, target Target, object bacnet.ObjectIdentifier, maxItems int) ([]bacnet.ObjectIdentifier, error) {
	val, err := c.ReadProperty(ctx, target, object, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList})
	if err != nil {
		return nil, err
	}
	return DecodeObjectIdentifierList(val, maxItems, c.limits)
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
