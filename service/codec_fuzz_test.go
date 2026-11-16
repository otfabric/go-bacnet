// SPDX-License-Identifier: MIT

package service_test

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"unsafe"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Every decoder of a service message, with its encoder, in two tables: what
// a client decodes (acknowledgements and the unconfirmed messages devices
// send) and what a server decodes (requests). FuzzDecodeAcknowledgements
// and FuzzDecodeRequests run any input through any of them and check, for
// an input the decoder accepts:
//
//   - ownership: the result does not change when the input buffer does;
//   - the result encodes (unless the table says the encoder is stricter);
//   - the encoding decodes, and encodes to the same octets again.
//
// A decoder added to package service gets a line here; TestCodecTables
// fails when an exported Decode function of the listed shape is missing.

type codec struct {
	name   string
	decode func([]byte, bacnet.DecodeLimits) (any, error)
	encode func(any) ([]byte, error)
	// stricter, when set, says which decoded values the encoder refuses
	// (the decoder accepts them on purpose). A value that does not encode
	// ends the check; the round trip is still checked when it encodes.
	stricter string
}

func stricter(why string, c codec) codec { c.stricter = why; return c }

func pair[T any](name string, dec func([]byte, bacnet.DecodeLimits) (T, error), enc func(T) ([]byte, error)) codec {
	return codec{
		name:   name,
		decode: func(b []byte, l bacnet.DecodeLimits) (any, error) { return dec(b, l) },
		encode: func(v any) ([]byte, error) { return enc(v.(T)) },
	}
}

var acknowledgementCodecs = []codec{
	pair("ReadPropertyACK", service.DecodeReadPropertyACK, service.EncodeReadPropertyACK),
	pair("ReadPropertyMultipleACK", service.DecodeReadPropertyMultipleACK, service.EncodeReadPropertyMultipleACK),
	pair("ReadRangeACK", service.DecodeReadRangeACK, service.EncodeReadRangeACK),
	pair("AtomicReadFileACK", service.DecodeAtomicReadFileACK, service.EncodeAtomicReadFileACK),
	pair("AtomicWriteFileACK", service.DecodeAtomicWriteFileACK, service.EncodeAtomicWriteFileACK),
	pair("CreateObjectACK", service.DecodeCreateObjectACK, service.EncodeCreateObjectACK),
	pair("GetAlarmSummaryACK", service.DecodeGetAlarmSummaryACK, service.EncodeGetAlarmSummaryACK),
	pair("GetEnrollmentSummaryACK", service.DecodeGetEnrollmentSummaryACK, service.EncodeGetEnrollmentSummaryACK),
	pair("GetEventInformationACK", service.DecodeGetEventInformationACK, service.EncodeGetEventInformationACK),
	pair("AuditLogQueryACK", service.DecodeAuditLogQueryACK, service.EncodeAuditLogQueryACK),
	pair("VTOpenACK", service.DecodeVTOpenACK, service.EncodeVTOpenACK),
	pair("VTDataACK", service.DecodeVTDataACK, service.EncodeVTDataACK),
	pair("WritePropertyMultipleError", service.DecodeWritePropertyMultipleError, func(e *service.WritePropertyMultipleError) ([]byte, error) {
		return service.EncodeWritePropertyMultipleError(*e)
	}),
	stricter("a notification without values decodes, pending a decision; the encoder asks for one",
		pair("COVNotification", service.DecodeCOVNotification, service.EncodeCOVNotification)),
	pair("COVNotificationMultiple", service.DecodeCOVNotificationMultiple, service.EncodeCOVNotificationMultiple),
	pair("EventNotification", service.DecodeEventNotification, service.EncodeEventNotification),
	pair("AuditNotificationRequest", service.DecodeAuditNotificationRequest, service.EncodeAuditNotificationRequest),
	pair("IAm", service.DecodeIAm, service.EncodeIAm),
	pair("IHave", service.DecodeIHave, service.EncodeIHave),
	pair("YouAre", service.DecodeYouAre, service.EncodeYouAre),
}

