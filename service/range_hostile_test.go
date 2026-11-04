// SPDX-License-Identifier: MIT

package service_test

import (
	"errors"
	"math"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Boundary tests for every decoder that narrows a decoded Unsigned or Signed
// into a smaller Go integer. A value one past the range of the destination
// type must be rejected with an error; it must never wrap silently. Values at
// the edge must decode exactly when they decode at all.

var rangeLimits = bacnet.DefaultDecodeLimits()

// rangeCase is one boundary input. decode returns the narrowed field widened
// back to 64 bits, so a wrapped value is visible.
type rangeCase struct {
	name    string
	payload func(t *testing.T) []byte
	decode  func(payload []byte) (int64, error)
	want    int64 // the exact value expected when the decoder accepts
	reject  bool  // the decoder must return an error
	// mayReject: the decoder may refuse an in-range value for another reason
	// (for example a count that does not match the records), but if it
	// accepts it must not truncate.
	mayReject bool
}

func runRangeCases(t *testing.T, cases []rangeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := tc.payload(t)
			got, err := tc.decode(p)
			switch {
			case tc.reject:
				if err == nil {
					t.Fatalf("out-of-range value accepted, decoded as %d (payload % x)", got, p)
				}
			case err != nil:
				if !tc.mayReject {
					t.Fatalf("in-range value rejected: %v (payload % x)", err, p)
				}
			case got != tc.want:
				t.Fatalf("decoded %d, want %d (payload % x)", got, tc.want, p)
			}
		})
	}
}

// must panics on an encoder error: the inputs are fixed by the test, so a
// failure is a bug in the test itself.
func must(b []byte, err error) []byte {
	if err != nil {
		panic(err)
	}
	return b
}

func appValue(t *testing.T, dst []byte, v bacnet.ApplicationValue) []byte {
	t.Helper()
	return must(bacnet.AppendApplicationValue(dst, v))
}

func ctxUnsigned(t *testing.T, dst []byte, tag uint8, v uint64) []byte {
	t.Helper()
	return must(bacnet.AppendContextUnsigned(dst, tag, v))
}

func ctxTagged(t *testing.T, dst []byte, tag uint8, body ...bacnet.ApplicationValue) []byte {
	t.Helper()
	els := make([]bacnet.Element, len(body))
	for i, v := range body {
		els[i] = bacnet.Element{Value: v}
	}
	return must(bacnet.AppendContextTagged(dst, tag, els))
}

func octets(b ...byte) bacnet.ApplicationValue {
	return bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: b}
}

var fileObject = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}

const (
	maxU8  = math.MaxUint8
	maxU16 = math.MaxUint16
	maxU32 = math.MaxUint32
	maxI32 = math.MaxInt32
	minI32 = math.MinInt32
)

