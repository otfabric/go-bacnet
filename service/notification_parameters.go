// SPDX-License-Identifier: MIT

package service

import (
	"encoding/binary"
	"fmt"
	"math"

	"github.com/otfabric/go-bacnet"
)

// NotificationParametersChoice is the BACnetNotificationParameters CHOICE tag.
type NotificationParametersChoice uint8

const (
	NotificationChangeOfBitstring  NotificationParametersChoice = 0
	NotificationChangeOfState      NotificationParametersChoice = 1
	NotificationChangeOfValue      NotificationParametersChoice = 2
	NotificationCommandFailure     NotificationParametersChoice = 3
	NotificationFloatingLimit      NotificationParametersChoice = 4
	NotificationOutOfRange         NotificationParametersChoice = 5
	NotificationComplexEventType   NotificationParametersChoice = 6
	NotificationChangeOfLifeSafety NotificationParametersChoice = 8
	NotificationExtended           NotificationParametersChoice = 9
	NotificationBufferReady        NotificationParametersChoice = 10
	NotificationUnsignedRange      NotificationParametersChoice = 11
)

// PropertyStates is a BACnetPropertyStates CHOICE (context tag + value).
type PropertyStates struct {
	Choice uint8
	Value  bacnet.ApplicationValue
}

// ChangeOfStateParams is NotificationParameters change-of-state [1].
type ChangeOfStateParams struct {
	NewState    PropertyStates
	StatusFlags bacnet.BitString
}

// ChangeOfBitstringParams is NotificationParameters change-of-bitstring [0].
type ChangeOfBitstringParams struct {
	ReferencedBitstring bacnet.BitString
	StatusFlags         bacnet.BitString
}

// ChangeOfValueParams is NotificationParameters change-of-value [2].
// NewValue is the CHOICE body under context tag 0 (changed-bits or changed-value).
type ChangeOfValueParams struct {
	NewValue    bacnet.ApplicationValue
	StatusFlags bacnet.BitString
}

// OutOfRangeParams is NotificationParameters out-of-range [5].
type OutOfRangeParams struct {
	ExceedingValue float32
	StatusFlags    bacnet.BitString
	Deadband       float32
	ExceededLimit  float32
}

// NotificationParameters is a typed BACnetNotificationParameters CHOICE.
//
// Unrecognized or partially supported alternatives keep RawElements (inner
// SEQUENCE of the chosen alternative). Encode prefers a typed body when set.
type NotificationParameters struct {
	Choice NotificationParametersChoice

	ChangeOfBitstring *ChangeOfBitstringParams
	ChangeOfState     *ChangeOfStateParams
	ChangeOfValue     *ChangeOfValueParams
	OutOfRange        *OutOfRangeParams

	// RawElements is the inner SEQUENCE of the chosen alternative when no
	// typed body is set (or as a decode fallback for unsupported choices).
	RawElements []bacnet.Element
}

// EncodeNotificationParameters encodes the CHOICE as elements for context tag 12.
func EncodeNotificationParameters(p NotificationParameters) ([]bacnet.Element, error) {
	inner, choice, err := encodeNotificationInner(p)
	if err != nil {
		return nil, err
	}
	return []bacnet.Element{{
		Context:   true,
		TagNumber: uint8(choice),
		Value:     bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: inner},
	}}, nil
}

