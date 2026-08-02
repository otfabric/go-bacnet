// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// AlarmSummaryEntry is one GetAlarmSummary ACK list element.
type AlarmSummaryEntry struct {
	Object           bacnet.ObjectIdentifier
	AlarmState       uint32
	AckedTransitions bacnet.BitString
}

// GetAlarmSummaryACK is a GetAlarmSummary ComplexACK (SEQUENCE OF summaries).
type GetAlarmSummaryACK struct {
	Entries []AlarmSummaryEntry
}

// EncodeGetAlarmSummary encodes an empty GetAlarmSummary request payload.
func EncodeGetAlarmSummary() ([]byte, error) { return nil, nil }

// EncodeGetAlarmSummaryACK encodes a GetAlarmSummary ACK.
func EncodeGetAlarmSummaryACK(ack GetAlarmSummaryACK) ([]byte, error) {
	var dst []byte
	for _, e := range ack.Entries {
		var err error
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.ObjectIDValue(e.Object))
		if err != nil {
			return nil, err
		}
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.EnumValue(e.AlarmState))
		if err != nil {
			return nil, err
		}
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{
			Kind:      bacnet.ValueBitString,
			BitString: e.AckedTransitions,
		})
		if err != nil {
			return nil, err
		}
	}
	return dst, nil
}

// DecodeGetAlarmSummaryACK decodes a GetAlarmSummary ComplexACK payload.
func DecodeGetAlarmSummaryACK(payload []byte, limits bacnet.DecodeLimits) (GetAlarmSummaryACK, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return GetAlarmSummaryACK{}, err
	}
	if n != len(payload) {
		return GetAlarmSummaryACK{}, fmt.Errorf("%w: GetAlarmSummaryACK trailing", bacnet.ErrTrailingData)
	}
	if len(els)%3 != 0 {
		return GetAlarmSummaryACK{}, fmt.Errorf("%w: GetAlarmSummaryACK length", bacnet.ErrMalformed)
	}
	out := GetAlarmSummaryACK{Entries: make([]AlarmSummaryEntry, 0, len(els)/3)}
	for i := 0; i+2 < len(els); i += 3 {
		obj, err := bacnet.AsObjectID(els[i].Value)
		if err != nil {
			return GetAlarmSummaryACK{}, err
		}
		state, err := bacnet.AsEnumerated(els[i+1].Value)
		if err != nil {
			return GetAlarmSummaryACK{}, err
		}
		if els[i+2].Value.Kind != bacnet.ValueBitString {
			return GetAlarmSummaryACK{}, fmt.Errorf("%w: ackedTransitions", bacnet.ErrMalformed)
		}
		out.Entries = append(out.Entries, AlarmSummaryEntry{
			Object:           obj,
			AlarmState:       uint32(state),
			AckedTransitions: els[i+2].Value.BitString,
		})
	}
	return out, nil
}
