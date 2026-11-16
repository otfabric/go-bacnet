// SPDX-License-Identifier: MIT

package service_test

import (
	"bytes"
	"errors"
	"reflect"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// The decoders and encoders of these messages are inverse: what one
// decodes the other writes back, and what one writes the other reads back.

// AuthRequest: the parameters are the elements inside the request's [0].
func TestAuthRequestCodecInverse(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	for _, tc := range []struct {
		name   string
		params []bacnet.Element
		wire   string
	}{
		{name: "one context element", params: []bacnet.Element{{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{1}}}}, wire: "0e09010f"},
		{name: "application elements", params: []bacnet.Element{{TagNumber: 2, Value: bacnet.UnsignedValue(9)}, {TagNumber: 1, Value: bacnet.BoolValue(true)}}, wire: "0e2109110f"},
		{name: "a constructed element", params: []bacnet.Element{{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{TagNumber: 2, Value: bacnet.UnsignedValue(2)}}}}}, wire: "0e1e21021f0f"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := fromHex(tc.wire)
			enc, err := service.EncodeAuthRequest(service.AuthRequest{Parameters: tc.params})
			if err != nil || !bytes.Equal(enc, wire) {
				t.Fatalf("encoded % x (%v), want % x", enc, err, wire)
			}
			got, err := service.DecodeAuthRequest(wire, limits)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.Parameters, tc.params) {
				t.Fatalf("decoded %+v\nwant %+v", got.Parameters, tc.params)
			}
			again, err := service.EncodeAuthRequest(got)
			if err != nil || !bytes.Equal(again, wire) {
				t.Fatalf("re-encoded % x (%v), want % x", again, err, wire)
			}
		})
	}
	empty, err := service.DecodeAuthRequest(fromHex("0e0f"), limits)
	if err != nil || len(empty.Parameters) != 0 {
		t.Fatalf("empty [0]: %+v %v", empty, err)
	}
	for _, tc := range []struct {
		name string
		wire string
		want error
	}{
		{name: "elements without the [0]", wire: "210911", want: bacnet.ErrMalformed},
		{name: "an application element", wire: "2109", want: bacnet.ErrMalformed},
		{name: "another context tag", wire: "1e09011f", want: bacnet.ErrMalformed},
		{name: "a primitive [0]", wire: "0901", want: bacnet.ErrMalformed},
		{name: "something after the [0]", wire: "0e09010f0902", want: bacnet.ErrTrailingData},
		{name: "two [0]", wire: "0e09010f0e09020f", want: bacnet.ErrTrailingData},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := service.DecodeAuthRequest(fromHex(tc.wire), limits); !errors.Is(err, tc.want) {
				t.Fatalf("decoded %+v, %v; want an error wrapping %v", got, err, tc.want)
			}
		})
	}
}

// ReadRangeACK: an item of itemData keeps its tag, so an ACK with
// context-tagged items re-encodes to the octets it came from.
func TestReadRangeACKItemsReencode(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	head := func(objType bacnet.ObjectType, prop bacnet.PropertyIdentifier, count uint64) []byte {
		b := must(bacnet.AppendContextObjectID(nil, 0, bacnet.ObjectIdentifier{Type: objType, Instance: 1}))
		b = append(b, must(bacnet.AppendContextUnsigned(nil, 1, uint64(prop)))...)
		b = append(b, must(bacnet.AppendContextBitString(nil, 3, service.EncodeResultFlags(true, true, false)))...)
		return append(b, must(bacnet.AppendContextUnsigned(nil, 4, count))...)
	}
	for _, tc := range []struct {
		name    string
		objType bacnet.ObjectType
		prop    bacnet.PropertyIdentifier
		items   string // the content of itemData [5]
		count   uint64
		want    []bacnet.ApplicationValue
	}{
		{name: "application items", objType: bacnet.ObjectTypeTrendLog, prop: bacnet.PropertyRecipientList, items: "2101" + "4400000000", count: 2,
			want: []bacnet.ApplicationValue{bacnet.UnsignedValue(1), bacnet.RealValue(0)}},
		{name: "a context primitive", objType: bacnet.ObjectTypeNotificationClass, prop: bacnet.PropertyRecipientList, items: "0c02000001", count: 1,
			want: []bacnet.ApplicationValue{{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
				{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{0x02, 0, 0, 1}}}}}}},
		{name: "a context constructed", objType: bacnet.ObjectTypeNotificationClass, prop: bacnet.PropertyRecipientList, items: "1e21051f", count: 1,
			want: []bacnet.ApplicationValue{{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
				{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{TagNumber: 2, Value: bacnet.UnsignedValue(5)}}}}}}}},
		{name: "nested context constructed", objType: bacnet.ObjectTypeNotificationClass, prop: bacnet.PropertyRecipientList, items: "1e0e09010f1f" + "2101", count: 2},
		{name: "a BACnetDestination", objType: bacnet.ObjectTypeNotificationClass, prop: bacnet.PropertyRecipientList,
			items: "8201fe" + "b400000000" + "b4173b3b63" + "0c02000001" + "2101" + "10" + "8205e0", count: 7},
		{name: "event log records as the items of another property", objType: bacnet.ObjectTypeEventLog, prop: bacnet.PropertyDescription,
			items: "0ea46f090703b4120519400f" + "1e0a05001f", count: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			items := fromHex(tc.items)
			wire := head(tc.objType, tc.prop, tc.count)
			wire = append(append(append(wire, 0x5e), items...), 0x5f)
			ack, err := service.DecodeReadRangeACK(wire, limits)
			if err != nil {
				t.Fatal(err)
			}
			if tc.want != nil && !reflect.DeepEqual(ack.ItemData, tc.want) {
				t.Fatalf("items %+v\nwant %+v", ack.ItemData, tc.want)
			}
			again, err := service.EncodeReadRangeACK(ack)
			if err != nil {
				t.Fatalf("decoded ACK does not encode: %v", err)
			}
			if !bytes.Equal(again, wire) {
				t.Fatalf("re-encoded\n% x\nwant\n% x", again, wire)
			}
		})
	}
}

