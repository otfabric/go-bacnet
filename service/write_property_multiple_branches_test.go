// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestWritePropertyMultipleDecodeBranches(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}

	// arrayIndex without property
	body, err := bacnet.AppendContextUnsigned(nil, 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 1, mustParseEls(t, body))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("arrayIndex: %v", err)
	}

	// propertyValue without property
	valBody, err := bacnet.AppendContextTagged(nil, 2, []bacnet.Element{
		{Value: bacnet.RealValue(1)},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 1, mustParseEls(t, valBody))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("value without prop: %v", err)
	}

	// priority without value
	prioOnly, err := bacnet.AppendContextUnsigned(nil, 0, 85)
	if err != nil {
		t.Fatal(err)
	}
	prioOnly, err = bacnet.AppendContextUnsigned(prioOnly, 3, 8)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 1, mustParseEls(t, prioOnly))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("priority: %v", err)
	}

	// empty propertyValue wrapper
	prop, err := bacnet.AppendContextUnsigned(nil, 0, 85)
	if err != nil {
		t.Fatal(err)
	}
	prop, err = bacnet.AppendContextTagged(prop, 2, nil)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 1, mustParseEls(t, prop))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty value: %v", err)
	}

	// priority out of range
	good, err := bacnet.AppendContextUnsigned(nil, 0, 85)
	if err != nil {
		t.Fatal(err)
	}
	good, err = bacnet.AppendContextTagged(good, 2, []bacnet.Element{{Value: bacnet.RealValue(1)}})
	if err != nil {
		t.Fatal(err)
	}
	good, err = bacnet.AppendContextUnsigned(good, 3, 17)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 1, mustParseEls(t, good))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("prio range: %v", err)
	}

	// trailing data / empty / properties-without-object / unexpected tag
	goodSpecs, err := service.EncodeWritePropertyMultiple([]service.WriteAccessSpecification{{
		Object: obj,
		Properties: []service.WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(1),
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(append(append([]byte(nil), goodSpecs...), 0x29, 0x01), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("extra tag: %v", err)
	}
	if _, err := service.DecodeWritePropertyMultiple(nil, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("empty: %v", err)
	}
	propsOnly, err := bacnet.AppendContextTagged(nil, 1, []bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(85)},
		{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.RealValue(1)}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(propsOnly, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("props without object: %v", err)
	}
	if _, err := service.DecodeWritePropertyMultiple([]byte{0x29, 0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected: %v", err)
	}

	// object without properties
	objOnly, err := bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(objOnly, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("no properties: %v", err)
	}

	// duplicate propertyValue / constructed multi-element value / priority kind
	dupVal, err := bacnet.AppendContextUnsigned(nil, 0, 85)
	if err != nil {
		t.Fatal(err)
	}
	dupVal, err = bacnet.AppendContextTagged(dupVal, 2, []bacnet.Element{{Value: bacnet.RealValue(1)}})
	if err != nil {
		t.Fatal(err)
	}
	dupVal, err = bacnet.AppendContextTagged(dupVal, 2, []bacnet.Element{{Value: bacnet.RealValue(2)}})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 1, mustParseEls(t, dupVal))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup value: %v", err)
	}

	multi, err := bacnet.AppendContextUnsigned(nil, 0, 85)
	if err != nil {
		t.Fatal(err)
	}
	multi, err = bacnet.AppendContextTagged(multi, 2, []bacnet.Element{
		{Value: bacnet.UnsignedValue(1)},
		{Value: bacnet.UnsignedValue(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 1, mustParseEls(t, multi))
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeWritePropertyMultiple(raw, limits)
	if err != nil || len(got) != 1 || got[0].Properties[0].Value.Kind != bacnet.ValueConstructed {
		t.Fatalf("constructed value: %+v %v", got, err)
	}

	prioKind, err := bacnet.AppendContextUnsigned(nil, 0, 85)
	if err != nil {
		t.Fatal(err)
	}
	prioKind, err = bacnet.AppendContextTagged(prioKind, 2, []bacnet.Element{{Value: bacnet.RealValue(1)}})
	if err != nil {
		t.Fatal(err)
	}
	prioKind = append(prioKind, 0x38) // context tag 3, length 0
	raw, err = bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 1, mustParseEls(t, prioKind))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("prio kind: %v", err)
	}

	// flush missing value after property id only
	propOnly, err := bacnet.AppendContextUnsigned(nil, 0, 85)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 1, mustParseEls(t, propOnly))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("prop without value: %v", err)
	}

	// unexpected WritePropertyValue tag
	weird, err := bacnet.AppendContextUnsigned(nil, 0, 85)
	if err != nil {
		t.Fatal(err)
	}
	weird, err = bacnet.AppendContextUnsigned(weird, 4, 1)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextObjectID(nil, 0, obj)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = bacnet.AppendContextTagged(raw, 1, mustParseEls(t, weird))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeWritePropertyMultiple(raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected prop tag: %v", err)
	}
}