func encodeNotificationInner(p NotificationParameters) ([]bacnet.Element, NotificationParametersChoice, error) {
	switch {
	case p.ChangeOfState != nil:
		ps, err := encodePropertyStates(p.ChangeOfState.NewState)
		if err != nil {
			return nil, 0, err
		}
		body := []bacnet.Element{{
			Context: true, TagNumber: 0,
			Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: ps},
		}}
		flags, err := parseContextBitStringElement(1, p.ChangeOfState.StatusFlags)
		if err != nil {
			return nil, 0, err
		}
		return append(body, flags), NotificationChangeOfState, nil
	case p.ChangeOfBitstring != nil:
		raw, err := bacnet.AppendContextBitString(nil, 0, p.ChangeOfBitstring.ReferencedBitstring)
		if err != nil {
			return nil, 0, err
		}
		raw, err = bacnet.AppendContextBitString(raw, 1, p.ChangeOfBitstring.StatusFlags)
		if err != nil {
			return nil, 0, err
		}
		els, n, err := bacnet.ParseSequence(raw, bacnet.DefaultDecodeLimits(), -1)
		if err != nil || n != len(raw) {
			return nil, 0, fmt.Errorf("%w: change-of-bitstring", bacnet.ErrMalformed)
		}
		return els, NotificationChangeOfBitstring, nil
	case p.ChangeOfValue != nil:
		body := []bacnet.Element{{
			Context: true, TagNumber: 0, Value: p.ChangeOfValue.NewValue,
		}}
		flags, err := parseContextBitStringElement(1, p.ChangeOfValue.StatusFlags)
		if err != nil {
			return nil, 0, err
		}
		return append(body, flags), NotificationChangeOfValue, nil
	case p.OutOfRange != nil:
		els, err := encodeOutOfRange(*p.OutOfRange)
		return els, NotificationOutOfRange, err
	case len(p.RawElements) > 0:
		return p.RawElements, p.Choice, nil
	default:
		return nil, 0, fmt.Errorf("%w: empty NotificationParameters", bacnet.ErrMalformed)
	}
}

func parseContextBitStringElement(tag uint8, bs bacnet.BitString) (bacnet.Element, error) {
	raw, err := bacnet.AppendContextBitString(nil, tag, bs)
	if err != nil {
		return bacnet.Element{}, err
	}
	el, n, err := bacnet.ParseTag(raw, bacnet.DefaultDecodeLimits())
	if err != nil || n != len(raw) {
		return bacnet.Element{}, fmt.Errorf("%w: statusFlags", bacnet.ErrMalformed)
	}
	return el, nil
}

func encodeOutOfRange(p OutOfRangeParams) ([]bacnet.Element, error) {
	raw, err := bacnet.AppendContextTagged(nil, 0, []bacnet.Element{{Value: bacnet.RealValue(p.ExceedingValue)}})
	if err != nil {
		return nil, err
	}
	raw, err = bacnet.AppendContextBitString(raw, 1, p.StatusFlags)
	if err != nil {
		return nil, err
	}
	raw, err = bacnet.AppendContextTagged(raw, 2, []bacnet.Element{{Value: bacnet.RealValue(p.Deadband)}})
	if err != nil {
		return nil, err
	}
	raw, err = bacnet.AppendContextTagged(raw, 3, []bacnet.Element{{Value: bacnet.RealValue(p.ExceededLimit)}})
	if err != nil {
		return nil, err
	}
	parsed, n, err := bacnet.ParseSequence(raw, bacnet.DefaultDecodeLimits(), -1)
	if err != nil || n != len(raw) {
		return nil, fmt.Errorf("%w: out-of-range", bacnet.ErrMalformed)
	}
	return parsed, nil
}

func encodePropertyStates(ps PropertyStates) ([]bacnet.Element, error) {
	switch ps.Value.Kind {
	case bacnet.ValueBoolean:
		raw, err := bacnet.AppendContextBool(nil, ps.Choice, ps.Value.Boolean)
		if err != nil {
			return nil, err
		}
		el, n, err := bacnet.ParseTag(raw, bacnet.DefaultDecodeLimits())
		if err != nil || n != len(raw) {
			return nil, fmt.Errorf("%w: PropertyStates", bacnet.ErrMalformed)
		}
		return []bacnet.Element{el}, nil
	case bacnet.ValueEnumerated:
		raw, err := bacnet.AppendContextUnsigned(nil, ps.Choice, uint64(ps.Value.Enumerated))
		if err != nil {
			return nil, err
		}
		el, n, err := bacnet.ParseTag(raw, bacnet.DefaultDecodeLimits())
		if err != nil || n != len(raw) {
			return nil, fmt.Errorf("%w: PropertyStates", bacnet.ErrMalformed)
		}
		return []bacnet.Element{el}, nil
	case bacnet.ValueUnsigned:
		raw, err := bacnet.AppendContextUnsigned(nil, ps.Choice, ps.Value.Unsigned)
		if err != nil {
			return nil, err
		}
		el, n, err := bacnet.ParseTag(raw, bacnet.DefaultDecodeLimits())
		if err != nil || n != len(raw) {
			return nil, fmt.Errorf("%w: PropertyStates", bacnet.ErrMalformed)
		}
		return []bacnet.Element{el}, nil
	default:
		return []bacnet.Element{{
			Context: true, TagNumber: ps.Choice, Value: ps.Value,
		}}, nil
	}
}