var requestCodecs = []codec{
	pair("ReadProperty", service.DecodeReadProperty, service.EncodeReadProperty),
	pair("ReadPropertyMultiple", service.DecodeReadPropertyMultiple, service.EncodeReadPropertyMultiple),
	pair("ReadRange", service.DecodeReadRange, service.EncodeReadRange),
	pair("WriteProperty", service.DecodeWriteProperty, service.EncodeWriteProperty),
	pair("WritePropertyMultiple", service.DecodeWritePropertyMultiple, service.EncodeWritePropertyMultiple),
	pair("SubscribeCOV", service.DecodeSubscribeCOV, service.EncodeSubscribeCOV),
	pair("SubscribeCOVProperty", service.DecodeSubscribeCOVProperty, service.EncodeSubscribeCOVProperty),
	pair("SubscribeCOVPropertyMultiple", service.DecodeSubscribeCOVPropertyMultiple, service.EncodeSubscribeCOVPropertyMultiple),
	pair("AtomicReadFile", service.DecodeAtomicReadFile, service.EncodeAtomicReadFile),
	pair("AtomicWriteFile", service.DecodeAtomicWriteFile, service.EncodeAtomicWriteFile),
	pair("CreateObject", service.DecodeCreateObject, service.EncodeCreateObject),
	pair("DeleteObject", service.DecodeDeleteObject, service.EncodeDeleteObject),
	stricter("an empty list of elements decodes, pending a decision; the encoder asks for an element",
		pair("ListElementRequest", service.DecodeListElementRequest, service.EncodeListElementRequest)),
	pair("AcknowledgeAlarm", service.DecodeAcknowledgeAlarm, service.EncodeAcknowledgeAlarm),
	pair("GetEnrollmentSummary", service.DecodeGetEnrollmentSummary, service.EncodeGetEnrollmentSummary),
	pair("GetEventInformation", service.DecodeGetEventInformation, service.EncodeGetEventInformation),
	pair("GetAlarmSummary", func(b []byte, l bacnet.DecodeLimits) (struct{}, error) {
		return struct{}{}, service.DecodeGetAlarmSummary(b, l)
	}, func(struct{}) ([]byte, error) { return service.EncodeGetAlarmSummary() }),
	pair("DeviceCommunicationControl", service.DecodeDeviceCommunicationControl, service.EncodeDeviceCommunicationControl),
	pair("ReinitializeDevice", service.DecodeReinitializeDevice, service.EncodeReinitializeDevice),
	pair("LifeSafetyOperation", service.DecodeLifeSafetyOperation, service.EncodeLifeSafetyOperation),
	pair("AuditLogQuery", service.DecodeAuditLogQuery, service.EncodeAuditLogQuery),
	pair("VTOpen", service.DecodeVTOpen, service.EncodeVTOpen),
	pair("VTData", service.DecodeVTData, service.EncodeVTData),
	pair("AuthRequest", service.DecodeAuthRequest, service.EncodeAuthRequest),
	pair("PrivateTransfer", service.DecodePrivateTransfer, service.EncodePrivateTransfer),
	pair("TextMessage", service.DecodeTextMessage, service.EncodeTextMessage),
	pair("TimeSynchronization", service.DecodeTimeSynchronization, service.EncodeTimeSynchronization),
	pair("WriteGroup", service.DecodeWriteGroup, service.EncodeWriteGroup),
	pair("WhoIs", service.DecodeWhoIs, service.EncodeWhoIs),
	pair("WhoHas", service.DecodeWhoHas, service.EncodeWhoHas),
	pair("WhoAmI", service.DecodeWhoAmI, service.EncodeWhoAmI),
}

