// SPDX-License-Identifier: MIT

// Package device provides optional supervisory helpers built on the client API.
// The client package must not import this package.
package device

import (
	"context"
	"fmt"
	"math"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// PropertyReader is the client surface required by snapshot helpers.
type PropertyReader interface {
	ReadProperty(ctx context.Context, target client.Target, object bacnet.ObjectIdentifier, property bacnet.PropertyReference) (bacnet.ApplicationValue, error)
	ReadPropertyMultiple(ctx context.Context, target client.Target, specs []service.ReadAccessSpecification) ([]service.ReadAccessResult, error)
}

// SnapshotOptions bounds device inspection.
type SnapshotOptions struct {
	MaxObjects    int // default 256
	MaxProperties int // default 64 per object
}

// ObjectSnapshot is one object's inspected properties.
type ObjectSnapshot struct {
	Object     bacnet.ObjectIdentifier
	Properties map[bacnet.PropertyIdentifier]bacnet.ApplicationValue
	Errors     map[bacnet.PropertyIdentifier]error
}

// Snapshot is a bounded device inspection result.
type Snapshot struct {
	Device        bacnet.ObjectIdentifier
	ObjectName    string
	VendorID      uint32
	ProtocolRev   uint32
	MaxAPDU       uint32
	Objects       []ObjectSnapshot
	PartialErrors []error
}

// ReadSnapshot inspects a Device object and a bounded Object_List.
func ReadSnapshot(ctx context.Context, r PropertyReader, target client.Target, deviceInstance uint32, opts SnapshotOptions) (Snapshot, error) {
	if opts.MaxObjects <= 0 {
		opts.MaxObjects = 256
	}
	if opts.MaxProperties <= 0 {
		opts.MaxProperties = 64
	}
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: deviceInstance}
	snap := Snapshot{Device: dev}

	metaSpecs := []service.ReadAccessSpecification{{
		Object: dev,
		Properties: []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName},
			{Identifier: bacnet.PropertyVendorIdentifier},
			{Identifier: bacnet.PropertyProtocolRevision},
			{Identifier: bacnet.PropertyMaxAPDULength},
			{Identifier: bacnet.PropertyObjectList},
		},
	}}
	results, err := r.ReadPropertyMultiple(ctx, target, metaSpecs)
	if err != nil {
		return snap, err
	}
	var objectList bacnet.ApplicationValue
	metaSeen := map[bacnet.PropertyIdentifier]bool{}
	for _, res := range results {
		if !dev.Matches(res.Object) {
			continue // results for an object that was not requested
		}
		for _, pr := range res.Properties {
			// Keep only the requested metadata properties, each once.
			if !requested(metaSpecs[0].Properties, pr.Property.Identifier) || metaSeen[pr.Property.Identifier] {
				continue
			}
			metaSeen[pr.Property.Identifier] = true
			if pr.Err != nil {
				snap.PartialErrors = append(snap.PartialErrors, pr.Err)
				continue
			}
			switch pr.Property.Identifier {
			case bacnet.PropertyObjectName:
				if pr.Value.Kind == bacnet.ValueCharacterString {
					snap.ObjectName = pr.Value.Character.Value
				}
			case bacnet.PropertyVendorIdentifier:
				// Vendor_Identifier is Unsigned16 (clause 12.11).
				if n, e := bacnet.AsUnsigned(pr.Value); e == nil {
					if n > math.MaxUint16 {
						snap.PartialErrors = append(snap.PartialErrors, fmt.Errorf("%w: %s %d exceeds 16 bits", bacnet.ErrMalformed, pr.Property.Identifier, n))
						continue
					}
					snap.VendorID = uint32(n)
				}
			case bacnet.PropertyProtocolRevision:
				if n, e := bacnet.AsUnsigned(pr.Value); e == nil {
					if n > math.MaxUint32 {
						snap.PartialErrors = append(snap.PartialErrors, fmt.Errorf("%w: %s %d exceeds 32 bits", bacnet.ErrMalformed, pr.Property.Identifier, n))
						continue
					}
					snap.ProtocolRev = uint32(n)
				}
			case bacnet.PropertyMaxAPDULength:
				if n, e := bacnet.AsUnsigned(pr.Value); e == nil {
					if n > math.MaxUint32 {
						snap.PartialErrors = append(snap.PartialErrors, fmt.Errorf("%w: %s %d exceeds 32 bits", bacnet.ErrMalformed, pr.Property.Identifier, n))
						continue
					}
					snap.MaxAPDU = uint32(n)
				}
			case bacnet.PropertyObjectList:
				objectList = pr.Value
			default:
				// Ignore unexpected properties in the metadata RPM result.
			}
		}
	}
	ids, err := client.DecodeObjectIdentifierList(objectList, opts.MaxObjects, bacnet.DefaultDecodeLimits())
	if err != nil {
		return snap, fmt.Errorf("object-list: %w", err)
	}
	for _, id := range ids {
		os := ObjectSnapshot{
			Object:     id,
			Properties: map[bacnet.PropertyIdentifier]bacnet.ApplicationValue{},
			Errors:     map[bacnet.PropertyIdentifier]error{},
		}
		props := []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName},
			{Identifier: bacnet.PropertyObjectType},
			{Identifier: bacnet.PropertyPresentValue},
			{Identifier: bacnet.PropertyStatusFlags},
		}
		if len(props) > opts.MaxProperties {
			props = props[:opts.MaxProperties]
		}
		ores, err := r.ReadPropertyMultiple(ctx, target, []service.ReadAccessSpecification{{Object: id, Properties: props}})
		if err != nil {
			snap.PartialErrors = append(snap.PartialErrors, err)
			snap.Objects = append(snap.Objects, os)
			continue
		}
		for _, res := range ores {
			if !id.Matches(res.Object) {
				continue // results for an object that was not requested
			}
			for _, pr := range res.Properties {
				// Store only requested properties: at most len(props),
				// which is at most MaxProperties.
				if !requested(props, pr.Property.Identifier) {
					continue
				}
				if _, dup := os.Properties[pr.Property.Identifier]; dup {
					continue
				}
				if _, dup := os.Errors[pr.Property.Identifier]; dup {
					continue
				}
				if pr.Err != nil {
					os.Errors[pr.Property.Identifier] = pr.Err
					continue
				}
				os.Properties[pr.Property.Identifier] = pr.Value
			}
		}
		snap.Objects = append(snap.Objects, os)
	}
	return snap, nil
}

func requested(refs []bacnet.PropertyReference, id bacnet.PropertyIdentifier) bool {
	for _, r := range refs {
		if r.Identifier == id {
			return true
		}
	}
	return false
}