// COVNotification and Add/RemoveListElement with an empty list: clauses
// 13.6.1.1.5/13.7 and 15.1.1.1.4/15.2.1.1.4 ask for one or more, and the
// encoders refuse to write none. The decoders accept an empty list on
// purpose, pending a decision on refusing it; this test pins that, so a
// change of it is deliberate.
func TestEmptyListsAccepted(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	cov := func(values string) []byte {
		b := must(bacnet.AppendContextUnsigned(nil, 0, 1))
		b = append(b, must(bacnet.AppendContextObjectID(nil, 1, dev))...)
		b = append(b, must(bacnet.AppendContextObjectID(nil, 2, av))...)
		b = append(b, must(bacnet.AppendContextUnsigned(nil, 3, 60))...)
		return append(append(append(b, 0x4e), fromHex(values)...), 0x4f)
	}
	list := func(elements string) []byte {
		b := must(bacnet.AppendContextObjectID(nil, 0, av))
		b = append(b, must(bacnet.AppendContextUnsigned(nil, 1, uint64(bacnet.PropertyRecipientList)))...)
		return append(append(append(b, 0x3e), fromHex(elements)...), 0x3f)
	}
	t.Run("COVNotification without values", func(t *testing.T) {
		got, err := service.DecodeCOVNotification(cov(""), limits)
		if err != nil || got.ProcessIdentifier != 1 || got.MonitoredObject != av || len(got.Values) != 0 {
			t.Fatalf("decoded %+v, %v; want the notification with no values", got, err)
		}
		if b, err := service.EncodeCOVNotification(got); err == nil {
			t.Fatalf("the encoder wrote a notification without values: % x", b)
		}
	})
	t.Run("COVNotification with one value", func(t *testing.T) {
		wire := cov("0955" + "2e44000000002f")
		got, err := service.DecodeCOVNotification(wire, limits)
		if err != nil || len(got.Values) != 1 {
			t.Fatalf("decoded %+v, %v", got, err)
		}
		if again, err := service.EncodeCOVNotification(got); err != nil || !bytes.Equal(again, wire) {
			t.Fatalf("re-encoded % x (%v), want % x", again, err, wire)
		}
	})
	t.Run("ListElementRequest with an empty list", func(t *testing.T) {
		got, err := service.DecodeListElementRequest(list(""), limits)
		if err != nil || got.Object != av || got.Property.Identifier != bacnet.PropertyRecipientList || len(got.Elements) != 0 {
			t.Fatalf("decoded %+v, %v; want the request with no elements", got, err)
		}
		if b, err := service.EncodeListElementRequest(got); err == nil {
			t.Fatalf("the encoder wrote an empty list of elements: % x", b)
		}
	})
	t.Run("ListElementRequest with one element", func(t *testing.T) {
		wire := list("2101")
		got, err := service.DecodeListElementRequest(wire, limits)
		if err != nil || len(got.Elements) != 1 {
			t.Fatalf("decoded %+v, %v", got, err)
		}
		if again, err := service.EncodeListElementRequest(got); err != nil || !bytes.Equal(again, wire) {
			t.Fatalf("re-encoded % x (%v), want % x", again, err, wire)
		}
	})
}