// same reports whether two decoded values are equal, with a NaN equal to
// the NaN of the same bits (reflect.DeepEqual has no NaN equal to itself).
func same(a, b reflect.Value) bool {
	if a.IsValid() != b.IsValid() {
		return false
	}
	if !a.IsValid() {
		return true
	}
	if a.Type() != b.Type() {
		return false
	}
	switch a.Kind() {
	case reflect.Float32, reflect.Float64:
		return math.Float64bits(a.Float()) == math.Float64bits(b.Float())
	case reflect.Pointer, reflect.Interface:
		if a.IsNil() || b.IsNil() {
			return a.IsNil() == b.IsNil()
		}
		return same(a.Elem(), b.Elem())
	case reflect.Struct:
		for i := range a.NumField() {
			if !same(a.Field(i), b.Field(i)) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		if a.Kind() == reflect.Slice && a.IsNil() != b.IsNil() {
			return false
		}
		if a.Len() != b.Len() {
			return false
		}
		for i := range a.Len() {
			if !same(a.Index(i), b.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Map:
		if a.Len() != b.Len() {
			return false
		}
		for _, k := range a.MapKeys() {
			if !same(a.MapIndex(k), b.MapIndex(k)) {
				return false
			}
		}
		return true
	default:
		return reflect.DeepEqual(a.Interface(), b.Interface())
	}
}

// checkCodec runs the invariants of c on one input.
func checkCodec(t *testing.T, c codec, data []byte) {
	t.Helper()
	limits := bacnet.DefaultDecodeLimits()
	input := append([]byte(nil), data...)
	v, err := c.decode(input, limits)
	if err != nil {
		return
	}
	// Ownership: a second decode of a pristine copy, then the first input is
	// overwritten; the first result is still the value of the second.
	want, err := c.decode(append([]byte(nil), data...), limits)
	if err != nil {
		t.Fatalf("%s: the same input decodes once and fails once: %v", c.name, err)
	}
	for i := range input {
		input[i] ^= 0xFF
	}
	if !same(reflect.ValueOf(v), reflect.ValueOf(want)) {
		t.Fatalf("%s: the decoded value changed with the input buffer\ninput % x\nnow  %+v\nwant %+v", c.name, data, v, want)
	}
	first, err := c.encode(v)
	if err != nil {
		if c.stricter == "" {
			t.Fatalf("%s: decoded value does not encode: %v\ninput % x\nvalue %+v", c.name, err, data, v)
		}
		return
	}
	again, err := c.decode(append([]byte(nil), first...), limits)
	if err != nil {
		if errors.Is(err, bacnet.ErrLimitExceeded) {
			return // the standard form of the value is larger than the input was
		}
		t.Fatalf("%s: the encoding of a decoded value does not decode: %v\ninput   % x\nencoded % x", c.name, err, data, first)
	}
	second, err := c.encode(again)
	if err != nil {
		t.Fatalf("%s: the value decoded from an encoding does not encode: %v\nencoded % x", c.name, err, first)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("%s: encode(decode(x)) is not stable\ninput  % x\nfirst  % x\nsecond % x", c.name, data, first, second)
	}
}

// codecSeeds are well-formed messages of several services, and the inputs
// that earlier releases decoded differently. Each is tried on every decoder
// of a table: most refuse most of them, which is the point.
func codecSeeds(t testing.TB) [][]byte {
	t.Helper()
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 100}
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	idx := uint32(2)
	indexed := bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &idx}
	prio := uint8(8)
	stamp := service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 4}
	lo, hi := uint32(1), uint32(100)
	seeds := [][]byte{nil, {0x00}, recordedTLM, recordedEventLog,
		fromHex("11" + fileStream), fromHex("11" + fileRecord),
		fromHex(rpmObject + "1e" + "2955" + "3901" + rpmValue + "294d" + rpmError + "1f"),
		fromHex(covmHead + covmStamp + "4e" + covmObject + "1e" + "0955" + "1901" + covmValue + "3c0a000000" + "1f" + "4f"),
	}
	for _, enc := range []func() ([]byte, error){
		func() ([]byte, error) {
			return service.EncodeReadProperty(service.ReadPropertyRequest{Object: av, Property: indexed})
		},
		func() ([]byte, error) {
			return service.EncodeReadPropertyACK(service.ReadPropertyACK{Object: av, Property: pv, Value: bacnet.RealValue(21.5)})
		},
		func() ([]byte, error) {
			return service.EncodeReadPropertyMultiple([]service.ReadAccessSpecification{{Object: av, Properties: []bacnet.PropertyReference{pv, indexed}}})
		},
		func() ([]byte, error) {
			return service.EncodeWriteProperty(service.WritePropertyRequest{Object: av, Property: pv, Value: bacnet.RealValue(1), Priority: &prio})
		},
		func() ([]byte, error) {
			return service.EncodeWritePropertyMultiple([]service.WriteAccessSpecification{{Object: av, Properties: []service.WritePropertyValue{{Property: pv, Value: bacnet.RealValue(1), Priority: &prio}}}})
		},
		func() ([]byte, error) {
			return service.EncodeSubscribeCOV(service.SubscribeCOVRequest{ProcessIdentifier: 1, MonitoredObject: av, IssueConfirmed: true, Lifetime: 60})
		},
		func() ([]byte, error) {
			return service.EncodeCOVNotification(service.COVNotification{ProcessIdentifier: 1, InitiatingDevice: dev, MonitoredObject: av, TimeRemaining: 30,
				Values: []service.PropertyValue{{Property: pv, Value: bacnet.RealValue(2)}}})
		},
		func() ([]byte, error) {
			return service.EncodeAcknowledgeAlarm(service.AcknowledgeAlarmRequest{ProcessIdentifier: 1, EventObject: av, EventStateAcked: 3, TimeStamp: stamp,
				AcknowledgmentSource: bacnet.CharacterString{Value: "op"}, TimeOfAcknowledgment: stamp})
		},
		func() ([]byte, error) {
			return service.EncodeEventNotification(service.EventNotification{ProcessIdentifier: 1, InitiatingDevice: dev, EventObject: av, TimeStamp: stamp,
				NotificationClass: 1, Priority: 100, EventType: 5, NotifyType: 2, ToState: 0})
		},
		func() ([]byte, error) {
			return service.EncodeIAm(service.IAm{Device: dev, MaxAPDULength: 1476, VendorID: 7})
		},
		func() ([]byte, error) { return service.EncodeWhoIs(service.WhoIs{LowLimit: &lo, HighLimit: &hi}) },
		func() ([]byte, error) { return service.EncodeDeleteObject(service.DeleteObjectRequest{Object: av}) },
		func() ([]byte, error) {
			return service.EncodeCreateObject(service.CreateObjectRequest{ObjectIdentifier: &av})
		},
		func() ([]byte, error) {
			return service.EncodeAtomicReadFile(service.AtomicReadFileRequest{File: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}, Count: 10})
		},
	} {
		b, err := enc()
		if err != nil {
			t.Fatal(err)
		}
		seeds = append(seeds, b)
	}
	return seeds
}

