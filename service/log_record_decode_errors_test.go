// SPDX-License-Identifier: MIT

package service

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

// TestLogRecordDecodeDatumErrors feeds BACnetLogRecords whose logDatum or
// statusFlags carry a value that does not fit the CHOICE alternative.
func TestLogRecordDecodeDatumErrors(t *testing.T) {
	okTS := bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate}},
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime}},
	}}}
	prim := func(tag uint8, octets ...byte) bacnet.Element {
		return bacnet.Element{Context: true, TagNumber: tag, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: octets}}
	}
	datum := func(choice bacnet.Element) bacnet.Element {
		return bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{choice}}}
	}
	nine := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}
	cases := map[string][]bacnet.Element{
		// timestamp [0] must be constructed.
		"primitive timestamp": {prim(0, 1), datum(prim(byte(LogDatumNull)))},
		"empty bit string":    {okTS, datum(prim(byte(LogDatumBitString)))},
		"empty log status": {okTS, datum(bacnet.Element{Context: true, TagNumber: byte(LogDatumLogStatus), Value: bacnet.ApplicationValue{
			Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}}})},
		"enumerated over 32 bits": {okTS, datum(prim(byte(LogDatumEnumerated), 1, 2, 3, 4, 5))},
		"unsigned over 64 bits":   {okTS, datum(prim(byte(LogDatumUnsigned), nine...))},
		"signed over 64 bits":     {okTS, datum(prim(byte(LogDatumSigned), nine...))},
		"empty status flags":      {okTS, datum(prim(byte(LogDatumNull))), prim(2)},
	}
	for name, els := range cases {
		if _, _, err := decodeLogRecord(els); !errors.Is(err, bacnet.ErrMalformed) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := DecodeLogRecords(els, 1); err == nil {
			t.Errorf("%s: DecodeLogRecords accepted", name)
		}
	}
	// The same shapes with valid values decode.
	rec, n, err := decodeLogRecord([]bacnet.Element{okTS, datum(prim(byte(LogDatumEnumerated), 7)), prim(2, 4, 0x80)})
	if err != nil || n != 3 || rec.DatumChoice != LogDatumEnumerated || rec.Datum.Enumerated != 7 || rec.StatusFlags == nil || rec.StatusFlags.Bytes[0] != 0x80 {
		t.Fatalf("valid record %+v %d %v", rec, n, err)
	}
}
