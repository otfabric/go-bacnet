// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// listObject is an application object with a generic list property that
// ReadRange reads as item data, and list services that fail.
type listObject struct {
	*object.Base
	items []bacnet.ApplicationValue
}

var listObjectID = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeStructuredView, Instance: 5}

// badValue is a value no encoder accepts.
var badValue = bacnet.ApplicationValue{Kind: bacnet.ValueKind(200)}

func newListObject(n int) *listObject {
	o := &listObject{Base: object.NewBase(listObjectID, "LIST-5")}
	for i := 0; i < n; i++ {
		o.items = append(o.items, bacnet.UnsignedValue(uint64(1000+i)))
	}
	o.Define(bacnet.PropertySubordinateList, &object.Property{Get: func() bacnet.ApplicationValue {
		els := make([]bacnet.Element, len(o.items))
		for i, v := range o.items {
			els[i] = bacnet.Element{Value: v}
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
	}})
	// Description cannot be encoded: reading it fails as the application's
	// failure, not as a malformed reply.
	o.Define(bacnet.PropertyDescription, &object.Property{Get: func() bacnet.ApplicationValue { return badValue }})
	return o
}

func (o *listObject) ReadRange(_ context.Context, prop bacnet.PropertyReference, _ object.RangeQuery) (object.RangeResult, error) {
	switch prop.Identifier {
	case bacnet.PropertySubordinateList:
		return object.RangeResult{Items: o.items, Position: 1, Total: uint32(len(o.items))}, nil
	case bacnet.PropertyDescription:
		return object.RangeResult{Items: []bacnet.ApplicationValue{badValue}, Position: 1, Total: 1}, nil
	default:
		return object.RangeResult{}, object.ErrReadAccessDenied
	}
}

func (o *listObject) AddListElements(context.Context, bacnet.PropertyReference, []bacnet.Element) error {
	return &object.ListError{Err: object.ErrInvalidDataType, First: 2}
}

func (o *listObject) RemoveListElements(context.Context, bacnet.PropertyReference, []bacnet.Element) error {
	return object.ErrWriteAccessDenied
}

func (h *harness) expectWrappedError(class, code uint16, first uint32) {
	h.t.Helper()
	got := h.expectType(apdu.TypeError)
	d, err := apdu.DecodeError(got.pdu.Error.Payload, bacnet.DefaultDecodeLimits())
	if err != nil || !d.Wrapped || d.Class != class || d.Code != code {
		h.t.Fatalf("error %+v (%v), want %s/%s", d, err, bacnet.ErrorClassName(class), bacnet.ErrorCodeName(code))
	}
	if n, ok := d.FirstFailedElementNumber(); !ok || n != first {
		h.t.Fatalf("first failed element %d (%v), want %d", n, ok, first)
	}
}

func isBACnetError(err error, class, code uint16) bool {
	var e *bacnet.ErrorResponse
	return errors.As(err, &e) && e.Class == class && e.Code == code
}

func (h *harness) complexACK() []byte {
	h.t.Helper()
	return h.expectType(apdu.TypeComplexACK).pdu.ComplexACK.Payload
}

func TestReadPropertyErrors(t *testing.T) {
	h := newHarness(t)
	if err := h.dev.Add(newListObject(1)); err != nil {
		t.Fatal(err)
	}
	enc := mustEncode(t)
	rp := func(o bacnet.ObjectIdentifier, p bacnet.PropertyIdentifier) []byte {
		return enc(service.EncodeReadProperty(service.ReadPropertyRequest{Object: o, Property: bacnet.PropertyReference{Identifier: p}}))
	}
	h.confirmed(1, apdu.ServiceReadProperty, []byte{0xFF})
	h.expectReject(bacnet.RejectReasonInvalidTag)
	h.confirmed(2, apdu.ServiceReadProperty, rp(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 77}, bacnet.PropertyPresentValue))
	h.expectError(bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	// ALL, REQUIRED and OPTIONAL are ReadPropertyMultiple selectors only.
	for i, p := range []bacnet.PropertyIdentifier{bacnet.PropertyAll, bacnet.PropertyRequired, bacnet.PropertyOptional} {
		h.confirmed(uint8(3+i), apdu.ServiceReadProperty, rp(av1, p))
		h.expectError(bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
	}
	h.confirmed(6, apdu.ServiceReadProperty, rp(av1, bacnet.PropertyFileSize))
	h.expectError(bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
	// A value that cannot be encoded is the application's failure.
	h.confirmed(7, apdu.ServiceReadProperty, rp(listObjectID, bacnet.PropertyDescription))
	h.expectError(bacnet.ErrorClassDevice, bacnet.ErrorCodeOperationalProblem)
	h.confirmed(8, apdu.ServiceReadPropertyMultiple, enc(service.EncodeReadPropertyMultiple([]service.ReadAccessSpecification{{
		Object: listObjectID, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyAll}},
	}})))
	h.expectError(bacnet.ErrorClassDevice, bacnet.ErrorCodeOperationalProblem)
	h.confirmed(9, apdu.ServiceReadPropertyMultiple, []byte{0xFF})
	h.expectReject(bacnet.RejectReasonInvalidTag)
}

// TestReadPropertyMultipleSelectors checks REQUIRED and OPTIONAL against
// the object's own split of its properties, and per-property errors.
func TestReadPropertyMultipleSelectors(t *testing.T) {
	h := newHarness(t)
	enc := mustEncode(t)
	all, required := h.av.Properties()
	unknown := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 77}
	h.confirmed(1, apdu.ServiceReadPropertyMultiple, enc(service.EncodeReadPropertyMultiple([]service.ReadAccessSpecification{
		{Object: av1, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyRequired}}},
		{Object: av1, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyOptional}, {Identifier: bacnet.PropertyFileSize}}},
		{Object: unknown, Properties: []bacnet.PropertyReference{pvRef, {Identifier: bacnet.PropertyObjectName}}},
	})))
	res, err := service.DecodeReadPropertyMultipleACK(h.complexACK(), bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("%d results", len(res))
	}
	nreq := 0
	for _, id := range all {
		if required[id] {
			nreq++
		}
	}
	if len(res[0].Properties) != nreq {
		t.Fatalf("REQUIRED: %d properties, want %d", len(res[0].Properties), nreq)
	}
	for _, p := range res[0].Properties {
		if !required[p.Property.Identifier] || p.Err != nil {
			t.Fatalf("REQUIRED returned %+v", p)
		}
	}
	opt := res[1].Properties
	if len(opt) != len(all)-nreq+1 {
		t.Fatalf("OPTIONAL: %d properties, want %d", len(opt), len(all)-nreq+1)
	}
	for _, p := range opt[:len(opt)-1] {
		if required[p.Property.Identifier] {
			t.Fatalf("OPTIONAL returned required %v", p.Property.Identifier)
		}
	}
	if last := opt[len(opt)-1]; !isBACnetError(last.Err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty) {
		t.Fatalf("unknown property %+v", last)
	}
	if res[2].Object != unknown || len(res[2].Properties) != 2 {
		t.Fatalf("unknown object %+v", res[2])
	}
	for _, p := range res[2].Properties {
		if !isBACnetError(p.Err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject) {
			t.Fatalf("unknown object property %+v", p)
		}
	}
}