func TestRangeVTSessionIdentifiers(t *testing.T) {
	vtOpenACK := func(sid uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte { return appValue(t, nil, bacnet.UnsignedValue(sid)) }
	}
	decodeOpenACK := func(p []byte) (int64, error) {
		ack, err := service.DecodeVTOpenACK(p, rangeLimits)
		return int64(ack.RemoteVTSessionIdentifier), err
	}
	vtOpen := func(sid uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			return appValue(t, appValue(t, nil, bacnet.EnumValue(0)), bacnet.UnsignedValue(sid))
		}
	}
	decodeOpen := func(p []byte) (int64, error) {
		req, err := service.DecodeVTOpen(p, rangeLimits)
		return int64(req.LocalVTSessionIdentifier), err
	}
	vtData := func(sid, flag uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			b := appValue(t, nil, bacnet.UnsignedValue(sid))
			b = appValue(t, b, octets('x'))
			return appValue(t, b, bacnet.UnsignedValue(flag))
		}
	}
	decodeDataSID := func(p []byte) (int64, error) {
		req, err := service.DecodeVTData(p, rangeLimits)
		return int64(req.VTSessionIdentifier), err
	}
	decodeDataFlag := func(p []byte) (int64, error) {
		req, err := service.DecodeVTData(p, rangeLimits)
		return int64(req.VTDataFlag), err
	}
	runRangeCases(t, []rangeCase{
		{name: "VTOpenACK/255", payload: vtOpenACK(maxU8), decode: decodeOpenACK, want: maxU8},
		{name: "VTOpenACK/256", payload: vtOpenACK(maxU8 + 1), decode: decodeOpenACK, reject: true},
		{name: "VTOpenACK/2^32+1", payload: vtOpenACK(1<<32 + 1), decode: decodeOpenACK, reject: true},
		{name: "VTOpen/255", payload: vtOpen(maxU8), decode: decodeOpen, want: maxU8},
		{name: "VTOpen/256", payload: vtOpen(maxU8 + 1), decode: decodeOpen, reject: true},
		{name: "VTData/session255", payload: vtData(maxU8, 1), decode: decodeDataSID, want: maxU8},
		{name: "VTData/session256", payload: vtData(maxU8+1, 1), decode: decodeDataSID, reject: true},
		{name: "VTData/flag256", payload: vtData(1, maxU8+1), decode: decodeDataFlag, reject: true},
	})
}

func TestRangeAtomicReadFileACK(t *testing.T) {
	stream := func(start int64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			b := appValue(t, nil, bacnet.BoolValue(true))
			return ctxTagged(t, b, 0, bacnet.SignedValue(start), octets(1))
		}
	}
	record := func(start int64, count uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			b := appValue(t, nil, bacnet.BoolValue(true))
			return ctxTagged(t, b, 1, bacnet.SignedValue(start), bacnet.UnsignedValue(count), octets(1))
		}
	}
	start := func(p []byte) (int64, error) {
		ack, err := service.DecodeAtomicReadFileACK(p, rangeLimits)
		return int64(ack.StartPosition), err
	}
	count := func(p []byte) (int64, error) {
		ack, err := service.DecodeAtomicReadFileACK(p, rangeLimits)
		return int64(ack.RecordCount), err
	}
	runRangeCases(t, []rangeCase{
		{name: "stream/start=MaxInt32", payload: stream(maxI32), decode: start, want: maxI32},
		{name: "stream/start=MaxInt32+1", payload: stream(maxI32 + 1), decode: start, reject: true},
		{name: "stream/start=MinInt32", payload: stream(minI32), decode: start, want: minI32},
		{name: "stream/start=MinInt32-1", payload: stream(minI32 - 1), decode: start, reject: true},
		{name: "stream/start=2^40", payload: stream(1 << 40), decode: start, reject: true},
		{name: "record/start=MaxInt32+1", payload: record(maxI32+1, 1), decode: start, reject: true},
		{name: "record/start=MinInt32-1", payload: record(minI32-1, 1), decode: start, reject: true},
		{name: "record/count=1", payload: record(0, 1), decode: count, want: 1},
		{name: "record/count=MaxUint32", payload: record(0, maxU32), decode: count, want: maxU32, mayReject: true},
		{name: "record/count=2^32", payload: record(0, maxU32+1), decode: count, reject: true},
		{name: "record/count=2^32+1", payload: record(0, maxU32+2), decode: count, reject: true},
	})
}

func TestRangeAtomicWriteFileACK(t *testing.T) {
	ack := func(tag uint8, pos int64) func(*testing.T) []byte {
		return func(t *testing.T) []byte { return must(bacnet.AppendContextSigned(nil, tag, pos)) }
	}
	decode := func(p []byte) (int64, error) {
		a, err := service.DecodeAtomicWriteFileACK(p, rangeLimits)
		return int64(a.StartPosition), err
	}
	runRangeCases(t, []rangeCase{
		{name: "stream/MaxInt32", payload: ack(0, maxI32), decode: decode, want: maxI32},
		{name: "stream/MaxInt32+1", payload: ack(0, maxI32+1), decode: decode, reject: true},
		{name: "stream/MinInt32", payload: ack(0, minI32), decode: decode, want: minI32},
		{name: "stream/MinInt32-1", payload: ack(0, minI32-1), decode: decode, reject: true},
		{name: "record/MaxInt32+1", payload: ack(1, maxI32+1), decode: decode, reject: true},
	})
}