// DecodeNotificationParameters decodes context tag 12 contents (CHOICE body).
func DecodeNotificationParameters(elements []bacnet.Element) (NotificationParameters, error) {
	if len(elements) != 1 || !elements[0].Context || !bacnet.IsContextConstructed(elements[0]) {
		return NotificationParameters{}, fmt.Errorf("%w: NotificationParameters CHOICE", bacnet.ErrMalformed)
	}
	choiceEl := elements[0]
	p := NotificationParameters{
		Choice:      NotificationParametersChoice(choiceEl.TagNumber),
		RawElements: cloneElements(choiceEl.Value.Elements),
	}
	switch p.Choice {
	case NotificationChangeOfState:
		cos, err := decodeChangeOfState(choiceEl.Value.Elements)
		if err != nil {
			return NotificationParameters{}, err
		}
		p.ChangeOfState = &cos
	case NotificationChangeOfBitstring:
		cob, err := decodeChangeOfBitstring(choiceEl.Value.Elements)
		if err != nil {
			return NotificationParameters{}, err
		}
		p.ChangeOfBitstring = &cob
	case NotificationChangeOfValue:
		cov, err := decodeChangeOfValue(choiceEl.Value.Elements)
		if err != nil {
			return NotificationParameters{}, err
		}
		p.ChangeOfValue = &cov
	case NotificationOutOfRange:
		oor, err := decodeOutOfRange(choiceEl.Value.Elements)
		if err != nil {
			return NotificationParameters{}, err
		}
		p.OutOfRange = &oor
	case NotificationCommandFailure,
		NotificationFloatingLimit,
		NotificationComplexEventType,
		NotificationChangeOfLifeSafety,
		NotificationExtended,
		NotificationBufferReady,
		NotificationUnsignedRange:
		// Typed bodies not implemented yet; RawElements retained above.
	default:
		// Unknown / proprietary CHOICE tag — keep RawElements only.
	}
	return p, nil
}

func decodeChangeOfState(els []bacnet.Element) (ChangeOfStateParams, error) {
	if len(els) != 2 {
		return ChangeOfStateParams{}, fmt.Errorf("%w: change-of-state fields", bacnet.ErrMalformed)
	}
	if els[0].TagNumber != 0 || !bacnet.IsContextConstructed(els[0]) {
		return ChangeOfStateParams{}, fmt.Errorf("%w: new-state", bacnet.ErrMalformed)
	}
	if len(els[0].Value.Elements) != 1 {
		return ChangeOfStateParams{}, fmt.Errorf("%w: PropertyStates", bacnet.ErrMalformed)
	}
	psEl := els[0].Value.Elements[0]
	if !psEl.Context {
		return ChangeOfStateParams{}, fmt.Errorf("%w: PropertyStates choice", bacnet.ErrMalformed)
	}
	ps := PropertyStates{Choice: psEl.TagNumber}
	switch {
	case !bacnet.IsContextConstructed(psEl):
		if u, err := bacnet.ContextUnsigned(psEl); err == nil {
			ps.Value = bacnet.EnumValue(uint32(u))
		} else if b, err := bacnet.ContextBool(psEl); err == nil {
			ps.Value = bacnet.BoolValue(b)
		} else {
			ps.Value = bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: append([]byte(nil), psEl.Value.OctetString...)}
		}
	default:
		ps.Value = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: psEl.Value.Elements}.Clone()
	}
	if els[1].TagNumber != 1 || bacnet.IsContextConstructed(els[1]) {
		return ChangeOfStateParams{}, fmt.Errorf("%w: status-flags", bacnet.ErrMalformed)
	}
	flags, err := bacnet.ContextBitString(els[1])
	if err != nil {
		return ChangeOfStateParams{}, err
	}
	return ChangeOfStateParams{NewState: ps, StatusFlags: flags}, nil
}

