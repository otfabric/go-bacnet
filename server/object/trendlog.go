// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Logging types (BACnetLoggingType).
const (
	LoggingPolled    uint32 = 0
	LoggingCOV       uint32 = 1
	LoggingTriggered uint32 = 2
)

// RangeQuery selects items of a list property for ReadRange (clause 15.8).
type RangeQuery struct {
	By    service.ReadRangeBy
	Index uint32           // byPosition (1-based) and bySequenceNumber
	Time  service.DateTime // byTime
	Count int32
}

// RangeResult is the selected items in order. Position is the 1-based
// position of the first item, Total the number of items in the list; for a
// log, FirstSequence is the sequence number of the first item.
type RangeResult struct {
	Records       []service.LogRecord
	Items         []bacnet.ApplicationValue
	Position      uint32
	Total         uint32
	FirstSequence uint32
	Sequenced     bool
}

// RangeReader is implemented by objects with list properties ReadRange
// can read.
type RangeReader interface {
	ReadRange(ctx context.Context, prop bacnet.PropertyReference, q RangeQuery) (RangeResult, error)
}

// Poller is implemented by objects that act on time (a polled Trend Log).
// The server calls Poll about once a second.
type Poller interface {
	Poll(ctx context.Context, now time.Time, dev *Device)
}

// TrendLogConfig configures a Trend Log object.
type TrendLogConfig struct {
	Instance     uint32
	Name         string
	Description  string
	BufferSize   uint32 // default 1000
	StopWhenFull bool
	// Source and SourceProperty select a property of an object of the same
	// device that is sampled every LogInterval (polled logging). Without a
	// Source records are only added with Log.
	Source         *bacnet.ObjectIdentifier
	SourceProperty bacnet.PropertyIdentifier
	LogInterval    time.Duration
	Enable         bool
}

// TrendLog is a Trend Log object (clause 12.25). Log_Buffer is read with
// ReadRange only; ReadProperty of it answers read-access-denied, as
// bacnet-stack does.
type TrendLog struct {
	*Base
	cfg      TrendLogConfig
	enable   bool
	records  []logEntry // oldest first
	total    uint32     // Total_Record_Count: sequence number of the newest
	lastPoll time.Time
}

type logEntry struct {
	seq uint32
	at  time.Time
	rec service.LogRecord
}

// NewTrendLog returns a Trend Log object.
func NewTrendLog(cfg TrendLogConfig) (*TrendLog, error) {
	if cfg.Name == "" {
		return nil, errorf("object name required")
	}
	if cfg.Instance >= bacnet.MaxObjectInstance {
		return nil, errorf("instance %d out of range", cfg.Instance)
	}
	if cfg.BufferSize == 0 {
		cfg.BufferSize = 1000
	}
	if cfg.Source != nil && cfg.LogInterval <= 0 {
		return nil, errorf("polled logging needs a log interval")
	}
	tl := &TrendLog{Base: NewBase(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeTrendLog, Instance: cfg.Instance}, cfg.Name), cfg: cfg, enable: cfg.Enable}
	if cfg.Description != "" {
		tl.Define(bacnet.PropertyDescription, &Property{Value: charString(cfg.Description), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
	}
	tl.Define(bacnet.PropertyEnable, &Property{Required: true, Writable: true,
		Get: func() bacnet.ApplicationValue { return bacnet.BoolValue(tl.enable) },
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			if w.Property.ArrayIndex != nil {
				return nil, ErrNotAnArray
			}
			if w.Value.Kind != bacnet.ValueBoolean {
				return nil, ErrInvalidDataType
			}
			tl.enable = w.Value.Boolean
			return []bacnet.PropertyIdentifier{bacnet.PropertyEnable}, nil
		}})
	tl.Define(bacnet.PropertyStopWhenFull, &Property{Required: true, Value: bacnet.BoolValue(cfg.StopWhenFull)})
	tl.Define(bacnet.PropertyBufferSize, &Property{Required: true, Value: bacnet.UnsignedValue(uint64(cfg.BufferSize))})
	tl.Define(bacnet.PropertyLogBuffer, &Property{Required: true, Get: func() bacnet.ApplicationValue { return bacnet.ApplicationValue{} }})
	tl.Define(bacnet.PropertyRecordCount, &Property{Required: true, Writable: true,
		Get: func() bacnet.ApplicationValue { return bacnet.UnsignedValue(uint64(len(tl.records))) },
		Set: func(w Write) ([]bacnet.PropertyIdentifier, error) {
			// Only zero may be written: it empties the buffer (clause 12.25).
			if w.Value.Kind != bacnet.ValueUnsigned {
				return nil, ErrInvalidDataType
			}
			if w.Value.Unsigned != 0 {
				return nil, ErrValueOutOfRange
			}
			tl.records = nil
			return []bacnet.PropertyIdentifier{bacnet.PropertyRecordCount}, nil
		}})
	tl.Define(bacnet.PropertyTotalRecordCount, &Property{Required: true, Get: func() bacnet.ApplicationValue { return bacnet.UnsignedValue(uint64(tl.total)) }})
	logging := LoggingTriggered
	if cfg.Source != nil {
		logging = LoggingPolled
		tl.Define(bacnet.PropertyLogInterval, &Property{Value: bacnet.UnsignedValue(uint64(cfg.LogInterval / (10 * time.Millisecond)))})
	}
	tl.Define(bacnet.PropertyLoggingType, &Property{Required: true, Value: bacnet.EnumValue(logging)})
	tl.Define(bacnet.PropertyStatusFlags, &Property{Required: true, Get: func() bacnet.ApplicationValue { return statusFlags(false, false, false, false) }})
	tl.Define(bacnet.PropertyEventState, &Property{Required: true, Value: bacnet.EnumValue(EventStateNormal)})
	return tl, nil
}

