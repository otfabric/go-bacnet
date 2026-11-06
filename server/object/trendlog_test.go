// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

var logBuffer = bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}

var t0 = time.Date(2026, 1, 2, 3, 4, 0, 0, time.Local)

func filledLog(t *testing.T, size uint32, n int, stop bool) *TrendLog {
	t.Helper()
	tl, err := NewTrendLog(TrendLogConfig{Instance: 1, Name: "TL-1", BufferSize: size, StopWhenFull: stop, Enable: true, Description: "d"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < n; i++ {
		if err := tl.Log(t0.Add(time.Duration(i)*time.Minute), bacnet.RealValue(float32(i)), nil); err != nil {
			t.Fatal(err)
		}
	}
	return tl
}

func seqs(r RangeResult) []float32 {
	var out []float32
	for _, rec := range r.Records {
		out = append(out, rec.Datum.Real)
	}
	return out
}

func equal(a, b []float32) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func dt(at time.Time) service.DateTime { return service.DateTime{Date: dateOf(at), Time: timeOf(at)} }

func TestTrendLogRange(t *testing.T) {
	tl := filledLog(t, 5, 8, false) // records 3..7, sequence 4..8
	ctx := context.Background()
	cases := []struct {
		name  string
		q     RangeQuery
		want  []float32
		pos   uint32
		first uint32
	}{
		{"all", RangeQuery{By: service.ReadRangeAll}, []float32{3, 4, 5, 6, 7}, 1, 4},
		{"position forward", RangeQuery{By: service.ReadRangeByPosition, Index: 2, Count: 2}, []float32{4, 5}, 2, 5},
		{"position forward clipped", RangeQuery{By: service.ReadRangeByPosition, Index: 4, Count: 10}, []float32{6, 7}, 4, 7},
		{"position backward", RangeQuery{By: service.ReadRangeByPosition, Index: 3, Count: -2}, []float32{4, 5}, 2, 5},
		{"position backward clipped", RangeQuery{By: service.ReadRangeByPosition, Index: 2, Count: -5}, []float32{3, 4}, 1, 4},
		{"position past end", RangeQuery{By: service.ReadRangeByPosition, Index: 6, Count: 1}, nil, 0, 0},
		{"position zero", RangeQuery{By: service.ReadRangeByPosition, Index: 0, Count: 1}, nil, 0, 0},
		{"position count zero", RangeQuery{By: service.ReadRangeByPosition, Index: 1}, nil, 0, 0},
		{"sequence forward", RangeQuery{By: service.ReadRangeBySequenceNumber, Index: 5, Count: 2}, []float32{4, 5}, 2, 5},
		{"sequence clipped low", RangeQuery{By: service.ReadRangeBySequenceNumber, Index: 1, Count: 5}, []float32{3, 4}, 1, 4},
		{"sequence backward", RangeQuery{By: service.ReadRangeBySequenceNumber, Index: 8, Count: -2}, []float32{6, 7}, 4, 7},
		{"sequence outside", RangeQuery{By: service.ReadRangeBySequenceNumber, Index: 20, Count: 2}, nil, 0, 0},
		{"sequence count zero", RangeQuery{By: service.ReadRangeBySequenceNumber, Index: 5}, nil, 0, 0},
		{"time forward", RangeQuery{By: service.ReadRangeByTime, Time: dt(t0.Add(4 * time.Minute)), Count: 2}, []float32{5, 6}, 3, 6},
		{"time forward none", RangeQuery{By: service.ReadRangeByTime, Time: dt(t0.Add(7 * time.Minute)), Count: 2}, nil, 0, 0},
		{"time backward", RangeQuery{By: service.ReadRangeByTime, Time: dt(t0.Add(5 * time.Minute)), Count: -3}, []float32{3, 4}, 1, 4},
		{"time backward none", RangeQuery{By: service.ReadRangeByTime, Time: dt(t0), Count: -3}, nil, 0, 0},
		{"time count zero", RangeQuery{By: service.ReadRangeByTime, Time: dt(t0)}, nil, 0, 0},
	}
	for _, c := range cases {
		r, err := tl.ReadRange(ctx, logBuffer, c.q)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !equal(seqs(r), c.want) || r.Total != 5 || (c.want != nil && (r.Position != c.pos || r.FirstSequence != c.first)) {
			t.Errorf("%s: got %v pos %d seq %d total %d", c.name, seqs(r), r.Position, r.FirstSequence, r.Total)
		}
	}
	if _, err := tl.ReadRange(ctx, logBuffer, RangeQuery{By: 9}); err == nil {
		t.Error("unknown range accepted")
	}
	if _, err := tl.ReadRange(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyRecordCount}, RangeQuery{}); AsError(err).Code != bacnet.ErrorCodePropertyIsNotAList {
		t.Errorf("non-list property: %v", err)
	}
	idx := uint32(1)
	if _, err := tl.ReadRange(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer, ArrayIndex: &idx}, RangeQuery{}); !errors.Is(err, ErrNotAnArray) {
		t.Errorf("array index: %v", err)
	}
	empty := filledLog(t, 5, 0, false)
	if r, err := empty.ReadRange(ctx, logBuffer, RangeQuery{By: service.ReadRangeAll}); err != nil || len(r.Records) != 0 {
		t.Errorf("empty: %v %+v", err, r)
	}
}