func TestWritePropertyHandlers(t *testing.T) {
	var hooked []object.Write
	denied := object.Err(bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	h := newHarness(t, WithHooks(Hooks{BeforeWrite: func(_ context.Context, obj bacnet.ObjectIdentifier, w object.Write) error {
		if obj != av1 {
			return errors.New("unexpected object")
		}
		hooked = append(hooked, w)
		if w.Value.Kind == bacnet.ValueReal && w.Value.Real == 13 {
			return denied
		}
		return nil
	}}))
	enc := mustEncode(t)
	prio := uint8(5)
	h.confirmed(1, apdu.ServiceWriteProperty, []byte{0xFF})
	h.expectReject(bacnet.RejectReasonInvalidTag)
	h.confirmed(2, apdu.ServiceWriteProperty, enc(service.EncodeWriteProperty(service.WritePropertyRequest{
		Object: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 77}, Property: pvRef, Value: bacnet.RealValue(1)})))
	h.expectError(bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	h.confirmed(3, apdu.ServiceWriteProperty, enc(service.EncodeWriteProperty(service.WritePropertyRequest{
		Object: av1, Property: pvRef, Value: bacnet.RealValue(13)})))
	h.expectError(bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	if h.av.PresentValue().Real != 1 {
		t.Fatalf("refused write applied: %+v", h.av.PresentValue())
	}
	// WritePropertyMultiple: the priority reaches the object, and the
	// first failure ends the request with the writes before it applied.
	h.confirmed(4, apdu.ServiceWritePropertyMultiple, enc(service.EncodeWritePropertyMultiple([]service.WriteAccessSpecification{{
		Object: av1, Properties: []service.WritePropertyValue{
			{Property: pvRef, Value: bacnet.RealValue(21), Priority: &prio},
			{Property: pvRef, Value: bacnet.RealValue(13)},
			{Property: pvRef, Value: bacnet.RealValue(22)},
		},
	}})))
	got := h.expectType(apdu.TypeError)
	wpm, err := service.DecodeWritePropertyMultipleError(got.pdu.Error.Payload, bacnet.DefaultDecodeLimits())
	if err != nil || wpm.Code != bacnet.ErrorCodeWriteAccessDenied || wpm.FirstFailed != av1 || wpm.FirstProperty.Identifier != bacnet.PropertyPresentValue {
		t.Fatalf("WPM error %+v %v", wpm, err)
	}
	if h.av.PresentValue().Real != 21 {
		t.Fatalf("Present_Value %+v", h.av.PresentValue())
	}
	if cp, _ := h.av.ReadProperty(context.Background(), bacnet.PropertyReference{Identifier: bacnet.PropertyCurrentCommandPriority}); cp.Unsigned != 5 {
		t.Fatalf("Current_Command_Priority %+v", cp)
	}
	if len(hooked) != 3 || hooked[1].Priority != 5 || hooked[2].Priority != 0 {
		t.Fatalf("hooked %+v", hooked)
	}
	h.confirmed(5, apdu.ServiceWritePropertyMultiple, []byte{0xFF})
	h.expectReject(bacnet.RejectReasonInvalidTag)
}

func TestWhoIsWhoHas(t *testing.T) {
	h := newHarness(t)
	enc := mustEncode(t)
	u := func(v uint32) *uint32 { return &v }
	// Malformed or out of range: no answer.
	h.unconfirmed(apdu.ServiceWhoIs, []byte{0xFF})
	h.unconfirmed(apdu.ServiceWhoIs, enc(service.EncodeWhoIs(service.WhoIs{LowLimit: u(1), HighLimit: u(99)})))
	h.unconfirmed(apdu.ServiceWhoIs, enc(service.EncodeWhoIs(service.WhoIs{LowLimit: u(101), HighLimit: u(200)})))
	h.unconfirmed(apdu.ServiceWhoHas, []byte{0xFF})
	h.unconfirmed(apdu.ServiceWhoHas, enc(service.EncodeWhoHas(service.WhoHas{LowLimit: u(1), HighLimit: u(99), Object: &av1})))
	missing := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 3}
	h.unconfirmed(apdu.ServiceWhoHas, enc(service.EncodeWhoHas(service.WhoHas{Object: &missing})))
	h.unconfirmed(apdu.ServiceWhoHas, enc(service.EncodeWhoHas(service.WhoHas{Name: &bacnet.CharacterString{Value: "nope"}})))
	h.quiet(30 * time.Millisecond)

	ihave := func(got sent) service.IHave {
		t.Helper()
		if got.pdu.Type != apdu.TypeUnconfirmedRequest || got.pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceIHave {
			t.Fatalf("got %+v, want I-Have", got.pdu)
		}
		ih, err := service.DecodeIHave(got.pdu.UnconfirmedRequest.Payload, bacnet.DefaultDecodeLimits())
		if err != nil {
			t.Fatal(err)
		}
		return ih
	}
	// A unicast Who-Has in range, by identifier: a unicast I-Have.
	h.unconfirmed(apdu.ServiceWhoHas, enc(service.EncodeWhoHas(service.WhoHas{LowLimit: u(100), HighLimit: u(100), Object: &av1})))
	got := h.next()
	if ih := ihave(got); ih.Object != av1 || ih.Name.Value != "AV-1" || ih.Device.Instance != 100 || got.pkt.Broadcast {
		t.Fatalf("I-Have %+v, broadcast %v", ih, got.pkt.Broadcast)
	}
	// A broadcast Who-Has by name: a broadcast I-Have.
	req := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceWhoHas,
		Payload: enc(service.EncodeWhoHas(service.WhoHas{Name: &bacnet.CharacterString{Value: "AV-1"}}))})
	h.injectFrom(clientEP, bvlc.FunctionOriginalBroadcastNPDU, npdu.NPDU{Version: npdu.Version1, APDU: req})
	got = h.next()
	if ih := ihave(got); ih.Object != av1 || !got.pkt.Broadcast {
		t.Fatalf("I-Have %+v, broadcast %v", ih, got.pkt.Broadcast)
	}
}

