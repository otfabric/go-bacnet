//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// A WritePropertyMultiple whose second or third write fails.
//
// Every request writes the Analog Value's Present_Value first (valid), then
// the failing element, then the Binary Value's Present_Value (valid). The
// tests assert, per peer:
//
//   - the error: a *service.WritePropertyMultipleError naming the first failed
//     element where the peer sends the WritePropertyMultiple-Error structure,
//     a plain *bacnet.ErrorResponse where it sends an ordinary Error;
//   - that errors.As(err, **bacnet.ErrorResponse) matches in both cases
//     (WritePropertyMultipleError unwraps to it);
//   - by reading back: the write before the failing one is applied and the
//     write after it is not. No peer rolls back, no peer continues past the
//     failure.
//
// bacnet-stack and BACnet4J execute the service themselves. On BACpypes3 and
// Worldiety the handler is adapter code (BACpypes3 raises NotImplementedError
// for WritePropertyMultiple; Worldiety has no object model), so those two say
// what the adapter does over the stack's codec, not what a stack does.
type wpmFailure struct {
	name string
	// The failing element: a property of the Analog Value written after its
	// Present_Value, or (unknownObject) a write to an object that does not
	// exist, between the two valid writes.
	property      bacnet.PropertyIdentifier
	value         bacnet.ApplicationValue
	unknownObject bool

	class, code uint16
	// plainError: the peer answers with an ordinary Error, so the failed
	// element is not known to the client.
	plainError bool
}

var wpmMissingObject = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 4000}

func runWritePropertyMultipleFirstFailed(t *testing.T, p interopPeer, cases []wpmFailure) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, p.image, p.name)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()

	av, baseline := analogValueObject(dev)
	bv := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)
	readAV := func() float32 {
		t.Helper()
		v, err := c.ReadProperty(ctx, peer.target, av, pv)
		if err != nil {
			t.Fatalf("read %v present-value: %v", av, err)
		}
		f, err := bacnet.AsReal(v)
		if err != nil {
			t.Fatal(err)
		}
		return f
	}
	readBV := func() uint32 {
		t.Helper()
		v, err := c.ReadProperty(ctx, peer.target, bv, pv)
		if err != nil {
			t.Fatalf("read %v present-value: %v", bv, err)
		}
		if v.Kind != bacnet.ValueEnumerated {
			t.Fatalf("%v present-value is %v, want an enumeration", bv, v.Kind)
		}
		return v.Enumerated
	}
	if got := readBV(); got != 0 {
		t.Fatalf("%v present-value = %d before the test, want inactive (0)", bv, got)
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			written := baseline + float32(i) + 1.5
			first := service.WritePropertyValue{Property: pv, Value: bacnet.RealValue(written), Priority: &prio}
			last := service.WriteAccessSpecification{Object: bv, Properties: []service.WritePropertyValue{
				{Property: pv, Value: bacnet.EnumValue(1), Priority: &prio}}}
			var specs []service.WriteAccessSpecification
			wantObject, wantProperty := av, tc.property
			if tc.unknownObject {
				wantObject, wantProperty = wpmMissingObject, bacnet.PropertyPresentValue
				specs = []service.WriteAccessSpecification{
					{Object: av, Properties: []service.WritePropertyValue{first}},
					{Object: wpmMissingObject, Properties: []service.WritePropertyValue{
						{Property: pv, Value: bacnet.RealValue(1), Priority: &prio}}},
					last,
				}
			} else {
				specs = []service.WriteAccessSpecification{
					{Object: av, Properties: []service.WritePropertyValue{first,
						{Property: bacnet.PropertyReference{Identifier: tc.property}, Value: tc.value}}},
					last,
				}
			}

			err := c.WritePropertyMultiple(ctx, peer.target, specs)
			if err == nil {
				t.Fatal("WritePropertyMultiple succeeded, want the failing element refused")
			}
			var unknown *bacnet.OutcomeUnknownError
			if errors.As(err, &unknown) {
				t.Fatalf("a definite Error reported as outcome-unknown: %v", err)
			}
			var er *bacnet.ErrorResponse
			if !errors.As(err, &er) {
				t.Fatalf("got %T %v, want an error matching *bacnet.ErrorResponse", err, err)
			}
			if er.Class != tc.class || er.Code != tc.code {
				t.Fatalf("error %s/%s, want %s/%s", bacnet.ErrorClassName(er.Class), bacnet.ErrorCodeName(er.Code),
					bacnet.ErrorClassName(tc.class), bacnet.ErrorCodeName(tc.code))
			}
			var we *service.WritePropertyMultipleError
			switch isWPM := errors.As(err, &we); {
			case tc.plainError && isWPM:
				t.Fatalf("peer now sends a WritePropertyMultiple-Error (first failed %v property %d); it used to send a plain Error",
					we.FirstFailed, we.FirstProperty.Identifier)
			case tc.plainError:
				// Nothing more to learn from the reply.
			case !isWPM:
				t.Fatalf("got %T %v, want *service.WritePropertyMultipleError", err, err)
			default:
				if we.Class != tc.class || we.Code != tc.code {
					t.Fatalf("WritePropertyMultipleError %d/%d differs from the unwrapped ErrorResponse %d/%d",
						we.Class, we.Code, er.Class, er.Code)
				}
				if we.FirstFailed != wantObject || we.FirstProperty.Identifier != wantProperty || we.FirstProperty.ArrayIndex != nil {
					t.Fatalf("first failed write %v property %d index %v, want %v property %d",
						we.FirstFailed, we.FirstProperty.Identifier, we.FirstProperty.ArrayIndex, wantObject, wantProperty)
				}
			}

			// The write before the failing one is applied, the one after it is not.
			if got := readAV(); math.Abs(float64(got-written)) > 0.01 {
				t.Fatalf("%v present-value = %v after the failed request, want %v (the write before the failing one applied)",
					av, got, written)
			}
			if got := readBV(); got != 0 {
				t.Fatalf("%v present-value = %d: the write after the failing one was executed", bv, got)
			}

			restore := []service.WriteAccessSpecification{{Object: av, Properties: []service.WritePropertyValue{
				{Property: pv, Value: bacnet.RealValue(baseline), Priority: &prio}}}}
			if err := c.WritePropertyMultiple(ctx, peer.target, restore); err != nil {
				t.Fatalf("restore: %v", err)
			}
			if got := readAV(); math.Abs(float64(got-baseline)) > 0.01 {
				t.Fatalf("%v present-value = %v after the restore, want %v", av, got, baseline)
			}
		})
	}
}