// ReadProperty implements Object; Log_Buffer is refused.
func (tl *TrendLog) ReadProperty(ctx context.Context, ref bacnet.PropertyReference) (bacnet.ApplicationValue, error) {
	if ref.Identifier == bacnet.PropertyLogBuffer {
		return bacnet.ApplicationValue{}, ErrReadAccessDenied
	}
	return tl.Base.ReadProperty(ctx, ref)
}

// Log appends a record with the given time stamp when the log is enabled.
// The datum choice follows the value kind (REAL, BOOLEAN, ENUMERATED,
// Unsigned, INTEGER, BIT STRING, NULL).
func (tl *TrendLog) Log(at time.Time, v bacnet.ApplicationValue, flags *bacnet.BitString) error {
	choice, ok := datumChoice(v.Kind)
	if !ok {
		return errorf("value kind %d cannot be logged", v.Kind)
	}
	tl.Update(func() []bacnet.PropertyIdentifier {
		if !tl.enable {
			return nil
		}
		if uint32(len(tl.records)) >= tl.cfg.BufferSize {
			if tl.cfg.StopWhenFull {
				tl.enable = false
				return []bacnet.PropertyIdentifier{bacnet.PropertyEnable}
			}
			tl.records = tl.records[1:]
		}
		tl.total++
		if tl.total == 0 {
			tl.total = 1 // Total_Record_Count wraps to 1
		}
		rec := service.LogRecord{
			Timestamp:   service.DateTime{Date: dateOf(at), Time: timeOf(at)},
			DatumChoice: choice, Datum: v.Clone(), StatusFlags: flags,
		}
		tl.records = append(tl.records, logEntry{seq: tl.total, at: at, rec: rec})
		return []bacnet.PropertyIdentifier{bacnet.PropertyRecordCount, bacnet.PropertyTotalRecordCount}
	})
	return nil
}

func datumChoice(k bacnet.ValueKind) (service.LogDatumChoice, bool) {
	switch k {
	case bacnet.ValueReal:
		return service.LogDatumReal, true
	case bacnet.ValueBoolean:
		return service.LogDatumBoolean, true
	case bacnet.ValueEnumerated:
		return service.LogDatumEnumerated, true
	case bacnet.ValueUnsigned:
		return service.LogDatumUnsigned, true
	case bacnet.ValueSigned:
		return service.LogDatumSigned, true
	case bacnet.ValueBitString:
		return service.LogDatumBitString, true
	case bacnet.ValueNull:
		return service.LogDatumNull, true
	default:
		// Other kinds have no log-datum choice here; Log refuses them.
	}
	return 0, false
}

// Poll implements Poller: polled logging of the source property.
func (tl *TrendLog) Poll(ctx context.Context, now time.Time, dev *Device) {
	if tl.cfg.Source == nil {
		return
	}
	due := false
	tl.View(func() { due = tl.enable && (tl.lastPoll.IsZero() || now.Sub(tl.lastPoll) >= tl.cfg.LogInterval) })
	if !due {
		return
	}
	tl.mu.Lock()
	tl.lastPoll = now
	tl.mu.Unlock()
	o, ok := dev.Object(*tl.cfg.Source)
	if !ok {
		return
	}
	v, err := o.ReadProperty(ctx, bacnet.PropertyReference{Identifier: tl.cfg.SourceProperty})
	if err != nil {
		return
	}
	var flags *bacnet.BitString
	if sf, err := o.ReadProperty(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags}); err == nil && sf.Kind == bacnet.ValueBitString {
		flags = &sf.BitString
	}
	_ = tl.Log(now, v, flags)
}

