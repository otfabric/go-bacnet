// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Error-path and malformed-input coverage for the service encoders and
// decoders. Payloads are built octet by octet so that each case reaches one
// specific check; the expected error is the sentinel the code wraps.

var (
	ceDev     = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 7}
	ceAV      = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	ceBadOID  = bacnet.ObjectIdentifier{Type: 0x400}
	ceBadBits = bacnet.BitString{UnusedBits: 9, Bytes: []byte{0}}
	ceFlags   = bacnet.BitString{UnusedBits: 4, Bytes: []byte{0}}
	ceBadTS   = service.TimeStamp{Choice: 99}
	ceSeqTS   = service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 1}
	ceLimits  = bacnet.DefaultDecodeLimits()
)

// ceCat concatenates octet slices.
func ceCat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// ceRaw is a context-tagged primitive (tag < 15) with the given content
// octets, which need not be a valid encoding of the field's type.
func ceRaw(tag uint8, content ...byte) []byte {
	if len(content) < 5 {
		return append([]byte{tag<<4 | 0x08 | byte(len(content))}, content...)
	}
	return append([]byte{tag<<4 | 0x08 | 5, byte(len(content))}, content...)
}

// ceOpen and ceClose are context opening and closing tags (tag < 15).
func ceOpen(tag uint8) []byte  { return []byte{tag<<4 | 0x0E} }
func ceClose(tag uint8) []byte { return []byte{tag<<4 | 0x0F} }

func ceWrap(tag uint8, body ...[]byte) []byte {
	return ceCat(ceOpen(tag), ceCat(body...), ceClose(tag))
}