func TestRangeAtomicFileRequests(t *testing.T) {
	read := func(tag uint8, start int64, n uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			b := appValue(t, nil, bacnet.ObjectIDValue(fileObject))
			return ctxTagged(t, b, tag, bacnet.SignedValue(start), bacnet.UnsignedValue(n))
		}
	}
	readStart := func(p []byte) (int64, error) {
		r, err := service.DecodeAtomicReadFile(p, rangeLimits)
		return int64(r.StartPosition), err
	}
	readCount := func(p []byte) (int64, error) {
		r, err := service.DecodeAtomicReadFile(p, rangeLimits)
		return int64(r.Count), err
	}
	writeStream := func(start int64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			b := appValue(t, nil, bacnet.ObjectIDValue(fileObject))
			return ctxTagged(t, b, 0, bacnet.SignedValue(start), octets(1))
		}
	}
	writeRecord := func(start int64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			b := appValue(t, nil, bacnet.ObjectIDValue(fileObject))
			return ctxTagged(t, b, 1, bacnet.SignedValue(start), bacnet.UnsignedValue(1), octets(1))
		}
	}
	writeStart := func(p []byte) (int64, error) {
		r, err := service.DecodeAtomicWriteFile(p, rangeLimits)
		return int64(r.StartPosition), err
	}
	runRangeCases(t, []rangeCase{
		{name: "read/stream/start=MaxInt32", payload: read(0, maxI32, 1), decode: readStart, want: maxI32},
		{name: "read/stream/start=MaxInt32+1", payload: read(0, maxI32+1, 1), decode: readStart, reject: true},
		{name: "read/stream/start=MinInt32-1", payload: read(0, minI32-1, 1), decode: readStart, reject: true},
		{name: "read/record/start=MaxInt32+1", payload: read(1, maxI32+1, 1), decode: readStart, reject: true},
		{name: "read/count=MaxUint32", payload: read(0, 0, maxU32), decode: readCount, want: maxU32},
		{name: "read/count=2^32", payload: read(0, 0, maxU32+1), decode: readCount, reject: true},
		{name: "write/stream/start=MaxInt32", payload: writeStream(maxI32), decode: writeStart, want: maxI32},
		{name: "write/stream/start=MaxInt32+1", payload: writeStream(maxI32 + 1), decode: writeStart, reject: true},
		{name: "write/stream/start=MinInt32-1", payload: writeStream(minI32 - 1), decode: writeStart, reject: true},
		{name: "write/record/start=MaxInt32+1", payload: writeRecord(maxI32 + 1), decode: writeStart, reject: true},
		{name: "write/record/start=MinInt32-1", payload: writeRecord(minI32 - 1), decode: writeStart, reject: true},
	})
}

