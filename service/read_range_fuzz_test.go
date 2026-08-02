// SPDX-License-Identifier: MIT

package service_test

import (
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
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeReadRangeACK(data, limits)
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
		_, _ = service.DecodeLogRecords(els, 1)
	})
}