func decodeChangeOfBitstring(els []bacnet.Element) (ChangeOfBitstringParams, error) {
	if len(els) != 2 {
		return ChangeOfBitstringParams{}, fmt.Errorf("%w: change-of-bitstring fields", bacnet.ErrMalformed)
	}
	if els[0].TagNumber != 0 {
		return ChangeOfBitstringParams{}, fmt.Errorf("%w: referenced-bitstring", bacnet.ErrMalformed)
	}
	ref, err := bacnet.ContextBitString(els[0])
	if err != nil {
		return ChangeOfBitstringParams{}, fmt.Errorf("%w: referenced-bitstring", bacnet.ErrMalformed)
	}
	if els[1].TagNumber != 1 {
		return ChangeOfBitstringParams{}, fmt.Errorf("%w: status-flags", bacnet.ErrMalformed)
	}
	flags, err := bacnet.ContextBitString(els[1])
	if err != nil {
		return ChangeOfBitstringParams{}, fmt.Errorf("%w: status-flags", bacnet.ErrMalformed)
	}
	return ChangeOfBitstringParams{ReferencedBitstring: ref, StatusFlags: flags}, nil
}

func decodeChangeOfValue(els []bacnet.Element) (ChangeOfValueParams, error) {
	if len(els) != 2 {
		return ChangeOfValueParams{}, fmt.Errorf("%w: change-of-value fields", bacnet.ErrMalformed)
	}
	if els[0].TagNumber != 0 {
		return ChangeOfValueParams{}, fmt.Errorf("%w: new-value", bacnet.ErrMalformed)
	}
	var newVal bacnet.ApplicationValue
	if bacnet.IsContextConstructed(els[0]) {
		newVal = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els[0].Value.Elements}.Clone()
	} else {
		newVal = bacnet.ApplicationValue{Kind: bacnet.ValueContext, Elements: []bacnet.Element{els[0]}}.Clone()
	}
	if els[1].TagNumber != 1 {
		return ChangeOfValueParams{}, fmt.Errorf("%w: status-flags", bacnet.ErrMalformed)
	}
	flags, err := bacnet.ContextBitString(els[1])
	if err != nil {
		return ChangeOfValueParams{}, fmt.Errorf("%w: status-flags", bacnet.ErrMalformed)
	}
	return ChangeOfValueParams{NewValue: newVal, StatusFlags: flags}, nil
}

func decodeOutOfRange(els []bacnet.Element) (OutOfRangeParams, error) {
	if len(els) != 4 {
		return OutOfRangeParams{}, fmt.Errorf("%w: out-of-range fields", bacnet.ErrMalformed)
	}
	exceeding, err := contextReal(els[0], 0)
	if err != nil {
		return OutOfRangeParams{}, err
	}
	if els[1].TagNumber != 1 {
		return OutOfRangeParams{}, fmt.Errorf("%w: status-flags", bacnet.ErrMalformed)
	}
	flags, err := bacnet.ContextBitString(els[1])
	if err != nil {
		return OutOfRangeParams{}, fmt.Errorf("%w: status-flags", bacnet.ErrMalformed)
	}
	deadband, err := contextReal(els[2], 2)
	if err != nil {
		return OutOfRangeParams{}, err
	}
	limit, err := contextReal(els[3], 3)
	if err != nil {
		return OutOfRangeParams{}, err
	}
	return OutOfRangeParams{
		ExceedingValue: exceeding,
		StatusFlags:    flags,
		Deadband:       deadband,
		ExceededLimit:  limit,
	}, nil
}

func contextReal(el bacnet.Element, tag uint8) (float32, error) {
	if el.TagNumber != tag {
		return 0, fmt.Errorf("%w: real tag", bacnet.ErrMalformed)
	}
	if bacnet.IsContextConstructed(el) {
		if len(el.Value.Elements) == 1 && !el.Value.Elements[0].Context &&
			el.Value.Elements[0].Value.Kind == bacnet.ValueReal {
			return el.Value.Elements[0].Value.Real, nil
		}
		return 0, fmt.Errorf("%w: real constructed", bacnet.ErrMalformed)
	}
	if len(el.Value.OctetString) != 4 {
		return 0, fmt.Errorf("%w: real length", bacnet.ErrMalformed)
	}
	bits := binary.BigEndian.Uint32(el.Value.OctetString)
	return math.Float32frombits(bits), nil
}

func cloneElements(els []bacnet.Element) []bacnet.Element {
	if els == nil {
		return nil
	}
	out := make([]bacnet.Element, len(els))
	for i, el := range els {
		out[i] = el
		out[i].Value = el.Value.Clone()
	}
	return out
}