func TestBacnetStackWritePropertyMultipleFirstFailed(t *testing.T) {
	runWritePropertyMultipleFirstFailed(t, stackPeer(), []wpmFailure{
		{name: "unknown property", property: unknownPropertyID, value: bacnet.UnsignedValue(1),
			class: bacnet.ErrorClassProperty, code: bacnet.ErrorCodeUnknownProperty},
		{name: "read-only property", property: bacnet.PropertyObjectType, value: bacnet.EnumValue(uint32(bacnet.ObjectTypeBinaryValue)),
			class: bacnet.ErrorClassProperty, code: bacnet.ErrorCodeWriteAccessDenied},
		{name: "wrong datatype", property: bacnet.PropertyPresentValue, value: bacnet.BoolValue(true),
			class: bacnet.ErrorClassProperty, code: bacnet.ErrorCodeInvalidDataType},
		{name: "unknown object", unknownObject: true,
			class: bacnet.ErrorClassObject, code: bacnet.ErrorCodeUnknownObject},
	})
}

// BACnet4J names the class of an unknown object "property" (the other peers:
// "object"). An unknown property is no failure on BACnet4J: see
// TestBACnet4JWritePropertyMultipleUnknownPropertyAccepted.
func TestBACnet4JWritePropertyMultipleFirstFailed(t *testing.T) {
	runWritePropertyMultipleFirstFailed(t, bacnet4jPeer(), []wpmFailure{
		{name: "read-only property", property: bacnet.PropertyObjectType, value: bacnet.EnumValue(uint32(bacnet.ObjectTypeBinaryValue)),
			class: bacnet.ErrorClassProperty, code: bacnet.ErrorCodeWriteAccessDenied},
		{name: "unknown object", unknownObject: true,
			class: bacnet.ErrorClassProperty, code: bacnet.ErrorCodeUnknownObject},
	})
}

// Adapter shim: the BACpypes3 adapter's handler loops write_property and lets
// the first exception end the request, which the stack turns into an ordinary
// Error. The client therefore gets a *bacnet.ErrorResponse and cannot tell
// which write failed. BACpypes3 accepts a write to Object_Type, so the failing
// element here is an object that does not exist.
func TestBACpypes3WritePropertyMultipleFirstFailed(t *testing.T) {
	runWritePropertyMultipleFirstFailed(t, bacpypes3Peer(), []wpmFailure{
		{name: "unknown object", unknownObject: true, plainError: true,
			class: bacnet.ErrorClassObject, code: bacnet.ErrorCodeUnknownObject},
	})
}

// Adapter shim: the Worldiety adapter's handler and its
// WritePropertyMultiple-Error encoding are adapter code. Up to bacnet-interop
// 0.9.0 it nested firstFailedWriteAttempt wrongly; go-bacnet then fell back to
// the class and code alone.
func TestWorldietyWritePropertyMultipleFirstFailed(t *testing.T) {
	requireAdapterVersion(t, worldietyPeer().image, "0.10.0")
	runWritePropertyMultipleFirstFailed(t, worldietyPeer(), []wpmFailure{
		{name: "unknown property", property: unknownPropertyID, value: bacnet.UnsignedValue(1),
			class: bacnet.ErrorClassProperty, code: bacnet.ErrorCodeWriteAccessDenied},
		{name: "read-only property", property: bacnet.PropertyObjectType, value: bacnet.EnumValue(uint32(bacnet.ObjectTypeBinaryValue)),
			class: bacnet.ErrorClassProperty, code: bacnet.ErrorCodeWriteAccessDenied},
		{name: "wrong datatype", property: bacnet.PropertyPresentValue, value: bacnet.BoolValue(true),
			class: bacnet.ErrorClassProperty, code: bacnet.ErrorCodeValueOutOfRange},
		{name: "unknown object", unknownObject: true,
			class: bacnet.ErrorClassObject, code: bacnet.ErrorCodeUnknownObject},
	})
}