func TestRangeMessagingPriorities(t *testing.T) {
	src := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	text := func(prio uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			b := must(bacnet.AppendContextObjectID(nil, 0, src))
			b = ctxUnsigned(t, b, 2, prio)
			return must(bacnet.AppendContextCharacterString(b, 3, bacnet.CharacterString{Value: "hi"}))
		}
	}
	textPrio := func(p []byte) (int64, error) {
		m, err := service.DecodeTextMessage(p, rangeLimits)
		return int64(m.MessagePriority), err
	}
	group := func(prio uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			b := ctxUnsigned(t, nil, 0, 1)
			b = ctxUnsigned(t, b, 1, prio)
			return ctxTagged(t, b, 2, bacnet.UnsignedValue(0))
		}
	}
	groupPrio := func(p []byte) (int64, error) {
		w, err := service.DecodeWriteGroup(p, rangeLimits)
		return int64(w.WritePriority), err
	}
	privVendor := func(vendor uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			return ctxUnsigned(t, ctxUnsigned(t, nil, 0, vendor), 1, 1)
		}
	}
	privDecode := func(p []byte) (int64, error) {
		pt, err := service.DecodePrivateTransfer(p, rangeLimits)
		return int64(pt.VendorID), err
	}
	runRangeCases(t, []rangeCase{
		{name: "TextMessage/priority=1", payload: text(1), decode: textPrio, want: 1},
		{name: "TextMessage/priority=256", payload: text(maxU8 + 1), decode: textPrio, reject: true},
		{name: "TextMessage/priority=2^32+1", payload: text(1<<32 + 1), decode: textPrio, reject: true},
		{name: "WriteGroup/priority=16", payload: group(16), decode: groupPrio, want: 16},
		{name: "WriteGroup/priority=256", payload: group(maxU8 + 1), decode: groupPrio, reject: true},
		{name: "WriteGroup/priority=257", payload: group(maxU8 + 2), decode: groupPrio, reject: true},
		{name: "PrivateTransfer/vendor=65535", payload: privVendor(maxU16), decode: privDecode, want: maxU16},
		{name: "PrivateTransfer/vendor=65536", payload: privVendor(maxU16 + 1), decode: privDecode, reject: true},
	})
}

func TestRangeExtendedNotificationVendor(t *testing.T) {
	params := func(vendor uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			b := must(bacnet.AppendTag(nil, bacnet.Element{Context: true, TagNumber: 9, Opening: true}))
			b = ctxUnsigned(t, b, 0, vendor)
			b = ctxUnsigned(t, b, 1, 7)
			return must(bacnet.AppendTag(b, bacnet.Element{Context: true, TagNumber: 9, Closing: true}))
		}
	}
	decode := func(p []byte) (int64, error) {
		els, _, err := bacnet.ParseSequence(p, rangeLimits, -1)
		if err != nil {
			return 0, err
		}
		np, err := service.DecodeNotificationParameters(els)
		if err != nil {
			return 0, err
		}
		if np.Extended == nil {
			return 0, nil
		}
		return int64(np.Extended.VendorID), nil
	}
	runRangeCases(t, []rangeCase{
		{name: "vendor=65535", payload: params(maxU16), decode: decode, want: maxU16},
		{name: "vendor=65536", payload: params(maxU16 + 1), decode: decode, reject: true},
		{name: "vendor=2^32", payload: params(1 << 32), decode: decode, reject: true},
	})
}