// ReadRange implements RangeReader for Log_Buffer (clause 15.8). Where the
// standard leaves room the selection follows bacnet-stack (trendlog.c):
// by time with a positive count starts at the first record newer than the
// reference, with a negative count it ends at the last record older than
// it; by position and by sequence number clip the range to the buffer.
func (tl *TrendLog) ReadRange(_ context.Context, prop bacnet.PropertyReference, q RangeQuery) (RangeResult, error) {
	if prop.Identifier != bacnet.PropertyLogBuffer {
		return RangeResult{}, Err(bacnet.ErrorClassServices, bacnet.ErrorCodePropertyIsNotAList)
	}
	if prop.ArrayIndex != nil {
		return RangeResult{}, ErrNotAnArray
	}
	tl.mu.RLock()
	defer tl.mu.RUnlock()
	n := len(tl.records)
	res := RangeResult{Total: uint32(n), Sequenced: true}
	if n == 0 {
		return res, nil
	}
	first, last := -1, -1 // indices into records, inclusive
	switch q.By {
	case service.ReadRangeAll:
		first, last = 0, n-1
	case service.ReadRangeByPosition:
		if q.Count == 0 {
			return res, nil
		}
		ref := int64(q.Index)
		lo, hi := ref, ref+int64(q.Count)-1
		if q.Count < 0 {
			lo, hi = ref+int64(q.Count)+1, ref
		}
		if lo < 1 {
			lo = 1
		}
		if hi > int64(n) {
			hi = int64(n)
		}
		if ref < 1 || ref > int64(n) || lo > hi {
			return res, nil
		}
		first, last = int(lo-1), int(hi-1)
	case service.ReadRangeBySequenceNumber:
		if q.Count == 0 {
			return res, nil
		}
		firstSeq, lastSeq := int64(tl.records[0].seq), int64(tl.records[n-1].seq)
		ref := int64(q.Index)
		lo, hi := ref, ref+int64(q.Count)-1
		if q.Count < 0 {
			lo, hi = ref+int64(q.Count)+1, ref
		}
		if hi < firstSeq || lo > lastSeq {
			return res, nil
		}
		lo, hi = max(lo, firstSeq), min(hi, lastSeq)
		first, last = int(lo-firstSeq), int(hi-firstSeq)
	case service.ReadRangeByTime:
		if q.Count == 0 {
			return res, nil
		}
		ref := dateTimeToTime(q.Time)
		if q.Count > 0 {
			for i, e := range tl.records {
				if e.at.After(ref) {
					first = i
					break
				}
			}
			if first < 0 {
				return res, nil
			}
			last = min(n-1, first+int(q.Count)-1)
		} else {
			for i := n - 1; i >= 0; i-- {
				if tl.records[i].at.Before(ref) {
					last = i
					break
				}
			}
			if last < 0 {
				return res, nil
			}
			first = max(0, last+int(q.Count)+1)
		}
	default:
		return RangeResult{}, Err(bacnet.ErrorClassServices, bacnet.ErrorCodeParameterOutOfRange)
	}
	for _, e := range tl.records[first : last+1] {
		res.Records = append(res.Records, e.rec)
	}
	res.Position = uint32(first + 1)
	res.FirstSequence = tl.records[first].seq
	return res, nil
}

func dateTimeToTime(dt service.DateTime) time.Time {
	d, t := dt.Date, dt.Time
	year := int(d.Year) + 1900
	month, day := int(d.Month), int(d.Day)
	if d.Year == 0xFF {
		year = 1900
	}
	if d.Month == 0xFF || d.Month > 12 {
		month = 1
	}
	if d.Day == 0xFF || d.Day > 31 {
		day = 1
	}
	h, m, s, cs := int(t.Hour), int(t.Minute), int(t.Second), int(t.Hundredths)
	if t.Hour == 0xFF {
		h = 0
	}
	if t.Minute == 0xFF {
		m = 0
	}
	if t.Second == 0xFF {
		s = 0
	}
	if t.Hundredths == 0xFF {
		cs = 0
	}
	return time.Date(year, time.Month(month), day, h, m, s, cs*1e7, time.Local)
}
