// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// EnrollmentFilter is the GetEnrollmentSummary acknowledgmentFilter
// enumeration: all (0), acked (1), not-acked (2).
type EnrollmentFilter uint8

// acknowledgmentFilter values (ASHRAE 135 GetEnrollmentSummary-Request).
const (
	AcknowledgmentFilterAll      EnrollmentFilter = 0
	AcknowledgmentFilterAcked    EnrollmentFilter = 1
	AcknowledgmentFilterNotAcked EnrollmentFilter = 2
)

// Misnamed aliases of the acknowledgmentFilter values, kept for source
// compatibility; prefer the AcknowledgmentFilter constants.
// EnrollmentFilterPriority and EnrollmentFilterNotificationClass (3 and 4)
// are not defined by the standard.
const (
	EnrollmentFilterAll EnrollmentFilter = iota
	EnrollmentFilterEventState
	EnrollmentFilterEventType
	EnrollmentFilterPriority
	EnrollmentFilterNotificationClass
)

// GetEnrollmentSummaryRequest is a GetEnrollmentSummary confirmed request.
//
// Empty AcknowledgmentFilter means “all”. Optional filters use context tags
// per ASHRAE 135 GetEnrollmentSummary-Request.
type GetEnrollmentSummaryRequest struct {
	AcknowledgmentFilter EnrollmentFilter // 0=all, 1=acked, 2=not-acked (as enum)
	// Optional filters omitted in v1 encode when zero/nil.
	EnrollmentFilter        *EnrollmentFilter // unused reserved for future CHOICE
	EventStateFilter        *uint32
	EventTypeFilter         *uint32
	PriorityFilter          *[2]uint32 // min,max
	NotificationClassFilter *uint32
}

// EnrollmentSummaryEntry is one GetEnrollmentSummary ACK element:
//
//	objectIdentifier, eventType, eventState, priority Unsigned8,
//	notificationClass Unsigned OPTIONAL
type EnrollmentSummaryEntry struct {
	Object            bacnet.ObjectIdentifier
	EventType         uint32
	EventState        uint32
	Priority          uint32
	NotificationClass uint32
	// NotificationClassAbsent reports that the optional notificationClass was
	// not on the wire (NotificationClass is then 0); set it to omit the field
	// on encode.
	NotificationClassAbsent bool
}

// GetEnrollmentSummaryACK is a GetEnrollmentSummary ComplexACK.
type GetEnrollmentSummaryACK struct {
	Entries []EnrollmentSummaryEntry
}

// EncodeGetEnrollmentSummary encodes a GetEnrollmentSummary request.
func EncodeGetEnrollmentSummary(req GetEnrollmentSummaryRequest) ([]byte, error) {
	dst, err := bacnet.AppendContextUnsigned(nil, 0, uint64(req.AcknowledgmentFilter))
	if err != nil {
		return nil, err
	}
	if req.EventStateFilter != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(*req.EventStateFilter))
		if err != nil {
			return nil, err
		}
	}
	if req.EventTypeFilter != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 3, uint64(*req.EventTypeFilter))
		if err != nil {
			return nil, err
		}
	}
	if req.PriorityFilter != nil {
		// priorityFilter [4] SEQUENCE { minPriority [0] Unsigned8, maxPriority [1] Unsigned8 }
		if req.PriorityFilter[0] > 255 || req.PriorityFilter[1] > 255 {
			return nil, fmt.Errorf("%w: GetEnrollmentSummary priority filter exceeds Unsigned8", bacnet.ErrMalformed)
		}
		dst = append(dst, 0x4E)
		dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(req.PriorityFilter[0]))
		if err != nil {
			return nil, err
		}
		dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(req.PriorityFilter[1]))
		if err != nil {
			return nil, err
		}
		dst = append(dst, 0x4F)
	}
	if req.NotificationClassFilter != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 5, uint64(*req.NotificationClassFilter))
		if err != nil {
			return nil, err
		}
	}
	return dst, nil
}