func ceU(t *testing.T, tag uint8, v uint64) []byte {
	t.Helper()
	b, err := bacnet.AppendContextUnsigned(nil, tag, v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ceOID(t *testing.T, tag uint8, id bacnet.ObjectIdentifier) []byte {
	t.Helper()
	b, err := bacnet.AppendContextObjectID(nil, tag, id)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// ceReal is a context-tagged REAL.
func ceReal(t *testing.T, tag uint8, v float32) []byte {
	t.Helper()
	b, err := bacnet.AppendContextReal(nil, tag, v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func ceElements(t *testing.T, raw []byte) []bacnet.Element {
	t.Helper()
	els, n, err := bacnet.ParseSequence(raw, ceLimits, -1)
	if err != nil || n != len(raw) {
		t.Fatalf("parse % x: n=%d err=%v", raw, n, err)
	}
	return els
}

func ceWant(t *testing.T, name string, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Errorf("%s: err = %v, want %v", name, err, target)
	}
}

const big32 = uint64(1) << 32

// --- EventNotification -----------------------------------------------------

// ceEventFields returns the fields of a valid EventNotification by tag; the
// caller replaces or drops entries and joins them with ceEventPayload.
func ceEventFields(t *testing.T) map[uint8][]byte {
	return map[uint8][]byte{
		0:  ceU(t, 0, 1),
		1:  ceOID(t, 1, ceDev),
		2:  ceOID(t, 2, ceAV),
		3:  ceWrap(3, ceU(t, 1, 5)),
		4:  ceU(t, 4, 1),
		5:  ceU(t, 5, 100),
		6:  ceU(t, 6, 5),
		7:  ceRaw(7, 0, 'h', 'i'),
		8:  ceU(t, 8, 0),
		9:  ceRaw(9, 1),
		10: ceU(t, 10, 0),
		11: ceU(t, 11, 3),
		12: ceWrap(12, ceWrap(5, ceReal(t, 0, 1), ceRaw(1, 4, 0), ceReal(t, 2, 0.5), ceReal(t, 3, 2))),
	}
}

func ceEventPayload(fields map[uint8][]byte) []byte {
	var out []byte
	for tag := uint8(0); tag <= 12; tag++ {
		out = append(out, fields[tag]...)
	}
	return out
}

func TestCoverageEventNotificationDecodeErrors(t *testing.T) {
	base := ceEventPayload(ceEventFields(t))
	if _, err := service.DecodeEventNotification(base, ceLimits); err != nil {
		t.Fatalf("base payload: %v", err)
	}
	// A second copy of every singular field is rejected.
	for tag := uint8(1); tag <= 12; tag++ {
		raw := append(append([]byte{}, base...), ceEventFields(t)[tag]...)
		_, err := service.DecodeEventNotification(raw, ceLimits)
		ceWant(t, "duplicate tag", err, bacnet.ErrMalformed)
	}
	bad := map[string]struct {
		tag   uint8
		value []byte
	}{
		"pid empty":          {0, ceRaw(0)},
		"device length":      {1, ceRaw(1, 1, 2, 3)},
		"event type 33 bit":  {6, ceU(t, 6, big32)},
		"message empty":      {7, ceRaw(7)},
		"notify type 33 bit": {8, ceU(t, 8, big32)},
		"from state 33 bit":  {10, ceU(t, 10, big32)},
		"to state 33 bit":    {11, ceU(t, 11, big32)},
		"params empty":       {12, ceWrap(12)},
	}
	for name, c := range bad {
		f := ceEventFields(t)
		f[c.tag] = c.value
		_, err := service.DecodeEventNotification(ceEventPayload(f), ceLimits)
		ceWant(t, name, err, bacnet.ErrMalformed)
	}
	// TimeStamp dateTime [2] needs exactly a date and a time.
	f := ceEventFields(t)
	f[3] = ceWrap(3, ceWrap(2))
	_, err := service.DecodeEventNotification(ceEventPayload(f), ceLimits)
	ceWant(t, "timestamp dateTime contents", err, bacnet.ErrMalformed)
}

func TestCoverageEventNotificationEncodeErrors(t *testing.T) {
	note := service.EventNotification{
		InitiatingDevice: ceDev, EventObject: ceAV, TimeStamp: ceBadTS,
	}
	_, err := service.EncodeEventNotification(note)
	ceWant(t, "bad timestamp", err, bacnet.ErrMalformed)
	note.TimeStamp = ceSeqTS
	note.Parameters = &service.NotificationParameters{}
	_, err = service.EncodeEventNotification(note)
	ceWant(t, "empty parameters", err, bacnet.ErrMalformed)
}

// --- NotificationParameters ------------------------------------------------

func TestCoverageNotificationParametersEncodeErrors(t *testing.T) {
	cases := map[string]service.NotificationParameters{
		"floating-limit flags": {FloatingLimit: &service.FloatingLimitParams{StatusFlags: ceBadBits}},
		"out-of-range flags":   {OutOfRange: &service.OutOfRangeParams{StatusFlags: ceBadBits}},
		"life-safety flags":    {ChangeOfLifeSafety: &service.ChangeOfLifeSafetyParams{StatusFlags: ceBadBits}},
		"change-of-state flags": {ChangeOfState: &service.ChangeOfStateParams{
			NewState: service.PropertyStates{Choice: 1, Value: bacnet.EnumValue(1)}, StatusFlags: ceBadBits}},
		"bitstring ref":   {ChangeOfBitstring: &service.ChangeOfBitstringParams{ReferencedBitstring: ceBadBits, StatusFlags: ceFlags}},
		"bitstring flags": {ChangeOfBitstring: &service.ChangeOfBitstringParams{ReferencedBitstring: ceFlags, StatusFlags: ceBadBits}},
		"cov flags": {ChangeOfValue: &service.ChangeOfValueParams{
			NewValue: bacnet.RealValue(1), StatusFlags: ceBadBits}},
		"command-failure flags": {CommandFailure: &service.CommandFailureParams{StatusFlags: ceBadBits}},
		"unsigned-range flags":  {UnsignedRange: &service.UnsignedRangeParams{StatusFlags: ceBadBits}},
		// Context tag 255 is reserved, whatever the PropertyStates value kind.
		"states bool tag":     {ChangeOfState: &service.ChangeOfStateParams{NewState: service.PropertyStates{Choice: 255, Value: bacnet.BoolValue(true)}}},
		"states enum tag":     {ChangeOfState: &service.ChangeOfStateParams{NewState: service.PropertyStates{Choice: 255, Value: bacnet.EnumValue(2)}}},
		"states unsigned tag": {ChangeOfState: &service.ChangeOfStateParams{NewState: service.PropertyStates{Choice: 255, Value: bacnet.UnsignedValue(2)}}},
	}
	for name, p := range cases {
		_, err := service.EncodeNotificationParameters(p)
		ceWant(t, name, err, bacnet.ErrMalformed)
	}
}

func TestCoverageNotificationParametersDecodeErrors(t *testing.T) {
	flags := ceRaw(1, 4, 0)
	r0, r2, r3 := ceReal(t, 0, 1), ceReal(t, 2, 2), ceReal(t, 3, 3)
	wrongTag := ceReal(t, 9, 1)
	cases := map[string][]byte{
		"floating-limit ref":       ceWrap(4, wrongTag, flags, r2, r3),
		"floating-limit flags":     ceWrap(4, r0, ceRaw(1), r2, r3),
		"floating-limit setpoint":  ceWrap(4, r0, flags, wrongTag, r3),
		"floating-limit limit":     ceWrap(4, r0, flags, r2, wrongTag),
		"out-of-range exceeding":   ceWrap(5, wrongTag, flags, r2, r3),
		"out-of-range flags tag":   ceWrap(5, r0, ceRaw(2, 4, 0), r2, r3),
		"out-of-range flags":       ceWrap(5, r0, ceRaw(1), r2, r3),
		"out-of-range deadband":    ceWrap(5, r0, flags, wrongTag, r3),
		"out-of-range limit":       ceWrap(5, r0, flags, r2, wrongTag),
		"bitstring ref":            ceWrap(0, ceRaw(0), flags),
		"bitstring flags tag":      ceWrap(0, ceRaw(0, 4, 0), ceRaw(2, 4, 0)),
		"bitstring flags":          ceWrap(0, ceRaw(0, 4, 0), ceRaw(1)),
		"cov new-value tag":        ceWrap(2, ceRaw(3, 1), flags),
		"cov flags tag":            ceWrap(2, ceWrap(0, ceRaw(1, 0, 0, 0, 0)), ceRaw(2, 4, 0)),
		"cov flags":                ceWrap(2, ceWrap(0, ceRaw(1, 0, 0, 0, 0)), ceRaw(1)),
		"state primitive":          ceWrap(1, ceRaw(0, 1), flags),
		"state two choices":        ceWrap(1, ceWrap(0, ceRaw(1, 1), ceRaw(1, 2)), flags),
		"state flags":              ceWrap(1, ceWrap(0, ceRaw(1, 1)), ceRaw(1)),
		"life-safety state":        ceWrap(8, ceRaw(0), ceRaw(1, 0), ceRaw(2, 4, 0), ceRaw(3, 0)),
		"life-safety mode":         ceWrap(8, ceRaw(0, 0), ceRaw(1), ceRaw(2, 4, 0), ceRaw(3, 0)),
		"life-safety flags":        ceWrap(8, ceRaw(0, 0), ceRaw(1, 0), ceRaw(2), ceRaw(3, 0)),
		"extended event type":      ceWrap(9, ceRaw(0, 1), ceRaw(1)),
		"unsigned-range flags":     ceWrap(11, ceRaw(0, 1), ceRaw(1), ceRaw(2, 1)),
		"unsigned-range limit":     ceWrap(11, ceRaw(0, 1), flags, ceRaw(2)),
		"buffer-ready prev count":  ceWrap(10, ceRaw(1), ceRaw(2, 1)),
		"buffer-ready count 33bit": ceWrap(10, ceU(t, 2, big32)),
	}
	for name, raw := range cases {
		_, err := service.DecodeNotificationParameters(ceElements(t, raw))
		ceWant(t, name, err, bacnet.ErrMalformed)
	}
}

// --- COVNotificationMultiple / SubscribeCOVPropertyMultiple ----------------

func TestCoverageCOVNotificationMultipleErrors(t *testing.T) {
	head := ceCat(ceU(t, 0, 1), ceOID(t, 1, ceDev), ceU(t, 2, 60))
	app := func(v bacnet.ApplicationValue) []byte {
		b, err := bacnet.AppendApplicationValue(nil, v)
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	value := ceWrap(2, app(bacnet.RealValue(1)))
	cases := map[string][]byte{
		"pid 33 bit":            ceCat(ceU(t, 0, big32), ceOID(t, 1, ceDev), ceU(t, 2, 60)),
		"device length":         ceCat(ceU(t, 0, 1), ceRaw(1, 1), ceU(t, 2, 60)),
		"time 33 bit":           ceCat(ceU(t, 0, 1), ceOID(t, 1, ceDev), ceU(t, 2, big32)),
		"object length":         ceCat(head, ceWrap(4, ceRaw(0, 1))),
		"object list tag":       ceCat(head, ceWrap(4, ceRaw(2, 1))),
		"value missing":         ceCat(head, ceWrap(4, ceOID(t, 0, ceAV), ceWrap(1, ceU(t, 0, 85), ceU(t, 0, 86), value))),
		"property 33 bit":       ceCat(head, ceWrap(4, ceOID(t, 0, ceAV), ceWrap(1, ceU(t, 0, big32), value))),
		"index without prop":    ceCat(head, ceWrap(4, ceOID(t, 0, ceAV), ceWrap(1, ceU(t, 1, 1)))),
		"index 33 bit":          ceCat(head, ceWrap(4, ceOID(t, 0, ceAV), ceWrap(1, ceU(t, 0, 85), ceU(t, 1, big32), value))),
		"time without prop":     ceCat(head, ceWrap(4, ceOID(t, 0, ceAV), ceWrap(1, ceRaw(3, 1, 2, 3, 4)))),
		"datetime without prop": ceCat(head, ceWrap(4, ceOID(t, 0, ceAV), ceWrap(1, ceWrap(3)))),
		"value list tag":        ceCat(head, ceWrap(4, ceOID(t, 0, ceAV), ceWrap(1, ceRaw(4, 1)))),
	}
	for name, raw := range cases {
		_, err := service.DecodeCOVNotificationMultiple(raw, ceLimits)
		ceWant(t, name, err, bacnet.ErrMalformed)
	}
	_, err := service.DecodeCOVNotificationMultiple([]byte{0x09}, ceLimits)
	if err == nil {
		t.Error("truncated payload decoded")
	}

	good := service.COVNotificationMultiple{
		SubscriberProcessIdentifier: 1, InitiatingDevice: ceDev, TimeRemaining: 60,
		Objects: []service.COVNotificationMultipleObject{{Object: ceAV, Values: []service.COVNotificationMultipleValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Value: bacnet.RealValue(1),
		}}}},
	}
	enc := map[string]func(n *service.COVNotificationMultiple){
		"initiating device": func(n *service.COVNotificationMultiple) { n.InitiatingDevice = ceBadOID },
		"object":            func(n *service.COVNotificationMultiple) { n.Objects[0].Object = ceBadOID },
		"value": func(n *service.COVNotificationMultiple) {
			n.Objects[0].Values[0].Value = bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: ceBadBits}
		},
	}
	for name, mutate := range enc {
		n := good
		n.Objects = []service.COVNotificationMultipleObject{{Object: ceAV, Values: append([]service.COVNotificationMultipleValue{}, good.Objects[0].Values...)}}
		mutate(&n)
		_, err := service.EncodeCOVNotificationMultiple(n)
		ceWant(t, name, err, bacnet.ErrMalformed)
	}

	_, err = service.EncodeSubscribeCOVPropertyMultiple(service.SubscribeCOVPropertyMultipleRequest{
		Subscriptions: []service.COVMultipleSubscription{{
			Object:     ceBadOID,
			Properties: []service.COVPropertyReference{{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}}},
		}},
	})
	ceWant(t, "subscription object", err, bacnet.ErrMalformed)
}

// --- WritePropertyMultiple-Error -------------------------------------------

func TestCoverageWritePropertyMultipleErrorDecode(t *testing.T) {
	errType := ceWrap(0, []byte{0x91, 0x02, 0x91, 0x20})
	ref := func(body ...[]byte) []byte { return ceCat(errType, ceWrap(1, body...)) }
	obj, prop := ceOID(t, 0, ceAV), ceU(t, 1, 85)
	cases := map[string][]byte{
		"duplicate first":     ceCat(ref(obj, prop), ceWrap(1, obj, prop)),
		"object length":       ref(ceRaw(0, 1), prop),
		"duplicate object":    ref(obj, obj, prop),
		"duplicate property":  ref(obj, prop, prop),
		"property empty":      ref(obj, ceRaw(1)),
		"property 33 bit":     ref(obj, ceU(t, 1, big32)),
		"duplicate index":     ref(obj, prop, ceU(t, 2, 1), ceU(t, 2, 2)),
		"index empty":         ref(obj, prop, ceRaw(2)),
		"index 33 bit":        ref(obj, prop, ceU(t, 2, big32)),
		"unexpected tag":      ref(obj, prop, ceU(t, 3, 1)),
		"missing property":    ref(obj),
		"parse error in body": ceCat(errType, ceOpen(1), obj),
	}
	for name, raw := range cases {
		_, err := service.DecodeWritePropertyMultipleError(raw, ceLimits)
		if name == "parse error in body" {
			if err == nil {
				t.Errorf("%s: decoded", name)
			}
			continue
		}
		ceWant(t, name, err, bacnet.ErrMalformed)
	}

	idx := uint32(3)
	want := service.WritePropertyMultipleError{
		Class: 2, Code: 32, FirstFailed: ceAV,
		FirstProperty: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: &idx},
	}
	raw, err := service.EncodeWritePropertyMultipleError(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeWritePropertyMultipleError(raw, ceLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*got, want) {
		t.Fatalf("round trip: got %+v, want %+v", *got, want)
	}
	var remote *bacnet.ErrorResponse
	if !errors.As(got, &remote) || remote.Class != 2 || remote.Code != 32 ||
		remote.Service != service.ServiceWritePropertyMultiple {
		t.Fatalf("errors.As ErrorResponse: %+v", remote)
	}
	_, err = service.EncodeWritePropertyMultipleError(service.WritePropertyMultipleError{FirstFailed: ceBadOID})
	ceWant(t, "encode bad object", err, bacnet.ErrMalformed)
}

// --- Audit -----------------------------------------------------------------

func TestCoverageAuditNotificationDecodeErrors(t *testing.T) {
	recipient := func(tag uint8) []byte { return ceWrap(tag, ceOID(t, 0, ceDev)) }
	minimal := ceCat(recipient(2), ceU(t, 4, 1), ceWrap(10, ceOID(t, 0, ceDev)))
	ok := ceWrap(0, minimal)
	if _, err := service.DecodeAuditNotificationRequest(ok, ceLimits); err != nil {
		t.Fatalf("minimal: %v", err)
	}
	// Each extra field is appended after the minimal notification; the
	// decoder takes fields in any order.
	extra := map[string][]byte{
		"timestamp choice":       ceWrap(0, ceRaw(3, 1)),
		"source object wrapped":  ceWrap(3),
		"source object length":   ceRaw(3, 1, 2, 3),
		"operation wrapped":      ceWrap(4),
		"operation empty":        ceRaw(4),
		"comment wrapped":        ceWrap(5),
		"comment empty":          ceRaw(5),
		"invoke id empty":        ceRaw(7),
		"user id empty":          ceRaw(8),
		"property primitive":     ceRaw(12, 85),
		"property empty":         ceWrap(12),
		"property id empty":      ceWrap(12, ceRaw(0)),
		"property index tag":     ceWrap(12, ceRaw(0, 85), ceRaw(2, 1)),
		"property index empty":   ceWrap(12, ceRaw(0, 85), ceRaw(1)),
		"target value primitive": ceRaw(14, 1),
		"result empty":           {0xF8, 16},
		"address network 17 bit": ceWrap(10, ceWrap(1, []byte{0x23, 0x01, 0x00, 0x00, 0x60})),
	}
	for name, field := range extra {
		raw := ceWrap(0, minimal, field)
		_, err := service.DecodeAuditNotificationRequest(raw, ceLimits)
		ceWant(t, name, err, bacnet.ErrMalformed)
	}
}

func TestCoverageAuditNotificationEncodeErrors(t *testing.T) {
	bad := bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: ceBadBits}
	badList := bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bad}}}
	cases := map[string]func(n *service.AuditNotification){
		"source timestamp": func(n *service.AuditNotification) { n.SourceTimestamp = &ceBadTS },
		"target timestamp": func(n *service.AuditNotification) { n.TargetTimestamp = &ceBadTS },
		"source object":    func(n *service.AuditNotification) { n.SourceObject = &ceBadOID },
		"target object":    func(n *service.AuditNotification) { n.TargetObject = &ceBadOID },
		"target value":     func(n *service.AuditNotification) { n.TargetValue = &bad },
		"current value":    func(n *service.AuditNotification) { n.CurrentValue = &badList },
	}
	for name, mutate := range cases {
		n := service.AuditNotification{SourceDevice: service.AuditDevice(ceDev), TargetDevice: service.AuditDevice(ceDev)}
		mutate(&n)
		_, err := service.EncodeAuditNotificationRequest([]service.AuditNotification{n})
		ceWant(t, name, err, bacnet.ErrMalformed)
	}
}

