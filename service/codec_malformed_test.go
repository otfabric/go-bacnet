// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestDecodeReadPropertyMalformedBranches(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}

	overflowID, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	overflowID, err = bacnet.AppendContextUnsigned(overflowID, 1, 1<<32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeReadProperty(overflowID, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("property overflow: %v", err)
	}

	dupIndex, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	dupIndex, err = bacnet.AppendContextUnsigned(dupIndex, 1, 75)
	if err != nil {
		t.Fatal(err)
	}
	dupIndex, err = bacnet.AppendContextUnsigned(dupIndex, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	dupIndex, err = bacnet.AppendContextUnsigned(dupIndex, 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeReadProperty(dupIndex, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate index: %v", err)
	}

	if _, err := service.DecodeReadProperty([]byte{0x39, 0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected tag: %v", err)
	}
}

func TestDecodeWritePropertyMalformedBranches(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	dupValue, err := service.EncodeWriteProperty(service.WritePropertyRequest{
		Object: obj, Property: prop, Value: bacnet.RealValue(1.0),
	})
	if err != nil {
		t.Fatal(err)
	}
	dupValue = append(dupValue, 0x3E, 0x44, 0x3F)
	if _, err := service.DecodeWriteProperty(dupValue, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate value: %v", err)
	}

	badPriority, err := service.EncodeWriteProperty(service.WritePropertyRequest{
		Object: obj, Property: prop, Value: bacnet.RealValue(1.0),
	})
	if err != nil {
		t.Fatal(err)
	}
	badPriority, err = bacnet.AppendContextUnsigned(badPriority, 4, 17)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWriteProperty(badPriority, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("priority range: %v", err)
	}

	emptyValue, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	emptyValue, err = bacnet.AppendContextUnsigned(emptyValue, 1, uint64(prop.Identifier))
	if err != nil {
		t.Fatal(err)
	}
	emptyValue = append(emptyValue, 0x3E, 0x3F)
	if _, err := service.DecodeWriteProperty(emptyValue, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty value wrapper: %v", err)
	}

	prio := uint8(8)
	nullRelinquish, err := service.EncodeWriteProperty(service.WritePropertyRequest{
		Object: obj, Property: prop, Value: bacnet.NullValue(), Priority: &prio,
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeWriteProperty(nullRelinquish, limits)
	if err != nil || dec.Value.Kind != bacnet.ValueNull || dec.Priority == nil || *dec.Priority != 8 {
		t.Fatalf("null relinquish: err=%v dec=%+v", err, dec)
	}
}

func TestDecodeRPMACKMalformedBranches(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	p, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	p = append(p, 0x29, 0x01) // unexpected context tag after object
	if _, err := service.DecodeReadPropertyMultipleACK(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected tag after object: %v", err)
	}

	// Property list without preceding object.
	raw := []byte{0x1E, 0x29, 0x75, 0x1F}
	if _, err := service.DecodeReadPropertyMultipleACK(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("results without object: %v", err)
	}

	// Property identifier without value/error outcome before next property.
	obj2 := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 2}
	p2, err := bacnet.AppendContextObjectID(nil, 0, obj2)
	if err != nil {
		t.Fatal(err)
	}
	p2 = append(p2, 0x1E)
	p2, err = bacnet.AppendContextUnsigned(p2, 2, uint64(bacnet.PropertyObjectName))
	if err != nil {
		t.Fatal(err)
	}
	p2, err = bacnet.AppendContextUnsigned(p2, 2, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	p2 = append(p2, 0x1F)
	if _, err := service.DecodeReadPropertyMultipleACK(p2, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("missing outcome: %v", err)
	}
}

func TestDiscoveryEncodeDecodeEdgeCases(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()

	if _, err := service.EncodeWhoIs(service.WhoIs{LowLimit: ptrU32(1)}); err == nil {
		t.Fatal("EncodeWhoIs partial limits should fail")
	}

	low, high := uint32(100), uint32(1)
	if _, err := service.DecodeWhoIs(mustWhoIsPayload(t, low, high), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("low>high: %v", err)
	}

	lowOnly, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWhoIs(lowOnly, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("partial limits: %v", err)
	}

	iam := service.IAm{
		Device:        bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MaxAPDULength: 480, Segmentation: 0, VendorID: 1,
	}
	p, err := service.EncodeIAm(iam)
	if err != nil {
		t.Fatal(err)
	}
	p = append(p, 0xFF)
	if _, err := service.DecodeIAm(p, limits); !errors.Is(err, bacnet.ErrTrailingData) {
		t.Fatalf("I-Am trailing: %v", err)
	}
}

func TestDecodeReadPropertyACKMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	dupObj, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	dupObj, err = bacnet.AppendContextObjectID(dupObj, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	dupObj, err = bacnet.AppendContextUnsigned(dupObj, 1, uint64(prop.Identifier))
	if err != nil {
		t.Fatal(err)
	}
	dupObj = append(dupObj, 0x3E, 0x3F)
	if _, err := service.DecodeReadPropertyACK(dupObj, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate object: %v", err)
	}

	emptyValue, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	emptyValue, err = bacnet.AppendContextUnsigned(emptyValue, 1, uint64(prop.Identifier))
	if err != nil {
		t.Fatal(err)
	}
	emptyValue = append(emptyValue, 0x3E, 0x3F)
	if _, err := service.DecodeReadPropertyACK(emptyValue, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty value: %v", err)
	}
}

func TestDecodeWritePropertyMissingFields(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	onlyObj, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWriteProperty(onlyObj, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("missing fields: %v", err)
	}

	if _, err := service.DecodeWriteProperty([]byte{0x39, 0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected tag: %v", err)
	}
}

func TestEncodeWritePropertyBadInputs(t *testing.T) {
	badObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectType(0x400), Instance: 1}
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	if _, err := service.EncodeWriteProperty(service.WritePropertyRequest{
		Object: badObj, Property: prop, Value: bacnet.RealValue(1.0),
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad object: %v", err)
	}
	if _, err := service.EncodeWriteProperty(service.WritePropertyRequest{
		Object: obj, Property: prop,
		Value: bacnet.ApplicationValue{Kind: bacnet.ValueKind(200)},
	}); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("bad value: %v", err)
	}
}

func TestDecodeWritePropertyMalformedExtra(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	tests := []struct {
		name    string
		build   func(t *testing.T) []byte
		wantErr error
	}{
		{
			name:    "ParseSequence truncated",
			build:   func(t *testing.T) []byte { return []byte{0x19} },
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "duplicate propertyIdentifier",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextObjectID(nil, 0, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 1, uint64(prop.Identifier))
				if err != nil {
					t.Fatal(err)
				}
				dup, err := bacnet.AppendContextUnsigned(p, 1, uint64(bacnet.PropertyObjectName))
				if err != nil {
					t.Fatal(err)
				}
				return dup
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name:    "ParseSequence truncated",
			build:   func(t *testing.T) []byte { return []byte{0x19} },
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "priority out of range",
			build: func(t *testing.T) []byte {
				enc, err := service.EncodeWriteProperty(service.WritePropertyRequest{
					Object: obj, Property: prop, Value: bacnet.RealValue(1.0),
				})
				if err != nil {
					t.Fatal(err)
				}
				prio, err := bacnet.AppendContextUnsigned(enc, 4, 0)
				if err != nil {
					t.Fatal(err)
				}
				return prio
			},
			wantErr: bacnet.ErrMalformed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.DecodeWriteProperty(tc.build(t), limits)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDecodeWritePropertyConstructedMultiValue(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogOutput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	val := bacnet.ApplicationValue{
		Kind: bacnet.ValueConstructed,
		Elements: []bacnet.Element{
			{Value: bacnet.RealValue(1.0)},
			{Value: bacnet.UnsignedValue(2)},
		},
	}
	enc, err := service.EncodeWriteProperty(service.WritePropertyRequest{
		Object: obj, Property: prop, Value: val,
	})
	if err != nil {
		t.Fatal(err)
	}
	dec, err := service.DecodeWriteProperty(enc, limits)
	if err != nil || dec.Value.Kind != bacnet.ValueConstructed || len(dec.Value.Elements) != 2 {
		t.Fatalf("constructed multi-value: err=%v dec=%+v", err, dec.Value)
	}
}

func TestEncodeReadPropertyBadObjectID(t *testing.T) {
	badObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectType(0x400), Instance: 1}
	if _, err := service.EncodeReadProperty(service.ReadPropertyRequest{
		Object:   badObj,
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad object: %v", err)
	}
}

func TestDecodeReadPropertyArrayIndexOverflow(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	p, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextUnsigned(p, 1, uint64(bacnet.PropertyPresentValue))
	if err != nil {
		t.Fatal(err)
	}
	p, err = bacnet.AppendContextUnsigned(p, 2, 1<<32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeReadProperty(p, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("arrayIndex overflow: %v", err)
	}
}

func TestEncodeReadPropertyMultipleErrors(t *testing.T) {
	badObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectType(0x400), Instance: 1}
	if _, err := service.EncodeReadPropertyMultiple([]service.ReadAccessSpecification{{
		Object:     badObj,
		Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyObjectName}},
	}}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("RPM bad object: %v", err)
	}

	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	if _, err := service.EncodeReadPropertyMultipleACK([]service.ReadAccessResult{{
		Object: obj,
		Properties: []service.PropertyResult{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
			Err:      errors.New("not ErrorResponse"),
		}},
	}}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("RPM ACK bad Err type: %v", err)
	}
	if _, err := service.EncodeReadPropertyMultipleACK([]service.ReadAccessResult{{
		Object: obj,
		Properties: []service.PropertyResult{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
			Value:    bacnet.ApplicationValue{Kind: bacnet.ValueKind(200)},
		}},
	}}); !errors.Is(err, bacnet.ErrUnsupported) {
		t.Fatalf("RPM ACK unsupported value: %v", err)
	}
}

func TestDecodeRPMACKMalformedExtra(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}

	tests := []struct {
		name    string
		build   func(t *testing.T) []byte
		wantErr error
	}{
		{
			name: "ContextObjectID fail",
			build: func(t *testing.T) []byte {
				return []byte{0x1A, 0x00, 0x01}
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "empty propertyValue wrapper",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextObjectID(nil, 0, obj)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x1E, 0x29, 0x75, 0x4E, 0x4F, 0x1F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "propertyValue without property",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextObjectID(nil, 0, obj)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x1E, 0x4E, 0x21, 0x01, 0x4F, 0x1F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "propertyAccessError without property",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextObjectID(nil, 0, obj)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x1E, 0x5E, 0x09, 0x01, 0x19, 0x01, 0x5F, 0x1F)
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "unexpected property tag",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextObjectID(nil, 0, obj)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x1E, 0x39, 0x01, 0x1F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "final flush missing outcome",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextObjectID(nil, 0, obj)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x1E)
				p, err = bacnet.AppendContextUnsigned(p, 2, uint64(bacnet.PropertyObjectName))
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x1F)
			},
			wantErr: bacnet.ErrMalformed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.DecodeReadPropertyMultipleACK(tc.build(t), limits)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v want %v", err, tc.wantErr)
			}
		})
	}
}

func ptrU32(v uint32) *uint32 { return &v }

func mustWhoIsPayload(t *testing.T, low, high uint32) []byte {
	t.Helper()
	p, err := service.EncodeWhoIs(service.WhoIs{LowLimit: &low, HighLimit: &high})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