// DecodeGetEnrollmentSummaryACK decodes a GetEnrollmentSummary ComplexACK.
func DecodeGetEnrollmentSummaryACK(payload []byte, limits bacnet.DecodeLimits) (GetEnrollmentSummaryACK, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return GetEnrollmentSummaryACK{}, err
	}
	if n != len(payload) {
		return GetEnrollmentSummaryACK{}, fmt.Errorf("%w: GetEnrollmentSummaryACK trailing", bacnet.ErrTrailingData)
	}
	// Each entry is four mandatory application values followed by an optional
	// notificationClass Unsigned; the next entry starts at an ObjectIdentifier.
	out := GetEnrollmentSummaryACK{Entries: make([]EnrollmentSummaryEntry, 0, len(els)/4)}
	for i := 0; i < len(els); {
		if len(els)-i < 4 {
			return GetEnrollmentSummaryACK{}, fmt.Errorf("%w: GetEnrollmentSummaryACK length", bacnet.ErrMalformed)
		}
		for _, el := range els[i : i+4] {
			if el.Context {
				return GetEnrollmentSummaryACK{}, fmt.Errorf("%w: GetEnrollmentSummaryACK context tag", bacnet.ErrMalformed)
			}
		}
		obj, err := bacnet.AsObjectID(els[i].Value)
		if err != nil {
			return GetEnrollmentSummaryACK{}, fmt.Errorf("%w: GetEnrollmentSummaryACK objectIdentifier", bacnet.ErrMalformed)
		}
		et, err := bacnet.AsEnumerated(els[i+1].Value)
		if err != nil {
			return GetEnrollmentSummaryACK{}, fmt.Errorf("%w: GetEnrollmentSummaryACK eventType", bacnet.ErrMalformed)
		}
		es, err := bacnet.AsEnumerated(els[i+2].Value)
		if err != nil {
			return GetEnrollmentSummaryACK{}, fmt.Errorf("%w: GetEnrollmentSummaryACK eventState", bacnet.ErrMalformed)
		}
		if els[i+3].Value.Kind != bacnet.ValueUnsigned || els[i+3].Value.Unsigned > 255 {
			return GetEnrollmentSummaryACK{}, fmt.Errorf("%w: GetEnrollmentSummaryACK priority", bacnet.ErrMalformed)
		}
		entry := EnrollmentSummaryEntry{
			Object:                  obj,
			EventType:               et,
			EventState:              es,
			Priority:                uint32(els[i+3].Value.Unsigned),
			NotificationClassAbsent: true,
		}
		i += 4
		if i < len(els) && !els[i].Context && els[i].Value.Kind == bacnet.ValueUnsigned {
			if els[i].Value.Unsigned > 0xFFFFFFFF {
				return GetEnrollmentSummaryACK{}, fmt.Errorf("%w: GetEnrollmentSummaryACK notificationClass", bacnet.ErrMalformed)
			}
			entry.NotificationClass = uint32(els[i].Value.Unsigned)
			entry.NotificationClassAbsent = false
			i++
		}
		out.Entries = append(out.Entries, entry)
	}
	return out, nil
}

// EncodeGetEnrollmentSummaryACK encodes a GetEnrollmentSummary ACK (tests/helpers).
func EncodeGetEnrollmentSummaryACK(ack GetEnrollmentSummaryACK) ([]byte, error) {
	var dst []byte
	for _, e := range ack.Entries {
		var err error
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.ObjectIDValue(e.Object))
		if err != nil {
			return nil, err
		}
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.EnumValue(e.EventType))
		if err != nil {
			return nil, err
		}
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.EnumValue(e.EventState))
		if err != nil {
			return nil, err
		}
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.UnsignedValue(uint64(e.Priority)))
		if err != nil {
			return nil, err
		}
		if e.NotificationClassAbsent {
			continue
		}
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.UnsignedValue(uint64(e.NotificationClass)))
		if err != nil {
			return nil, err
		}
	}
	return dst, nil
}