func TestCoverageAuditLogQueryACKErrors(t *testing.T) {
	date := []byte{0xA4, 126, 1, 2, 5}
	tm := []byte{0xB4, 12, 0, 0, 0}
	logStatus := ceRaw(0, 7, 0x80)
	record := func(datum ...[]byte) []byte {
		return ceWrap(1, ceWrap(0, date, tm), ceWrap(1, datum...))
	}
	ack := func(records ...[]byte) []byte {
		return ceCat(ceOID(t, 0, ceAV), ceWrap(1, records...), ceRaw(2, 1))
	}
	if _, err := service.DecodeAuditLogQueryACK(ack(ceU(t, 0, 1), record(logStatus)), ceLimits); err != nil {
		t.Fatalf("base: %v", err)
	}
	cases := map[string][]byte{
		"audit-log wrapped":   ceCat(ceWrap(0), ceWrap(1), ceRaw(2, 1)),
		"audit-log length":    ceCat(ceRaw(0, 1, 2, 3), ceWrap(1), ceRaw(2, 1)),
		"records primitive":   ceCat(ceOID(t, 0, ceAV), ceRaw(1, 0), ceRaw(2, 1)),
		"record shape":        ack(ceU(t, 0, 1), ceRaw(2, 1)),
		"record timestamp":    ack(ceU(t, 0, 1), ceWrap(1, ceWrap(0), ceWrap(1, logStatus))),
		"sequence empty":      ack(ceRaw(0), record(logStatus)),
		"no-more-items":       ceCat(ceOID(t, 0, ceAV), ceWrap(1), ceWrap(2)),
		"no-more-items value": ceCat(ceOID(t, 0, ceAV), ceWrap(1), ceRaw(2, 5)),
		"log-datum count":     ack(ceU(t, 0, 1), record(logStatus, logStatus)),
		"log-status bits":     ack(ceU(t, 0, 1), record(ceRaw(0, 9, 0))),
		"notification empty":  ack(ceU(t, 0, 1), record(ceWrap(1))),
		"time-change length":  ack(ceU(t, 0, 1), record(ceRaw(2, 0, 0))),
	}
	for name, raw := range cases {
		_, err := service.DecodeAuditLogQueryACK(raw, ceLimits)
		ceWant(t, name, err, bacnet.ErrMalformed)
	}

	_, err := service.EncodeAuditLogQueryACK(service.AuditLogQueryACK{AuditLog: ceBadOID})
	ceWant(t, "encode bad audit-log", err, bacnet.ErrMalformed)

	change := float32(-3.5)
	want := service.AuditLogQueryACK{
		AuditLog: ceAV,
		Records: []service.AuditLogRecordResult{{
			SequenceNumber: 9,
			Timestamp:      service.DateTime{Date: bacnet.Date{Year: 126, Month: 1, Day: 2, Weekday: 5}, Time: bacnet.Time{Hour: 12}},
			TimeChange:     &change,
		}},
		NoMoreItems: true,
	}
	raw, err := service.EncodeAuditLogQueryACK(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeAuditLogQueryACK(raw, ceLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("time-change round trip: got %+v, want %+v", got, want)
	}
}

func TestCoverageAuditLogQueryEncode(t *testing.T) {
	_, err := service.EncodeAuditLogQuery(service.AuditLogQueryRequest{AuditLog: ceBadOID})
	ceWant(t, "bad audit-log", err, bacnet.ErrMalformed)
	_, err = service.EncodeAuditLogQuery(service.AuditLogQueryRequest{
		AuditLog: ceAV, ByTarget: &service.AuditQueryByTarget{TargetDevice: ceBadOID},
	})
	ceWant(t, "bad target device", err, bacnet.ErrMalformed)

	ops := bacnet.BitString{UnusedBits: 0, Bytes: []byte{0x80, 0x01}}
	reqs := []service.AuditLogQueryRequest{
		{AuditLog: ceAV, RequestedCount: 4, ByTarget: &service.AuditQueryByTarget{
			TargetDevice: ceDev, TargetAddress: &service.AuditAddress{Network: 5, MAC: []byte{1, 2}},
		}},
		{AuditLog: ceAV, RequestedCount: 4, BySource: &service.AuditQueryBySource{
			SourceDevice: ceDev, Operations: &ops,
		}},
	}
	for i, want := range reqs {
		raw, err := service.EncodeAuditLogQuery(want)
		if err != nil {
			t.Fatalf("%d: %v", i, err)
		}
		got, err := service.DecodeAuditLogQuery(raw, ceLimits)
		if err != nil {
			t.Fatalf("%d: decode: %v", i, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%d: round trip: got %+v, want %+v", i, got, want)
		}
	}
}

func TestCoverageYouAreBadDevice(t *testing.T) {
	_, err := service.EncodeYouAre(service.YouAre{Device: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: bacnet.MaxObjectInstance + 1}})
	ceWant(t, "you-are device", err, bacnet.ErrMalformed)
}

// --- COVNotification -------------------------------------------------------

func TestCoverageCOVNotificationErrors(t *testing.T) {
	value := ceWrap(4, ceU(t, 0, 85), ceWrap(2, []byte{0x44, 0, 0, 0, 0}))
	fields := func() [][]byte {
		return [][]byte{ceU(t, 0, 1), ceOID(t, 1, ceDev), ceOID(t, 2, ceAV), ceU(t, 3, 60), value}
	}
	join := func(f [][]byte) []byte { return ceCat(f...) }
	if _, err := service.DecodeCOVNotification(join(fields()), ceLimits); err != nil {
		t.Fatalf("base: %v", err)
	}
	for i, f := range fields() {
		if i == 1 || i == 4 {
			continue // covered elsewhere
		}
		_, err := service.DecodeCOVNotification(ceCat(join(fields()), f), ceLimits)
		ceWant(t, "duplicate", err, bacnet.ErrMalformed)
	}
	replace := map[string]struct {
		i int
		v []byte
	}{
		"pid empty":      {0, ceRaw(0)},
		"object length":  {2, ceRaw(2, 1)},
		"time empty":     {3, ceRaw(3)},
		"unexpected tag": {4, ceRaw(5, 1)},
	}
	for name, c := range replace {
		f := fields()
		f[c.i] = c.v
		_, err := service.DecodeCOVNotification(join(f), ceLimits)
		ceWant(t, name, err, bacnet.ErrMalformed)
	}

	good := service.COVNotification{
		ProcessIdentifier: 1, InitiatingDevice: ceDev, MonitoredObject: ceAV, TimeRemaining: 60,
		Values: []service.PropertyValue{{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Value: bacnet.RealValue(1)}},
	}
	for name, mutate := range map[string]func(n *service.COVNotification){
		"initiating device": func(n *service.COVNotification) { n.InitiatingDevice = ceBadOID },
		"monitored object":  func(n *service.COVNotification) { n.MonitoredObject = ceBadOID },
		"value": func(n *service.COVNotification) {
			n.Values = []service.PropertyValue{{Value: bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: ceBadBits}}}
		},
	} {
		n := good
		mutate(&n)
		_, err := service.EncodeCOVNotification(n)
		ceWant(t, name, err, bacnet.ErrMalformed)
	}
	idx, prio := uint32(2), uint8(8)
	want := good
	want.Values = []service.PropertyValue{{
		Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &idx},
		Value:    bacnet.RealValue(4), Priority: &prio,
	}}
	raw, err := service.EncodeCOVNotification(want)
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeCOVNotification(raw, ceLimits)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("round trip: got %+v, want %+v", got, want)
	}
}

