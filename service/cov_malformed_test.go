// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestDecodeSubscribeCOVMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}

	tests := []struct {
		name    string
		build   func(t *testing.T) []byte
		wantErr error
	}{
		{
			name: "duplicate processIdentifier",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 0, 2)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "processIdentifier overflow",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1<<32)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "unexpected tag",
			build: func(t *testing.T) []byte {
				return []byte{0x39, 0x01}
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "incomplete subscription fields",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "duplicate lifetime",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 60)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 30)
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "ParseSequence truncated tag",
			build: func(t *testing.T) []byte {
				return []byte{0x19}
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "ContextUnsigned fail on processIdentifier",
			build: func(t *testing.T) []byte {
				p := []byte{0x08} // context 0, empty unsigned
				var err error
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "ContextObjectID fail on monitoredObject",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x1A, 0x00, 0x01)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "ContextBool bad value",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x29, 0x02)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "lifetime empty unsigned",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x38)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "lifetime overflow",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 1<<32)
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.DecodeSubscribeCOV(tc.build(t), limits)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDecodeSubscribeCOVPropertyMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	tests := []struct {
		name    string
		build   func(t *testing.T) []byte
		wantErr error
	}{
		{
			name: "missing property",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "duplicate monitoredProperty",
			build: func(t *testing.T) []byte {
				enc, err := service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
					SubscribeCOVRequest: service.SubscribeCOVRequest{
						ProcessIdentifier: 1, MonitoredObject: obj, IssueConfirmed: true, Lifetime: 60,
					},
					Property: prop,
				})
				if err != nil {
					t.Fatal(err)
				}
				dup, err := bacnet.AppendContextUnsigned(nil, 4, uint64(prop.Identifier))
				if err != nil {
					t.Fatal(err)
				}
				return append(enc, dup...)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "bad COVIncrement length",
			build: func(t *testing.T) []byte {
				enc, err := service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
					SubscribeCOVRequest: service.SubscribeCOVRequest{
						ProcessIdentifier: 1, MonitoredObject: obj, IssueConfirmed: true, Lifetime: 60,
					},
					Property: prop,
				})
				if err != nil {
					t.Fatal(err)
				}
				return append(enc, 0x55, 0x01)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "property reference missing identifier",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 60)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x4E, 0x4F)
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "ParseSequence truncated tag",
			build: func(t *testing.T) []byte {
				return []byte{0x19}
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "property reference arrayIndex overflow",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 60)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x4E)
				p, err = bacnet.AppendContextUnsigned(p, 0, uint64(prop.Identifier))
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 1, 1<<32)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x4F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "unexpected tag in property reference",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 60)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x4E, 0x59, 0x01, 0x4F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "ContextUnsigned fail in property reference",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 60)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x4E, 0x48, 0x4F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "processIdentifier overflow",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1<<32)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x4E)
				p, err = bacnet.AppendContextUnsigned(p, 0, uint64(prop.Identifier))
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x4F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "duplicate processIdentifier",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 0, 2)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x4E)
				p, err = bacnet.AppendContextUnsigned(p, 0, uint64(prop.Identifier))
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x4F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "duplicate monitoredObject",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x4E)
				p, err = bacnet.AppendContextUnsigned(p, 0, uint64(prop.Identifier))
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x4F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "duplicate issueConfirmed",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, false)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 60)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x4E)
				p, err = bacnet.AppendContextUnsigned(p, 0, uint64(prop.Identifier))
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x4F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "duplicate lifetime",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 60)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 61)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x4E)
				p, err = bacnet.AppendContextUnsigned(p, 0, uint64(prop.Identifier))
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x4F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "lifetime overflow",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 1<<32)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x4E)
				p, err = bacnet.AppendContextUnsigned(p, 0, uint64(prop.Identifier))
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x4F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "incomplete subscription fields",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x4E)
				p, err = bacnet.AppendContextUnsigned(p, 0, uint64(prop.Identifier))
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x4F)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "unexpected tag",
			build: func(t *testing.T) []byte {
				return []byte{0x69, 0x01}
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "ContextUnsigned fail on processIdentifier",
			build: func(t *testing.T) []byte {
				return []byte{0x08} // context tag 0, length 0
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "ContextObjectID fail",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x1A, 0x00, 0x01)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "ContextBool fail",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 2, 2) // not boolean 0/1
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "nested constructed in property reference",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextBool(p, 2, true)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 60)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x4E, 0x0E, 0x0F, 0x4F)
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.DecodeSubscribeCOVProperty(tc.build(t), limits)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v want %v", err, tc.wantErr)
			}
		})
	}
}

func TestDecodeCOVNotificationMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 3}
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}

	tests := []struct {
		name    string
		build   func(t *testing.T) []byte
		wantErr error
	}{
		{
			name: "duplicate initiatingDevice",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, dev)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, dev)
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "timeRemaining overflow",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, dev)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 2, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 1<<32)
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "empty propertyValue wrapper",
			build: func(t *testing.T) []byte {
				note := service.COVNotification{
					ProcessIdentifier: 1,
					InitiatingDevice:  dev,
					MonitoredObject:   obj,
					TimeRemaining:     10,
					Values:            nil,
				}
				p := encodeCOVNotification(t, note)
				p = append(p[:len(p)-1], 0x4E, 0x2E, 0x2F, 0x4F)
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "unexpected value tag",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, dev)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 2, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 10)
				if err != nil {
					t.Fatal(err)
				}
				p = append(p, 0x4E, 0x29, 0x75, 0x39, 0x01, 0x4F)
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "ParseSequence truncated tag",
			build: func(t *testing.T) []byte {
				return []byte{0x19}
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "ContextObjectID fail on initiatingDevice",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x1A, 0x00, 0x01)
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "processIdentifier overflow",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1<<32)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, dev)
				if err != nil {
					t.Fatal(err)
				}
				return p
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "decodeCOVValues unsigned fail",
			build: func(t *testing.T) []byte {
				p, err := bacnet.AppendContextUnsigned(nil, 0, 1)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 1, dev)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextObjectID(p, 2, obj)
				if err != nil {
					t.Fatal(err)
				}
				p, err = bacnet.AppendContextUnsigned(p, 3, 10)
				if err != nil {
					t.Fatal(err)
				}
				return append(p, 0x4E, 0x48, 0x4F)
			},
			wantErr: bacnet.ErrMalformed,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := service.DecodeCOVNotification(tc.build(t), limits)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v want %v", err, tc.wantErr)
			}
		})
	}
}

func TestEncodeSubscribeCOVErrors(t *testing.T) {
	badObj := bacnet.ObjectIdentifier{Type: bacnet.ObjectType(0x400), Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}

	tests := []struct {
		name    string
		encode  func() ([]byte, error)
		wantErr error
	}{
		{
			name: "EncodeSubscribeCOV invalid ObjectID",
			encode: func() ([]byte, error) {
				return service.EncodeSubscribeCOV(service.SubscribeCOVRequest{
					ProcessIdentifier: 1, MonitoredObject: badObj,
					IssueConfirmed: true, Lifetime: 60,
				})
			},
			wantErr: bacnet.ErrMalformed,
		},
		{
			name: "EncodeSubscribeCOVProperty invalid ObjectID",
			encode: func() ([]byte, error) {
				return service.EncodeSubscribeCOVProperty(service.SubscribeCOVPropertyRequest{
					SubscribeCOVRequest: service.SubscribeCOVRequest{
						ProcessIdentifier: 1, MonitoredObject: badObj,
						IssueConfirmed: true, Lifetime: 60,
					},
					Property: prop,
				})
			},
			wantErr: bacnet.ErrMalformed,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tc.encode()
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("got %v want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected encode error: %v", err)
			}
		})
	}
}