func fuzzCodecs(f *testing.F, table []codec) {
	for _, seed := range codecSeeds(f) {
		for i := range table {
			f.Add(uint8(i), seed)
		}
	}
	f.Fuzz(func(t *testing.T, which uint8, data []byte) {
		checkCodec(t, table[int(which)%len(table)], data)
	})
}

// FuzzDecodeAcknowledgements runs the invariants of checkCodec over what a
// client decodes.
func FuzzDecodeAcknowledgements(f *testing.F) { fuzzCodecs(f, acknowledgementCodecs) }

// FuzzDecodeRequests runs the invariants of checkCodec over what a server
// decodes.
func FuzzDecodeRequests(f *testing.F) { fuzzCodecs(f, requestCodecs) }

// At the element limit no decoder reports the limit; one element more is
// refused by every decoder (by ErrLimitExceeded where it parses the whole
// message first), and refusing it costs the memory of the elements parsed
// up to the limit, nothing built from them.
func TestDecodersAtElementLimit(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	if limits.MaxElements <= 0 {
		t.Skip("no element limit in the default limits")
	}
	// The cheapest element: an application Null, one octet.
	payload := func(n int) []byte { return make([]byte, n) }
	// The parser appends to a slice of elements: its final size plus the
	// copies made while it grew stay under five times the final size (about
	// 3.5 times where an int has 64 bits and 4.2 times where it has 32, as
	// append rounds each step up to a size class).
	budget := uint64(5 * (limits.MaxElements + 1) * int(unsafe.Sizeof(bacnet.Element{})))
	for _, table := range [][]codec{acknowledgementCodecs, requestCodecs} {
		for _, c := range table {
			t.Run(c.name, func(t *testing.T) {
				for _, n := range []int{limits.MaxElements - 1, limits.MaxElements} {
					if _, err := c.decode(payload(n), limits); errors.Is(err, bacnet.ErrLimitExceeded) {
						t.Fatalf("%d elements: %v, want no limit error", n, err)
					}
				}
				over := payload(limits.MaxElements + 1)
				var before, after runtime.MemStats
				runtime.ReadMemStats(&before)
				_, err := c.decode(over, limits)
				runtime.ReadMemStats(&after)
				if err == nil {
					t.Fatalf("%d elements accepted", len(over))
				}
				if got := after.TotalAlloc - before.TotalAlloc; got > budget {
					t.Fatalf("refusing %d elements allocated %d octets, budget %d", len(over), got, budget)
				}
			})
		}
	}
}

// The seeds pass the invariants on every decoder (what the fuzz targets
// start from), and every exported decoder of a whole message, a function
// Decode…(payload []byte, limits bacnet.DecodeLimits), is in a table.
func TestCodecTables(t *testing.T) {
	listed := map[string]bool{}
	for _, table := range [][]codec{acknowledgementCodecs, requestCodecs} {
		for _, c := range table {
			if listed[c.name] {
				t.Errorf("%s is listed twice", c.name)
			}
			listed[c.name] = true
			for _, seed := range codecSeeds(t) {
				checkCodec(t, c, seed)
			}
		}
	}
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(token.NewFileSet(), entry.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "Decode") || fn.Type.Params.NumFields() != 2 {
				continue
			}
			arr, ok := fn.Type.Params.List[0].Type.(*ast.ArrayType)
			if !ok || arr.Len != nil {
				continue // not a payload decoder (a single element)
			}
			if elt, ok := arr.Elt.(*ast.Ident); !ok || elt.Name != "byte" {
				continue // decodes elements, not a payload
			}
			found++
			if name := strings.TrimPrefix(fn.Name.Name, "Decode"); !listed[name] {
				t.Errorf("%s has no line in acknowledgementCodecs or requestCodecs", fn.Name.Name)
			}
		}
	}
	if found != len(listed) {
		t.Errorf("%d payload decoders in the package, %d lines in the tables", found, len(listed))
	}
}