// BACnet4J accepts a write to a property the object does not have: the
// request is acknowledged, the writes after it are executed, and the property
// then reads back.
func TestBACnet4JWritePropertyMultipleUnknownPropertyAccepted(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jPeer().image, bacnet4jPeer().name)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	bv := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	unknown := bacnet.PropertyReference{Identifier: unknownPropertyID}
	prio := uint8(8)

	if _, err := c.ReadProperty(ctx, peer.target, av, unknown); err == nil {
		t.Fatalf("%v already has property %d", av, unknownPropertyID)
	} else {
		assertErrorUnknownProperty(t, err)
	}
	err := c.WritePropertyMultiple(ctx, peer.target, []service.WriteAccessSpecification{
		{Object: av, Properties: []service.WritePropertyValue{{Property: unknown, Value: bacnet.UnsignedValue(7)}}},
		{Object: bv, Properties: []service.WritePropertyValue{{Property: pv, Value: bacnet.EnumValue(1), Priority: &prio}}},
	})
	if err != nil {
		t.Fatalf("WritePropertyMultiple with an unknown property: %v (BACnet4J used to accept it)", err)
	}
	v, err := c.ReadProperty(ctx, peer.target, bv, pv)
	if err != nil || v.Enumerated != 1 {
		t.Fatalf("%v present-value = %+v (%v), want active: the write after the unknown property", bv, v, err)
	}
	v, err = c.ReadProperty(ctx, peer.target, av, unknown)
	if err != nil {
		t.Fatalf("read back property %d: %v", unknownPropertyID, err)
	}
	if got, err := bacnet.AsUnsigned(v); err != nil || got != 7 {
		t.Fatalf("property %d reads back %+v (%v), want Unsigned 7", unknownPropertyID, v, err)
	}
}

// A WritePropertyMultiple that carries a value of the wrong datatype for a
// property BACnet4J knows (here BOOLEAN for the REAL Present_Value) fails
// while BACnet4J decodes the request (ArrayIndexOutOfBoundsException under
// PropertyValue). It answers with an ordinary Error services/
// operational-problem whose error-choice is 127 instead of the request's
// service choice 16, not with a WritePropertyMultiple-Error: the client gets a
// definite *bacnet.ErrorResponse without a first failed element. No write of
// the request is executed, not even the one encoded before the bad value.
func TestBACnet4JWritePropertyMultipleWrongDatatypeError(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, bacnet4jPeer().image, bacnet4jPeer().name)
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithTransactionOptions(2*time.Second, 0, time.Second))
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	av, baseline := analogValueObject(dev)
	bv := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prio := uint8(8)

	err := c.WritePropertyMultiple(ctx, peer.target, []service.WriteAccessSpecification{
		{Object: bv, Properties: []service.WritePropertyValue{{Property: pv, Value: bacnet.EnumValue(1), Priority: &prio}}},
		{Object: av, Properties: []service.WritePropertyValue{{Property: pv, Value: bacnet.BoolValue(true), Priority: &prio}}},
	})
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		t.Fatalf("a definite Error reported as outcome-unknown: %v", err)
	}
	var we *service.WritePropertyMultipleError
	if errors.As(err, &we) {
		t.Fatalf("got a WritePropertyMultiple-Error (%v); BACnet4J used to send a plain Error here", err)
	}
	var er *bacnet.ErrorResponse
	if !errors.As(err, &er) || er.Class != bacnet.ErrorClassServices || er.Code != bacnet.ErrorCodeOperationalProblem {
		t.Fatalf("got %T %v, want Error services/operational-problem", err, err)
	}
	if er.Service != 127 {
		t.Fatalf("error-choice %d, BACnet4J used to send 127", er.Service)
	}
	v, err := c.ReadProperty(ctx, peer.target, bv, pv)
	if err != nil || v.Enumerated != 0 {
		t.Fatalf("%v present-value = %+v (%v), want inactive: the request was dropped whole", bv, v, err)
	}
	v, err = c.ReadProperty(ctx, peer.target, av, pv)
	if f, e := bacnet.AsReal(v); err != nil || e != nil || math.Abs(float64(f-baseline)) > 0.01 {
		t.Fatalf("%v present-value = %+v (%v), want %v", av, v, err, baseline)
	}
}