func TestTrendLogProperties(t *testing.T) {
	ctx := context.Background()
	tl := filledLog(t, 3, 5, true) // stops after 3
	read := func(p bacnet.PropertyIdentifier) bacnet.ApplicationValue {
		v, err := tl.ReadProperty(ctx, bacnet.PropertyReference{Identifier: p})
		if err != nil {
			t.Fatalf("read %d: %v", p, err)
		}
		return v
	}
	if read(bacnet.PropertyRecordCount).Unsigned != 3 || read(bacnet.PropertyTotalRecordCount).Unsigned != 3 || read(bacnet.PropertyEnable).Boolean {
		t.Fatal("stop when full")
	}
	if _, err := tl.ReadProperty(ctx, logBuffer); !errors.Is(err, ErrReadAccessDenied) {
		t.Fatalf("Log_Buffer: %v", err)
	}
	w := func(p bacnet.PropertyIdentifier, v bacnet.ApplicationValue) error {
		return tl.WriteProperty(ctx, Write{Property: bacnet.PropertyReference{Identifier: p}, Value: v})
	}
	if err := w(bacnet.PropertyRecordCount, bacnet.UnsignedValue(1)); !errors.Is(err, ErrValueOutOfRange) {
		t.Fatalf("record count 1: %v", err)
	}
	if err := w(bacnet.PropertyRecordCount, bacnet.RealValue(0)); !errors.Is(err, ErrInvalidDataType) {
		t.Fatalf("record count real: %v", err)
	}
	if err := w(bacnet.PropertyRecordCount, bacnet.UnsignedValue(0)); err != nil || read(bacnet.PropertyRecordCount).Unsigned != 0 {
		t.Fatalf("record count 0: %v", err)
	}
	if err := w(bacnet.PropertyEnable, bacnet.UnsignedValue(1)); !errors.Is(err, ErrInvalidDataType) {
		t.Fatalf("enable unsigned: %v", err)
	}
	idx := uint32(1)
	if err := tl.WriteProperty(ctx, Write{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyEnable, ArrayIndex: &idx}, Value: bacnet.BoolValue(true)}); err == nil {
		t.Fatal("enable with index accepted")
	}
	if err := w(bacnet.PropertyEnable, bacnet.BoolValue(true)); err != nil || !read(bacnet.PropertyEnable).Boolean {
		t.Fatalf("enable: %v", err)
	}
	_ = tl.Log(t0, bacnet.BoolValue(true), &bacnet.BitString{UnusedBits: 4, Bytes: []byte{0}})
	if read(bacnet.PropertyTotalRecordCount).Unsigned != 4 {
		t.Fatal("total after re-enable")
	}
	if err := tl.Log(t0, charString("x"), nil); err == nil {
		t.Fatal("character string logged")
	}
	// Disabled logs ignore new records; a wrapping log drops the oldest.
	_ = w(bacnet.PropertyEnable, bacnet.BoolValue(false))
	_ = tl.Log(t0, bacnet.RealValue(1), nil)
	if read(bacnet.PropertyTotalRecordCount).Unsigned != 4 {
		t.Fatal("disabled log recorded")
	}
	for _, v := range []bacnet.ApplicationValue{bacnet.EnumValue(1), bacnet.UnsignedValue(1), bacnet.SignedValue(-1), bacnet.NullValue(), {Kind: bacnet.ValueBitString}} {
		if _, ok := datumChoice(v.Kind); !ok {
			t.Errorf("kind %d not loggable", v.Kind)
		}
	}
}