// TestRangeGuardedDecoders pins the decoders that already range-check, so a
// refactor cannot drop the guard.
func TestRangeGuardedDecoders(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	iam := func(maxAPDU, vendor uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			b := appValue(t, nil, bacnet.ObjectIDValue(dev))
			b = appValue(t, b, bacnet.UnsignedValue(maxAPDU))
			b = appValue(t, b, bacnet.EnumValue(3))
			return appValue(t, b, bacnet.UnsignedValue(vendor))
		}
	}
	iamAPDU := func(p []byte) (int64, error) {
		m, err := service.DecodeIAm(p, rangeLimits)
		return int64(m.MaxAPDULength), err
	}
	iamVendor := func(p []byte) (int64, error) {
		m, err := service.DecodeIAm(p, rangeLimits)
		return int64(m.VendorID), err
	}
	whoIs := func(lo, hi uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte { return ctxUnsigned(t, ctxUnsigned(t, nil, 0, lo), 1, hi) }
	}
	whoIsHigh := func(p []byte) (int64, error) {
		w, err := service.DecodeWhoIs(p, rangeLimits)
		if err != nil || w.HighLimit == nil {
			return 0, err
		}
		return int64(*w.HighLimit), nil
	}
	dcc := func(d uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte { return must(bacnet.AppendContextUnsigned(ctxUnsigned(t, nil, 0, d), 1, 0)) }
	}
	dccDecode := func(p []byte) (int64, error) {
		r, err := service.DecodeDeviceCommunicationControl(p, rangeLimits)
		if err != nil || r.TimeDuration == nil {
			return 0, err
		}
		return int64(*r.TimeDuration), nil
	}
	sub := func(pid uint64) func(*testing.T) []byte {
		return func(t *testing.T) []byte {
			b := ctxUnsigned(t, nil, 0, pid)
			return must(bacnet.AppendContextObjectID(b, 1, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}))
		}
	}
	subPID := func(p []byte) (int64, error) {
		r, err := service.DecodeSubscribeCOV(p, rangeLimits)
		return int64(r.ProcessIdentifier), err
	}
	runRangeCases(t, []rangeCase{
		{name: "IAm/maxAPDU=65535", payload: iam(maxU16, 1), decode: iamAPDU, want: maxU16},
		{name: "IAm/maxAPDU=65536", payload: iam(maxU16+1, 1), decode: iamAPDU, reject: true},
		{name: "IAm/maxAPDU=2^32", payload: iam(1<<32, 1), decode: iamAPDU, reject: true},
		{name: "IAm/vendor=65535", payload: iam(1476, maxU16), decode: iamVendor, want: maxU16},
		{name: "IAm/vendor=65536", payload: iam(1476, maxU16+1), decode: iamVendor, reject: true},
		{name: "IAm/vendor=2^32+999", payload: iam(1476, 1<<32+999), decode: iamVendor, reject: true},
		{name: "WhoIs/high=MaxUint32", payload: whoIs(0, maxU32), decode: whoIsHigh, want: maxU32},
		{name: "WhoIs/high=2^32", payload: whoIs(0, maxU32+1), decode: whoIsHigh, reject: true},
		{name: "DCC/duration=65535", payload: dcc(maxU16), decode: dccDecode, want: maxU16},
		{name: "DCC/duration=65536", payload: dcc(maxU16 + 1), decode: dccDecode, reject: true},
		{name: "SubscribeCOV/pid=MaxUint32", payload: sub(maxU32), decode: subPID, want: maxU32},
		{name: "SubscribeCOV/pid=2^32", payload: sub(maxU32 + 1), decode: subPID, reject: true},
	})
}

// TestRangeIAmObjectInstance: an object identifier is 32 bits on the wire, so
// the instance cannot exceed 22 bits after decode. A 5-byte object identifier
// must be refused rather than read as its low four bytes.
func TestRangeIAmObjectInstance(t *testing.T) {
	// Application tag 12 (ObjectIdentifier) with length 5.
	payload := []byte{0xC5, 0x05, 0x01, 0x02, 0x00, 0x00, 0x01}
	payload = appValue(t, payload, bacnet.UnsignedValue(1476))
	payload = appValue(t, payload, bacnet.EnumValue(3))
	payload = appValue(t, payload, bacnet.UnsignedValue(1))
	if m, err := service.DecodeIAm(payload, rangeLimits); err == nil {
		t.Fatalf("5-byte object identifier accepted: %+v", m)
	}
}

func TestDecodeCreateObjectRejectsWideObjectType(t *testing.T) {
	// objectSpecifier [0] { object-type [0] }: 0x3FF is the widest 10-bit type.
	for _, tc := range []struct {
		raw []byte
		ok  bool
	}{
		{[]byte{0x0E, 0x0A, 0x03, 0xFF, 0x0F}, true},
		{[]byte{0x0E, 0x0A, 0x04, 0x00, 0x0F}, false},
	} {
		req, err := service.DecodeCreateObject(tc.raw, bacnet.DefaultDecodeLimits())
		if tc.ok {
			if err != nil || req.ObjectType == nil || *req.ObjectType != 0x3FF {
				t.Fatalf("% x: req=%+v err=%v", tc.raw, req, err)
			}
			continue
		}
		if !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("% x: err=%v, want ErrMalformed", tc.raw, err)
		}
	}
}
