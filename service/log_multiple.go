// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// LogDataChoice identifies the log-datum alternative of a
// BACnetLogMultipleRecord (BACnetLogData) or a BACnetEventLogRecord.
type LogDataChoice uint8

const (
	// LogDataLogStatus is log-status [0] BACnetLogStatus.
	LogDataLogStatus LogDataChoice = 0
	// LogDataValues is log-data [1] of a BACnetLogMultipleRecord, or
	// notification [1] of a BACnetEventLogRecord.
	LogDataValues LogDataChoice = 1
	// LogDataTimeChange is time-change [2] REAL.
	LogDataTimeChange LogDataChoice = 2
)

// LogMultipleRecord is a BACnetLogMultipleRecord: an item of the
// Log_Buffer of a Trend Log Multiple object (clause 12.30).
//
// Wire form (ASHRAE 135):
//
//	timestamp [0] BACnetDateTime
//	log-data  [1] BACnetLogData ::= CHOICE {
//	    log-status  [0] BACnetLogStatus,
//	    log-data    [1] SEQUENCE OF CHOICE {
//	        boolean-value [0], real-value [1], enumerated-value [2],
//	        unsigned-value [3], integer-value [4], bitstring-value [5],
//	        null-value [6], failure [7] Error, any-value [8] },
//	    time-change [2] REAL }
type LogMultipleRecord struct {
	Timestamp DateTime
	Choice    LogDataChoice
	// LogStatus is set for LogDataLogStatus.
	LogStatus bacnet.BitString
	// Values is set for LogDataValues: one entry per member of the
	// object's Log_DeviceObjectProperty, in order.
	Values []LogMultipleValue
	// TimeChange is set for LogDataTimeChange.
	TimeChange float32
}

// LogMultipleValue is one value of a LogMultipleRecord's log-data. Choice
// uses the LogRecord alternatives (LogDatumBoolean … LogDatumAnyValue);
// on the wire its context tag is one less (boolean-value is [0]).
type LogMultipleValue struct {
	Choice LogDatumChoice
	Value  bacnet.ApplicationValue
}

// EventLogRecord is a BACnetEventLogRecord: an item of the Log_Buffer of an
// Event Log object (clause 12.27).
//
// Wire form (ASHRAE 135):
//
//	timestamp [0] BACnetDateTime
//	log-datum [1] CHOICE {
//	    log-status   [0] BACnetLogStatus,
//	    notification [1] ConfirmedEventNotification-Request,
//	    time-change  [2] REAL }
type EventLogRecord struct {
	Timestamp DateTime
	Choice    LogDataChoice
	// LogStatus is set for LogDataLogStatus.
	LogStatus bacnet.BitString
	// Notification is set for LogDataValues.
	Notification *EventNotification
	// TimeChange is set for LogDataTimeChange.
	TimeChange float32
}