// --- ReadRange ---------------------------------------------------------------

func TestCoverageReadRangeErrors(t *testing.T) {
	obj, prop := ceOID(t, 0, ceAV), ceU(t, 1, 85)
	pos := ceWrap(3, []byte{0x21, 0x01, 0x31, 0x02})
	reqCases := map[string][]byte{
		"property 33 bit": ceCat(obj, ceU(t, 1, big32)),
		"index 33 bit":    ceCat(obj, prop, ceU(t, 2, big32)),
		"position twice":  ceCat(obj, prop, pos, pos),
		"sequence twice":  ceCat(obj, prop, pos, ceWrap(6, []byte{0x21, 0x01, 0x31, 0x02})),
		"time twice":      ceCat(obj, prop, pos, ceWrap(7, []byte{0xA4, 126, 1, 1, 1, 0xB4, 0, 0, 0, 0, 0x31, 0x02})),
		"count 33 bit":    ceCat(obj, prop, ceWrap(3, []byte{0x21, 0x01, 0x35, 0x05, 0x01, 0x00, 0x00, 0x00, 0x00})),
	}
	for name, raw := range reqCases {
		_, err := service.DecodeReadRange(raw, ceLimits)
		ceWant(t, name, err, bacnet.ErrMalformed)
	}
	_, err := service.EncodeReadRange(service.ReadRangeRequest{Object: ceBadOID})
	ceWant(t, "encode bad object", err, bacnet.ErrMalformed)

	flags := ceRaw(3, 5, 0xE0)
	count := ceU(t, 4, 1)
	data := ceWrap(5, []byte{0x21, 0x07})
	ackOK := ceCat(obj, prop, flags, count, data)
	if _, err := service.DecodeReadRangeACK(ackOK, ceLimits); err != nil {
		t.Fatalf("ack base: %v", err)
	}
	ackCases := map[string][]byte{
		"duplicate property": ceCat(obj, prop, prop, flags, count, data),
		"property 33 bit":    ceCat(obj, ceU(t, 1, big32), flags, count, data),
		"duplicate index":    ceCat(obj, prop, ceU(t, 2, 1), ceU(t, 2, 1), flags, count, data),
		"index 33 bit":       ceCat(obj, prop, ceU(t, 2, big32), flags, count, data),
		"flags shape":        ceCat(obj, prop, ceRaw(3, 0, 0xE0), count, data),
		"duplicate count":    ceCat(obj, prop, flags, count, count, data),
		"duplicate data":     ceCat(obj, prop, flags, count, data, data),
		"first seq 33 bit":   ceCat(ackOK, ceU(t, 6, big32)),
		"unexpected tag":     ceCat(ackOK, ceU(t, 7, 1)),
		"object length":      ceCat(ceRaw(0, 1), prop, flags, count, data),
		"zero count data":    ceCat(obj, prop, flags, ceU(t, 4, 0), data),
	}
	for name, raw := range ackCases {
		_, err := service.DecodeReadRangeACK(raw, ceLimits)
		ceWant(t, name, err, bacnet.ErrMalformed)
	}
	for name, ack := range map[string]service.ReadRangeACK{
		"object": {Object: ceBadOID},
		"flags":  {Object: ceAV, ResultFlags: ceBadBits},
		"item":   {Object: ceAV, ResultFlags: ceFlags, ItemData: []bacnet.ApplicationValue{{Kind: bacnet.ValueBitString, BitString: ceBadBits}}},
	} {
		_, err := service.EncodeReadRangeACK(ack)
		ceWant(t, "encode ack "+name, err, bacnet.ErrMalformed)
	}
}

// --- VT-Data ACK, Text Message, Life Safety ----------------------------------

func TestCoverageVTAndMessagingErrors(t *testing.T) {
	cases := map[string][]byte{
		"accepted not boolean": ceRaw(0, 5),
		"count 33 bit":         ceCat(ceRaw(0, 0), ceU(t, 1, big32)),
	}
	for name, raw := range cases {
		_, err := service.DecodeVTDataACK(raw, ceLimits)
		ceWant(t, name, err, bacnet.ErrMalformed)
	}
	if _, err := service.DecodeVTDataACK([]byte{0x0E}, ceLimits); err == nil {
		t.Error("VT-Data ACK: unterminated payload decoded")
	}
	_, err := service.EncodeTextMessage(service.TextMessage{TextMessageSourceDevice: ceBadOID})
	ceWant(t, "text message device", err, bacnet.ErrMalformed)
	_, err = service.EncodeLifeSafetyOperation(service.LifeSafetyOperationRequest{Object: &ceBadOID})
	ceWant(t, "life safety object", err, bacnet.ErrMalformed)
}