func TestWritePropertyMultipleErrorDecodeBranches(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	e := service.WritePropertyMultipleError{
		Class: 2, Code: 32,
		FirstFailed:   bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1},
		FirstProperty: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: uint32Ptr(1)},
	}
	raw, err := service.EncodeWritePropertyMultipleError(e)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeWritePropertyMultipleError(raw, limits)
	if err != nil || got.FirstProperty.ArrayIndex == nil || *got.FirstProperty.ArrayIndex != 1 {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := service.DecodeWritePropertyMultipleError(append(append([]byte(nil), raw...), 0x29, 0x01), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("extra tag: %v", err)
	}
	if _, err := service.DecodeWritePropertyMultipleError([]byte{0x29, 0x01}, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("unexpected: %v", err)
	}
	// duplicate errorType
	dup := append(append([]byte(nil), raw[:len(raw)/2]...), raw...)
	_ = dup
	errType, err := bacnet.EncodeBACnetError(nil, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	body := append([]byte{0x0E}, errType...)
	body = append(body, 0x0F)
	body = append(body, 0x0E)
	body = append(body, errType...)
	body = append(body, 0x0F)
	if _, err := service.DecodeWritePropertyMultipleError(body, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup errorType: %v", err)
	}
	// missing firstFailed
	onlyErr := append([]byte{0x0E}, errType...)
	onlyErr = append(onlyErr, 0x0F)
	if _, err := service.DecodeWritePropertyMultipleError(onlyErr, limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("missing first: %v", err)
	}
	// bad object in firstFailedWriteAttempt
	badFirst := append([]byte{0x0E}, errType...)
	badFirst = append(badFirst, 0x0F, 0x1E, 0x0A, 0x00, 0x01, 0x1F) // truncated object id
	if _, err := service.DecodeWritePropertyMultipleError(badFirst, limits); err == nil {
		t.Fatal("expected bad firstFailed")
	}
	bad := bacnet.ObjectIdentifier{Type: 0x400, Instance: 0}
	if _, err := service.EncodeWritePropertyMultipleError(service.WritePropertyMultipleError{
		FirstFailed: bad, FirstProperty: bacnet.PropertyReference{Identifier: 85},
	}); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("bad encode object: %v", err)
	}
}

func uint32Ptr(v uint32) *uint32 { return &v }

func mustParseEls(t *testing.T, raw []byte) []bacnet.Element {
	t.Helper()
	els, n, err := bacnet.ParseSequence(raw, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(raw) {
		t.Fatalf("parse: %v n=%d", err, n)
	}
	return els
}

func TestGetEventInformationACKDuplicates(t *testing.T) {
	empty := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}
	ack := service.GetEventInformationACK{
		Summaries: []service.EventSummary{{
			Object: bacnet.ObjectIdentifier{Type: 2, Instance: 1}, EventState: 0,
			AcknowledgedTransitions: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}},
			EventTimeStamps:         empty, NotifyType: 0,
			EventEnable: bacnet.BitString{UnusedBits: 5, Bytes: []byte{0}}, EventPriorities: empty,
		}},
		MoreEvents: false,
	}
	raw, err := service.EncodeGetEventInformationACK(ack)
	if err != nil {
		t.Fatal(err)
	}
	// duplicate moreEvents
	dup, err := bacnet.AppendContextBool(raw, 1, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetEventInformationACK(dup, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("%v", err)
	}
	// Two empty lists before moreEvents is malformed (duplicate listOfEventSummaries).
	two := []byte{0x0E, 0x0F, 0x0E, 0x0F}
	two, err = bacnet.AppendContextBool(two, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeGetEventInformationACK(two, bacnet.DefaultDecodeLimits()); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("dup list: %v", err)
	}
}