// DecodeLogMultipleRecords splits the itemData of a ReadRange ACK of a Trend
// Log Multiple's Log_Buffer into count records.
func DecodeLogMultipleRecords(elements []bacnet.Element, count int) ([]LogMultipleRecord, error) {
	if err := checkRecordCount(elements, count); err != nil || count == 0 {
		return nil, err
	}
	out := make([]LogMultipleRecord, 0, count)
	for i := range count {
		ts, datum, err := splitRecord(elements[2*i:])
		if err != nil {
			return nil, err
		}
		rec := LogMultipleRecord{Timestamp: ts, Choice: LogDataChoice(datum.TagNumber)}
		switch rec.Choice {
		case LogDataLogStatus:
			rec.LogStatus, err = bacnet.ContextBitString(datum)
		case LogDataValues:
			rec.Values, err = decodeLogMultipleValues(datum)
		case LogDataTimeChange:
			rec.TimeChange, err = bacnet.ContextReal(datum)
		default:
			err = fmt.Errorf("%w: unknown BACnetLogData choice %d", bacnet.ErrMalformed, datum.TagNumber)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

func decodeLogMultipleValues(el bacnet.Element) ([]LogMultipleValue, error) {
	if !bacnet.IsContextConstructed(el) {
		return nil, fmt.Errorf("%w: BACnetLogData log-data is not constructed", bacnet.ErrMalformed)
	}
	out := make([]LogMultipleValue, 0, len(el.Value.Elements))
	for _, v := range el.Value.Elements {
		if !v.Context || v.TagNumber > 8 {
			return nil, fmt.Errorf("%w: BACnetLogData log-data choice tag", bacnet.ErrMalformed)
		}
		choice := LogDatumChoice(v.TagNumber + 1)
		val, err := decodeLogDatumValue(choice, v)
		if err != nil {
			return nil, err
		}
		out = append(out, LogMultipleValue{Choice: choice, Value: val})
	}
	return out, nil
}

// DecodeEventLogRecords splits the itemData of a ReadRange ACK of an Event
// Log's Log_Buffer into count records.
func DecodeEventLogRecords(elements []bacnet.Element, count int, limits bacnet.DecodeLimits) ([]EventLogRecord, error) {
	if err := checkRecordCount(elements, count); err != nil || count == 0 {
		return nil, err
	}
	out := make([]EventLogRecord, 0, count)
	for i := range count {
		ts, datum, err := splitRecord(elements[2*i:])
		if err != nil {
			return nil, err
		}
		rec := EventLogRecord{Timestamp: ts, Choice: LogDataChoice(datum.TagNumber)}
		switch rec.Choice {
		case LogDataLogStatus:
			rec.LogStatus, err = bacnet.ContextBitString(datum)
		case LogDataValues:
			if !bacnet.IsContextConstructed(datum) {
				return nil, fmt.Errorf("%w: BACnetEventLogRecord notification is not constructed", bacnet.ErrMalformed)
			}
			var raw []byte
			for _, e := range datum.Value.Elements {
				if raw, err = bacnet.AppendTag(raw, e); err != nil {
					return nil, err
				}
			}
			var note EventNotification
			if note, err = DecodeEventNotification(raw, limits); err == nil {
				rec.Notification = &note
			}
		case LogDataTimeChange:
			rec.TimeChange, err = bacnet.ContextReal(datum)
		default:
			err = fmt.Errorf("%w: unknown BACnetEventLogRecord choice %d", bacnet.ErrMalformed, datum.TagNumber)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, rec)
	}
	return out, nil
}

// checkRecordCount checks a peer-controlled record count against the flat
// itemData: every record is exactly a timestamp [0] and a datum [1].
func checkRecordCount(elements []bacnet.Element, count int) error {
	if count < 0 || count != len(elements)/2 || len(elements)%2 != 0 {
		return fmt.Errorf("%w: log record count %d does not match %d itemData elements", bacnet.ErrMalformed, count, len(elements))
	}
	return nil
}

// splitRecord reads the timestamp [0] and returns it with the single
// alternative held by the datum [1] of a two-element log record.
func splitRecord(els []bacnet.Element) (DateTime, bacnet.Element, error) {
	tsEl, datumEl := els[0], els[1]
	if tsEl.TagNumber != 0 || !bacnet.IsContextConstructed(tsEl) || len(tsEl.Value.Elements) != 2 {
		return DateTime{}, bacnet.Element{}, fmt.Errorf("%w: log record timestamp", bacnet.ErrMalformed)
	}
	d, t := tsEl.Value.Elements[0], tsEl.Value.Elements[1]
	if d.Context || d.Value.Kind != bacnet.ValueDate || t.Context || t.Value.Kind != bacnet.ValueTime {
		return DateTime{}, bacnet.Element{}, fmt.Errorf("%w: log record timestamp kinds", bacnet.ErrMalformed)
	}
	if datumEl.TagNumber != 1 || !bacnet.IsContextConstructed(datumEl) || len(datumEl.Value.Elements) != 1 || !datumEl.Value.Elements[0].Context {
		return DateTime{}, bacnet.Element{}, fmt.Errorf("%w: log record datum", bacnet.ErrMalformed)
	}
	return DateTime{Date: d.Value.Date, Time: t.Value.Time}, datumEl.Value.Elements[0], nil
}

// EncodeLogMultipleRecord encodes one BACnetLogMultipleRecord as itemData
// elements (for EncodeReadRangeACK).
func EncodeLogMultipleRecord(rec LogMultipleRecord) ([]bacnet.Element, error) {
	var datum []byte
	var err error
	switch rec.Choice {
	case LogDataLogStatus:
		datum, err = bacnet.AppendContextBitString(nil, 0, rec.LogStatus)
	case LogDataValues:
		var body []bacnet.Element
		for _, v := range rec.Values {
			if v.Choice < LogDatumBoolean || v.Choice > LogDatumAnyValue || v.Choice == LogDatumTimeChange {
				return nil, fmt.Errorf("%w: BACnetLogData log-data choice %d", bacnet.ErrMalformed, v.Choice)
			}
			els, encErr := encodeLogDatum(v.Choice, v.Value)
			if encErr != nil {
				return nil, encErr
			}
			// encodeLogDatum tags the BACnetLogRecord alternative; the
			// BACnetLogData list numbers them from zero (boolean-value [0]).
			els[0].TagNumber--
			body = append(body, els...)
		}
		datum, err = bacnet.AppendContextTagged(nil, 1, body)
	case LogDataTimeChange:
		datum, err = bacnet.AppendContextReal(nil, 2, rec.TimeChange)
	default:
		err = fmt.Errorf("%w: unknown BACnetLogData choice %d", bacnet.ErrMalformed, rec.Choice)
	}
	if err != nil {
		return nil, err
	}
	return encodeRecord(rec.Timestamp, datum)
}

// EncodeEventLogRecord encodes one BACnetEventLogRecord as itemData
// elements (for EncodeReadRangeACK).
func EncodeEventLogRecord(rec EventLogRecord) ([]bacnet.Element, error) {
	var datum []byte
	var err error
	switch rec.Choice {
	case LogDataLogStatus:
		datum, err = bacnet.AppendContextBitString(nil, 0, rec.LogStatus)
	case LogDataValues:
		if rec.Notification == nil {
			return nil, fmt.Errorf("%w: BACnetEventLogRecord notification missing", bacnet.ErrMalformed)
		}
		var raw []byte
		if raw, err = EncodeEventNotification(*rec.Notification); err != nil {
			return nil, err
		}
		datum = append(append([]byte{0x1E}, raw...), 0x1F) // [1] notification
	case LogDataTimeChange:
		datum, err = bacnet.AppendContextReal(nil, 2, rec.TimeChange)
	default:
		err = fmt.Errorf("%w: unknown BACnetEventLogRecord choice %d", bacnet.ErrMalformed, rec.Choice)
	}
	if err != nil {
		return nil, err
	}
	return encodeRecord(rec.Timestamp, datum)
}

func encodeRecord(ts DateTime, datum []byte) ([]bacnet.Element, error) {
	raw, err := bacnet.AppendContextTagged(nil, 0, []bacnet.Element{
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate, Date: ts.Date}},
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime, Time: ts.Time}},
	})
	if err != nil {
		return nil, err
	}
	raw = append(raw, 0x1E) // [1] log-datum
	raw = append(raw, datum...)
	raw = append(raw, 0x1F)
	els, _, err := bacnet.ParseSequence(raw, bacnet.DefaultDecodeLimits(), -1)
	return els, err
}