func TestReadRangeHandler(t *testing.T) {
	h := newHarness(t)
	lo := newListObject(40)
	if err := h.dev.Add(lo); err != nil {
		t.Fatal(err)
	}
	enc := mustEncode(t)
	rr := func(o bacnet.ObjectIdentifier, p bacnet.PropertyIdentifier) []byte {
		return enc(service.EncodeReadRange(service.ReadRangeRequest{Object: o, Property: bacnet.PropertyReference{Identifier: p}}))
	}
	h.confirmed(1, apdu.ServiceReadRange, []byte{0xFF})
	h.expectReject(bacnet.RejectReasonInvalidTag)
	h.confirmed(2, apdu.ServiceReadRange, rr(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 9}, bacnet.PropertyLogBuffer))
	h.expectError(bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	// The object's own error.
	h.confirmed(3, apdu.ServiceReadRange, rr(listObjectID, bacnet.PropertyObjectName))
	h.expectError(bacnet.ErrorClassProperty, bacnet.ErrorCodeReadAccessDenied)
	h.confirmed(4, apdu.ServiceReadRange, rr(listObjectID, bacnet.PropertyDescription))
	h.expectError(bacnet.ErrorClassDevice, bacnet.ErrorCodeOperationalProblem)

	// Every item fits a large buffer.
	h.confirmed(5, apdu.ServiceReadRange, rr(listObjectID, bacnet.PropertySubordinateList))
	ack, err := service.DecodeReadRangeACK(h.complexACK(), bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if ack.ItemCount != 40 || len(ack.ItemData) != 40 || !ack.FirstItem() || !ack.LastItem() || ack.MoreItems() || ack.FirstSequence != nil {
		t.Fatalf("full ACK count %d, flags %+v", ack.ItemCount, ack.ResultFlags)
	}
	// An unsegmented 50-octet buffer takes a prefix and sets MORE_ITEMS.
	h.request(apdu.ConfirmedRequest{MaxAPDU: 0, InvokeID: 6, ServiceChoice: apdu.ServiceReadRange, Payload: rr(listObjectID, bacnet.PropertySubordinateList)})
	payload := h.complexACK()
	if len(payload) > 50-3 {
		t.Fatalf("%d octets for a 50-octet APDU", len(payload))
	}
	ack, err = service.DecodeReadRangeACK(payload, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if ack.ItemCount == 0 || ack.ItemCount >= 40 || !ack.FirstItem() || ack.LastItem() || !ack.MoreItems() {
		t.Fatalf("truncated ACK count %d, flags %+v", ack.ItemCount, ack.ResultFlags)
	}
	for i, v := range ack.ItemData {
		if v.Unsigned != uint64(1000+i) {
			t.Fatalf("item %d = %+v", i, v)
		}
	}
	// A requester that accepts two segments of 50 octets gets more items,
	// still not all of them.
	h.request(apdu.ConfirmedRequest{SegmentedResponseAccepted: true, MaxSegments: 1, MaxAPDU: 0, InvokeID: 7,
		ServiceChoice: apdu.ServiceReadRange, Payload: rr(listObjectID, bacnet.PropertySubordinateList)})
	var seg []byte
	for {
		got := h.expectType(apdu.TypeComplexACK).pdu.ComplexACK
		seg = append(seg, got.Payload...)
		h.segmentACK(7, got.SequenceNumber, 1, false)
		if !got.MoreFollows {
			break
		}
	}
	two, err := service.DecodeReadRangeACK(seg, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if two.ItemCount <= ack.ItemCount || two.ItemCount >= 40 || !two.MoreItems() {
		t.Fatalf("two segments: count %d (one APDU %d), flags %+v", two.ItemCount, ack.ItemCount, two.ResultFlags)
	}
}

func TestListElementHandlers(t *testing.T) {
	refuse := object.Err(bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	var hooked int
	h := newHarness(t, WithHooks(Hooks{BeforeWrite: func(_ context.Context, obj bacnet.ObjectIdentifier, w object.Write) error {
		hooked++
		if w.Property.Identifier == bacnet.PropertyRecipientList {
			return refuse
		}
		return nil
	}}))
	if err := h.dev.Add(newListObject(0)); err != nil {
		t.Fatal(err)
	}
	enc := mustEncode(t)
	le := func(o bacnet.ObjectIdentifier, p bacnet.PropertyIdentifier) []byte {
		return enc(service.EncodeListElementRequest(service.ListElementRequest{Object: o, Property: bacnet.PropertyReference{Identifier: p},
			Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}, {Value: bacnet.UnsignedValue(2)}}}))
	}
	h.confirmed(1, apdu.ServiceAddListElement, []byte{0xFF})
	h.expectReject(bacnet.RejectReasonInvalidTag)
	h.confirmed(2, apdu.ServiceRemoveListElement, le(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeCalendar, Instance: 1}, bacnet.PropertyDateList))
	h.expectWrappedError(bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject, 0)
	// An object without list support: its own error for an unknown
	// property, property-is-not-a-list for any other.
	h.confirmed(3, apdu.ServiceAddListElement, le(av1, bacnet.PropertyDateList))
	h.expectWrappedError(bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty, 0)
	h.confirmed(4, apdu.ServiceAddListElement, le(av1, bacnet.PropertyPresentValue))
	h.expectWrappedError(bacnet.ErrorClassServices, bacnet.ErrorCodePropertyIsNotAList, 0)
	if hooked != 0 {
		t.Fatalf("hook called %d times for objects without lists", hooked)
	}
	// The hook refuses before the object is asked.
	h.confirmed(5, apdu.ServiceAddListElement, le(listObjectID, bacnet.PropertyRecipientList))
	h.expectWrappedError(bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied, 0)
	// A ListError names the element that failed.
	h.confirmed(6, apdu.ServiceAddListElement, le(listObjectID, bacnet.PropertySubordinateList))
	h.expectWrappedError(bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType, 2)
	h.confirmed(7, apdu.ServiceRemoveListElement, le(listObjectID, bacnet.PropertySubordinateList))
	h.expectWrappedError(bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied, 0)
	if hooked != 3 {
		t.Fatalf("hook called %d times", hooked)
	}
}

func TestCreateObjectHandler(t *testing.T) {
	h := newHarness(t)
	avType := bacnet.ObjectTypeAnalogValue
	h.dev.RegisterFactory(avType, func(instance uint32, name string) (object.Object, error) {
		return object.NewAnalogValue(object.PointConfig{Instance: instance, Name: name, Commandable: true})
	})
	enc := mustEncode(t)
	create := func(inv uint8, req service.CreateObjectRequest) {
		h.confirmed(inv, apdu.ServiceCreateObject, enc(service.EncodeCreateObject(req)))
	}
	pv := func(id bacnet.PropertyIdentifier, v bacnet.ApplicationValue) service.PropertyValue {
		return service.PropertyValue{Property: bacnet.PropertyReference{Identifier: id}, Value: v}
	}
	h.confirmed(1, apdu.ServiceCreateObject, []byte{0xFF})
	h.expectReject(bacnet.RejectReasonInvalidTag)
	// Object_Name must be a non-empty CharacterString not used yet.
	create(2, service.CreateObjectRequest{ObjectType: &avType, InitialValues: []service.PropertyValue{
		pv(bacnet.PropertyDescription, object.CharString("d")), pv(bacnet.PropertyObjectName, bacnet.UnsignedValue(1))}})
	h.expectWrappedError(bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType, 2)
	create(3, service.CreateObjectRequest{ObjectType: &avType, InitialValues: []service.PropertyValue{pv(bacnet.PropertyObjectName, object.CharString(""))}})
	h.expectWrappedError(bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType, 1)
	create(4, service.CreateObjectRequest{ObjectType: &avType, InitialValues: []service.PropertyValue{pv(bacnet.PropertyObjectName, object.CharString("AV-1"))}})
	h.expectWrappedError(bacnet.ErrorClassProperty, bacnet.ErrorCodeDuplicateName, 1)
	// Object_Identifier and Object_Type are fixed at creation: a matching
	// value is accepted, another is write-access-denied and the object is
	// removed again.
	id := bacnet.ObjectIdentifier{Type: avType, Instance: 20}
	create(5, service.CreateObjectRequest{ObjectIdentifier: &id, InitialValues: []service.PropertyValue{
		pv(bacnet.PropertyObjectType, bacnet.EnumValue(uint32(avType))),
		pv(bacnet.PropertyObjectIdentifier, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: avType, Instance: 21})),
	}})
	h.expectWrappedError(bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied, 2)
	if _, ok := h.dev.Object(id); ok {
		t.Fatal("object kept after a failed initial value")
	}
	// A failing write removes the object and names the value.
	create(6, service.CreateObjectRequest{ObjectIdentifier: &id, InitialValues: []service.PropertyValue{
		pv(bacnet.PropertyObjectName, object.CharString("NEW-20")), pv(bacnet.PropertyPresentValue, bacnet.UnsignedValue(1))}})
	h.expectWrappedError(bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType, 2)
	if _, ok := h.dev.ObjectByName("NEW-20"); ok {
		t.Fatal("object kept after a failed write")
	}
	// Success: the priority of an initial value is used.
	prio := uint8(3)
	create(7, service.CreateObjectRequest{ObjectIdentifier: &id, InitialValues: []service.PropertyValue{
		pv(bacnet.PropertyObjectIdentifier, bacnet.ObjectIDValue(id)),
		pv(bacnet.PropertyObjectName, object.CharString("NEW-20")),
		{Property: pvRef, Value: bacnet.RealValue(42), Priority: &prio},
	}})
	ack, err := service.DecodeCreateObjectACK(h.complexACK(), bacnet.DefaultDecodeLimits())
	if err != nil || ack.Object != id {
		t.Fatalf("ACK %+v %v", ack, err)
	}
	o, ok := h.dev.ObjectByName("NEW-20")
	if !ok || o.Identifier() != id {
		t.Fatal("created object not found by name")
	}
	if cp, _ := o.ReadProperty(context.Background(), bacnet.PropertyReference{Identifier: bacnet.PropertyCurrentCommandPriority}); cp.Unsigned != 3 {
		t.Fatalf("Current_Command_Priority %+v", cp)
	}
	// The wildcard instance picks one.
	wild := bacnet.ObjectIdentifier{Type: avType, Instance: bacnet.WildcardInstance}
	create(8, service.CreateObjectRequest{ObjectIdentifier: &wild})
	ack, err = service.DecodeCreateObjectACK(h.complexACK(), bacnet.DefaultDecodeLimits())
	if err != nil || ack.Object.Type != avType || ack.Object.Instance != 0 {
		t.Fatalf("wildcard ACK %+v %v", ack, err)
	}
}

