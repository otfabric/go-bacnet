// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// EnrollmentFilter is the optional GetEnrollmentSummary filter acknowledgment.
type EnrollmentFilter uint8

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

// EnrollmentSummaryEntry is one GetEnrollmentSummary ACK element.
type EnrollmentSummaryEntry struct {
	Object            bacnet.ObjectIdentifier
	EventType         uint32
	EventState        uint32
	Priority          uint32
	NotificationClass uint32
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
		body := []bacnet.Element{
			{Value: bacnet.UnsignedValue(uint64(req.PriorityFilter[0]))},
			{Value: bacnet.UnsignedValue(uint64(req.PriorityFilter[1]))},
		}
		dst, err = bacnet.AppendContextTagged(dst, 4, body)
		if err != nil {
			return nil, err
		}
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
	if len(els)%5 != 0 {
		return GetEnrollmentSummaryACK{}, fmt.Errorf("%w: GetEnrollmentSummaryACK length", bacnet.ErrMalformed)
	}
	out := GetEnrollmentSummaryACK{Entries: make([]EnrollmentSummaryEntry, 0, len(els)/5)}
	for i := 0; i+4 < len(els); i += 5 {
		obj, err := bacnet.AsObjectID(els[i].Value)
		if err != nil {
			return GetEnrollmentSummaryACK{}, err
		}
		et, err := bacnet.AsEnumerated(els[i+1].Value)
		if err != nil {
			return GetEnrollmentSummaryACK{}, err
		}
		es, err := bacnet.AsEnumerated(els[i+2].Value)
		if err != nil {
			return GetEnrollmentSummaryACK{}, err
		}
		prio, err := bacnet.AsUnsigned(els[i+3].Value)
		if err != nil {
			return GetEnrollmentSummaryACK{}, err
		}
		nc, err := bacnet.AsUnsigned(els[i+4].Value)
		if err != nil {
			return GetEnrollmentSummaryACK{}, err
		}
		out.Entries = append(out.Entries, EnrollmentSummaryEntry{
			Object:            obj,
			EventType:         uint32(et),
			EventState:        uint32(es),
			Priority:          uint32(prio),
			NotificationClass: uint32(nc),
		})
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
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.UnsignedValue(uint64(e.NotificationClass)))
		if err != nil {
			return nil, err
		}
	}
	return dst, nil
}
