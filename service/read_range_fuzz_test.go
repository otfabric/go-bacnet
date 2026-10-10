// SPDX-License-Identifier: MIT

package service_test

import (
	"math"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func FuzzDecodeReadRange(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeReadRange(service.ReadRangeRequest{
		Object:         bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1},
		Property:       bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By:             service.ReadRangeByPosition,
		ReferenceIndex: 1,
		Count:          10,
	}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeReadRange(data, limits)
	})
}

func FuzzDecodeReadRangeACK(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeReadRangeACK(service.ReadRangeACK{
		Object:      bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1},
		Property:    bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
		ResultFlags: service.EncodeResultFlags(true, true, false),
		ItemCount:   1,
		ItemData:    []bacnet.ApplicationValue{bacnet.RealValue(1)},
	}); err == nil {
		f.Add(p)
	}
	// Log_Buffer takes the typed LogRecord path, which sizes its result from
	// the peer-supplied itemCount: seed a well-formed ACK and one whose
	// itemCount (0xFFFFFFFF) far exceeds the single record present.
	rec := service.LogRecord{
		Timestamp:   service.DateTime{Date: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 4}, Time: bacnet.Time{Hour: 12}},
		DatumChoice: service.LogDatumReal,
		Datum:       bacnet.RealValue(1),
	}
	if p, err := service.EncodeReadRangeACK(service.ReadRangeACK{
		Object:      bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: 1},
		Property:    bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		ResultFlags: service.EncodeResultFlags(true, true, false),
		ItemCount:   2,
		LogRecords:  []service.LogRecord{rec, rec},
	}); err == nil {
		f.Add(p)
	}
	f.Add([]byte{0x0C, 0x05, 0x00, 0x00, 0x01, 0x19, 0x83, 0x3A, 0x05, 0xE0, 0x4C, 0xFF, 0xFF, 0xFF, 0xFF,
		0x5E, 0x0E, 0xA4, 0x7E, 0x0A, 0x0A, 0x06, 0xB4, 0x08, 0x00, 0x00, 0x00, 0x0F, 0x1E, 0x2C, 0x3F, 0x80, 0x00, 0x00, 0x1F, 0x5F})
	f.Fuzz(func(t *testing.T, data []byte) {
		ack, err := service.DecodeReadRangeACK(data, limits)
		if err == nil && len(ack.LogRecords) > limits.MaxElements {
			t.Fatalf("LogRecords %d exceeds MaxElements", len(ack.LogRecords))
		}
	})
}

func FuzzDecodeLogRecords(f *testing.F) {
	f.Add([]byte(nil))
	rec := service.LogRecord{
		Timestamp: service.DateTime{
			Date: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 1},
			Time: bacnet.Time{Hour: 12},
		},
		DatumChoice: service.LogDatumReal,
		Datum:       bacnet.RealValue(1),
	}
	if els, err := service.EncodeLogRecord(rec); err == nil {
		if raw, err := bacnet.AppendContextTagged(nil, 5, els); err == nil {
			f.Add(raw)
		}
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		els, _, err := bacnet.ParseSequence(data, bacnet.DefaultDecodeLimits(), -1)
		if err != nil {
			return
		}
		// Exercise the count the peer controls: the exact count, an off-by-one,
		// and the largest value a 32-bit itemCount can carry.
		for _, count := range []int{0, 1, len(els) / 2, len(els)/2 + 1, len(els), math.MaxInt32} {
			recs, err := service.DecodeLogRecords(els, count)
			if err == nil && len(recs) != count {
				t.Fatalf("count=%d decoded %d records", count, len(recs))
			}
		}
	})
}