func TestTrendLogConfig(t *testing.T) {
	if _, err := NewTrendLog(TrendLogConfig{}); err == nil {
		t.Error("no name accepted")
	}
	if _, err := NewTrendLog(TrendLogConfig{Name: "x", Instance: bacnet.MaxObjectInstance}); err == nil {
		t.Error("bad instance accepted")
	}
	src := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	if _, err := NewTrendLog(TrendLogConfig{Name: "x", Source: &src}); err == nil {
		t.Error("polled log without interval accepted")
	}
}

func TestTrendLogPoll(t *testing.T) {
	ctx := context.Background()
	d, err := NewDevice(DeviceConfig{Instance: 1, Name: "dev"})
	if err != nil {
		t.Fatal(err)
	}
	av, _ := NewAnalogValue(PointConfig{Instance: 1, Name: "AV-1", Value: 1})
	_ = d.Add(av)
	src := av.Identifier()
	tl, err := NewTrendLog(TrendLogConfig{Instance: 1, Name: "TL", Source: &src, SourceProperty: bacnet.PropertyPresentValue, LogInterval: time.Minute, Enable: true})
	if err != nil {
		t.Fatal(err)
	}
	missing := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 9}
	orphan, _ := NewTrendLog(TrendLogConfig{Instance: 2, Name: "TL2", Source: &missing, SourceProperty: bacnet.PropertyPresentValue, LogInterval: time.Minute, Enable: true})
	bad, _ := NewTrendLog(TrendLogConfig{Instance: 3, Name: "TL3", Source: &src, SourceProperty: bacnet.PropertyObjectName, LogInterval: time.Minute, Enable: true})
	unknown, _ := NewTrendLog(TrendLogConfig{Instance: 4, Name: "TL4", Source: &src, SourceProperty: bacnet.PropertyPolarity, LogInterval: time.Minute, Enable: true})
	manual := filledLog(t, 2, 0, false)
	for _, o := range []*TrendLog{tl, orphan, bad, unknown} {
		_ = d.Add(o)
	}
	for i := 0; i < 3; i++ {
		now := t0.Add(time.Duration(i) * 30 * time.Second)
		for _, o := range []*TrendLog{tl, orphan, bad, unknown, manual} {
			o.Poll(ctx, now, d)
		}
	}
	r, _ := tl.ReadRange(ctx, logBuffer, RangeQuery{By: service.ReadRangeAll})
	if len(r.Records) != 2 || r.Records[0].StatusFlags == nil {
		t.Fatalf("polled records: %+v", r)
	}
	for _, o := range []*TrendLog{orphan, bad, unknown, manual} {
		if r, _ := o.ReadRange(ctx, logBuffer, RangeQuery{By: service.ReadRangeAll}); len(r.Records) != 0 {
			t.Errorf("%s logged %d", o.Name(), len(r.Records))
		}
	}
}

func TestDateTimeToTimeWildcards(t *testing.T) {
	got := dateTimeToTime(service.DateTime{
		Date: bacnet.Date{Year: 0xFF, Month: 0xFF, Day: 0xFF, Weekday: 0xFF},
		Time: bacnet.Time{Hour: 0xFF, Minute: 0xFF, Second: 0xFF, Hundredths: 0xFF},
	})
	if !got.Equal(time.Date(1900, 1, 1, 0, 0, 0, 0, time.Local)) {
		t.Fatal(got)
	}
}