func TestDeleteObjectHandler(t *testing.T) {
	refuse := object.Err(bacnet.ErrorClassObject, bacnet.ErrorCodeObjectDeletionNotPermitted)
	allow := false
	h := newHarness(t, WithHooks(Hooks{BeforeDelete: func(context.Context, bacnet.ObjectIdentifier) error {
		if !allow {
			return refuse
		}
		return nil
	}}))
	enc := mustEncode(t)
	del := func(inv uint8, id bacnet.ObjectIdentifier) {
		h.confirmed(inv, apdu.ServiceDeleteObject, enc(service.EncodeDeleteObject(service.DeleteObjectRequest{Object: id})))
	}
	h.confirmed(1, apdu.ServiceDeleteObject, []byte{0xFF})
	h.expectReject(bacnet.RejectReasonInvalidTag)
	del(2, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 77})
	h.expectError(bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	del(3, av1)
	h.expectError(bacnet.ErrorClassObject, bacnet.ErrorCodeObjectDeletionNotPermitted)
	h.av.SetDeletable(true)
	del(4, av1)
	h.expectError(bacnet.ErrorClassObject, bacnet.ErrorCodeObjectDeletionNotPermitted)
	if _, ok := h.dev.Object(av1); !ok {
		t.Fatal("object deleted although the hook refused")
	}
	allow = true
	del(5, av1)
	h.expectType(apdu.TypeSimpleACK)
	if _, ok := h.dev.Object(av1); ok {
		t.Fatal("object not deleted")
	}
}
