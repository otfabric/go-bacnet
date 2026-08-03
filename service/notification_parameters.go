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

	ChangeOfBitstring  *ChangeOfBitstringParams
	ChangeOfState      *ChangeOfStateParams
	ChangeOfValue      *ChangeOfValueParams
	OutOfRange         *OutOfRangeParams
	CommandFailure     *CommandFailureParams
	FloatingLimit      *FloatingLimitParams
	UnsignedRange      *UnsignedRangeParams
	BufferReady        *BufferReadyParams
	ChangeOfLifeSafety *ChangeOfLifeSafetyParams
	Extended           *ExtendedParams
	ComplexEventType   *ComplexEventTypeParams

	// RawElements is the inner SEQUENCE of the chosen alternative when no
	// typed body is set (or as a decode fallback for unsupported choices).
	RawElements []bacnet.Element
}

// CommandFailureParams is NotificationParameters command-failure [3].
type CommandFailureParams struct {
	CommandValue  bacnet.ApplicationValue
	StatusFlags   bacnet.BitString
	FeedbackValue bacnet.ApplicationValue
}

// FloatingLimitParams is NotificationParameters floating-limit [4].
type FloatingLimitParams struct {
	ReferenceValue float32
	StatusFlags    bacnet.BitString
	SetPointValue  float32
	ErrorLimit     float32
}

// UnsignedRangeParams is NotificationParameters unsigned-range [11].
type UnsignedRangeParams struct {
	ExceedingValue uint32
	StatusFlags    bacnet.BitString
	ExceededLimit  uint32
}

// BufferReadyParams is NotificationParameters buffer-ready [10].
type BufferReadyParams struct {
	BufferProperty []bacnet.Element // DeviceObjectPropertyReference as elements
	PreviousCount  uint32
	CurrentCount   uint32
}

// ChangeOfLifeSafetyParams is NotificationParameters change-of-life-safety [8].
type ChangeOfLifeSafetyParams struct {
	NewState          uint32 // BACnetLifeSafetyState
	NewMode           uint32 // BACnetLifeSafetyMode
	StatusFlags       bacnet.BitString
	OperationExpected uint32 // BACnetLifeSafetyOperation
}

// ExtendedParams is NotificationParameters extended [9].
type ExtendedParams struct {
	VendorID          uint16
	ExtendedEventType uint32
	Parameters        []bacnet.Element
}

// ComplexEventTypeParams is NotificationParameters complex-event-type [6].
// Values are BACnetPropertyValue SEQUENCE elements kept as constructed lists.
type ComplexEventTypeParams struct {
	Values []bacnet.Element
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
	case p.CommandFailure != nil:
		flags, err := parseContextBitStringElement(1, p.CommandFailure.StatusFlags)
		if err != nil {
			return nil, 0, err
		}
		return []bacnet.Element{
			{Context: true, TagNumber: 0, Value: p.CommandFailure.CommandValue},
			flags,
			{Context: true, TagNumber: 2, Value: p.CommandFailure.FeedbackValue},
		}, NotificationCommandFailure, nil
	case p.FloatingLimit != nil:
		els, err := encodeFloatingLimit(*p.FloatingLimit)
		return els, NotificationFloatingLimit, err
	case p.BufferReady != nil:
		prev, err := parseContextUnsignedElement(1, uint64(p.BufferReady.PreviousCount))
		if err != nil {
			return nil, 0, err
		}
		cur, err := parseContextUnsignedElement(2, uint64(p.BufferReady.CurrentCount))
		if err != nil {
			return nil, 0, err
		}
		body := append([]bacnet.Element{}, p.BufferReady.BufferProperty...)
		return append(body, prev, cur), NotificationBufferReady, nil
	case p.UnsignedRange != nil:
		ex, err := parseContextUnsignedElement(0, uint64(p.UnsignedRange.ExceedingValue))
		if err != nil {
			return nil, 0, err
		}
		flags, err := parseContextBitStringElement(1, p.UnsignedRange.StatusFlags)
		if err != nil {
			return nil, 0, err
		}
		lim, err := parseContextUnsignedElement(2, uint64(p.UnsignedRange.ExceededLimit))
		if err != nil {
			return nil, 0, err
		}
		return []bacnet.Element{ex, flags, lim}, NotificationUnsignedRange, nil
	case p.ChangeOfLifeSafety != nil:
		els, err := encodeChangeOfLifeSafety(*p.ChangeOfLifeSafety)
		return els, NotificationChangeOfLifeSafety, err
	case p.Extended != nil:
		els, err := encodeExtended(*p.Extended)
		return els, NotificationExtended, err
	case p.ComplexEventType != nil:
		return append([]bacnet.Element{}, p.ComplexEventType.Values...), NotificationComplexEventType, nil
	case len(p.RawElements) > 0:
		return p.RawElements, p.Choice, nil
	default:
		return nil, 0, fmt.Errorf("%w: empty NotificationParameters", bacnet.ErrMalformed)
	}
}

func encodeChangeOfLifeSafety(p ChangeOfLifeSafetyParams) ([]bacnet.Element, error) {
	st, err := parseContextUnsignedElement(0, uint64(p.NewState))
	if err != nil {
		return nil, err
	}
	mode, err := parseContextUnsignedElement(1, uint64(p.NewMode))
	if err != nil {
		return nil, err
	}
	flags, err := parseContextBitStringElement(2, p.StatusFlags)
	if err != nil {
		return nil, err
	}
	op, err := parseContextUnsignedElement(3, uint64(p.OperationExpected))
	if err != nil {
		return nil, err
	}
	return []bacnet.Element{st, mode, flags, op}, nil
}

func encodeExtended(p ExtendedParams) ([]bacnet.Element, error) {
	vid, err := parseContextUnsignedElement(0, uint64(p.VendorID))
	if err != nil {
		return nil, err
	}
	et, err := parseContextUnsignedElement(1, uint64(p.ExtendedEventType))
	if err != nil {
		return nil, err
	}
	body := []bacnet.Element{vid, et}
	if len(p.Parameters) > 0 {
		body = append(body, bacnet.Element{
			Context: true, TagNumber: 2,
			Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: append([]bacnet.Element{}, p.Parameters...)},
		})
	}
	return body, nil
}

func encodeFloatingLimit(p FloatingLimitParams) ([]bacnet.Element, error) {
	flags, err := parseContextBitStringElement(1, p.StatusFlags)
	if err != nil {
		return nil, err
	}
	return []bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.RealValue(p.ReferenceValue)}}}},
		flags,
		{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.RealValue(p.SetPointValue)}}}},
		{Context: true, TagNumber: 3, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.RealValue(p.ErrorLimit)}}}},
	}, nil
}

func parseContextUnsignedElement(tag uint8, v uint64) (bacnet.Element, error) {
	raw, err := bacnet.AppendContextUnsigned(nil, tag, v)
	if err != nil {
		return bacnet.Element{}, err
	}
	el, n, err := bacnet.ParseTag(raw, bacnet.DefaultDecodeLimits())
	if err != nil || n != len(raw) {
		return bacnet.Element{}, fmt.Errorf("%w: context unsigned", bacnet.ErrMalformed)
	}
	return el, nil
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
	case NotificationCommandFailure:
		cf, err := decodeCommandFailure(choiceEl.Value.Elements)
		if err != nil {
			return NotificationParameters{}, err
		}
		p.CommandFailure = &cf
	case NotificationFloatingLimit:
		fl, err := decodeFloatingLimit(choiceEl.Value.Elements)
		if err != nil {
			return NotificationParameters{}, err
		}
		p.FloatingLimit = &fl
	case NotificationBufferReady:
		br, err := decodeBufferReady(choiceEl.Value.Elements)
		if err != nil {
			return NotificationParameters{}, err
		}
		p.BufferReady = &br
	case NotificationUnsignedRange:
		ur, err := decodeUnsignedRange(choiceEl.Value.Elements)
		if err != nil {
			return NotificationParameters{}, err
		}
		p.UnsignedRange = &ur
	case NotificationChangeOfLifeSafety:
		cls, err := decodeChangeOfLifeSafety(choiceEl.Value.Elements)
		if err != nil {
			return NotificationParameters{}, err
		}
		p.ChangeOfLifeSafety = &cls
	case NotificationExtended:
		ext, err := decodeExtended(choiceEl.Value.Elements)
		if err != nil {
			return NotificationParameters{}, err
		}
		p.Extended = &ext
	case NotificationComplexEventType:
		p.ComplexEventType = &ComplexEventTypeParams{Values: cloneElements(choiceEl.Value.Elements)}
	default:
		// Unknown / proprietary CHOICE tag — keep RawElements only.
	}
	return p, nil
}

func decodeChangeOfLifeSafety(els []bacnet.Element) (ChangeOfLifeSafetyParams, error) {
	if len(els) != 4 {
		return ChangeOfLifeSafetyParams{}, fmt.Errorf("%w: change-of-life-safety fields", bacnet.ErrMalformed)
	}
	st, err := bacnet.ContextUnsigned(els[0])
	if err != nil {
		return ChangeOfLifeSafetyParams{}, err
	}
	mode, err := bacnet.ContextUnsigned(els[1])
	if err != nil {
		return ChangeOfLifeSafetyParams{}, err
	}
	flags, err := bacnet.ContextBitString(els[2])
	if err != nil {
		return ChangeOfLifeSafetyParams{}, err
	}
	op, err := bacnet.ContextUnsigned(els[3])
	if err != nil {
		return ChangeOfLifeSafetyParams{}, err
	}
	return ChangeOfLifeSafetyParams{
		NewState: uint32(st), NewMode: uint32(mode), StatusFlags: flags, OperationExpected: uint32(op),
	}, nil
}

func decodeExtended(els []bacnet.Element) (ExtendedParams, error) {
	var ext ExtendedParams
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return ExtendedParams{}, err
			}
			ext.VendorID = uint16(u)
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return ExtendedParams{}, err
			}
			ext.ExtendedEventType = uint32(u)
		case el.TagNumber == 2 && bacnet.IsContextConstructed(el):
			ext.Parameters = cloneElements(el.Value.Elements)
		}
	}
	return ext, nil
}

func decodeCommandFailure(els []bacnet.Element) (CommandFailureParams, error) {
	if len(els) != 3 {
		return CommandFailureParams{}, fmt.Errorf("%w: command-failure fields", bacnet.ErrMalformed)
	}
	flags, err := bacnet.ContextBitString(els[1])
	if err != nil {
		return CommandFailureParams{}, err
	}
	return CommandFailureParams{CommandValue: els[0].Value, StatusFlags: flags, FeedbackValue: els[2].Value}, nil
}

func decodeFloatingLimit(els []bacnet.Element) (FloatingLimitParams, error) {
	if len(els) != 4 {
		return FloatingLimitParams{}, fmt.Errorf("%w: floating-limit fields", bacnet.ErrMalformed)
	}
	ref, err := contextReal(els[0], 0)
	if err != nil {
		return FloatingLimitParams{}, err
	}
	flags, err := bacnet.ContextBitString(els[1])
	if err != nil {
		return FloatingLimitParams{}, err
	}
	set, err := contextReal(els[2], 2)
	if err != nil {
		return FloatingLimitParams{}, err
	}
	lim, err := contextReal(els[3], 3)
	if err != nil {
		return FloatingLimitParams{}, err
	}
	return FloatingLimitParams{ReferenceValue: ref, StatusFlags: flags, SetPointValue: set, ErrorLimit: lim}, nil
}

func decodeBufferReady(els []bacnet.Element) (BufferReadyParams, error) {
	var br BufferReadyParams
	for _, el := range els {
		switch {
		case el.TagNumber == 0:
			br.BufferProperty = append(br.BufferProperty, el)
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return BufferReadyParams{}, err
			}
			br.PreviousCount = uint32(u)
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return BufferReadyParams{}, err
			}
			br.CurrentCount = uint32(u)
		}
	}
	return br, nil
}

func decodeUnsignedRange(els []bacnet.Element) (UnsignedRangeParams, error) {
	if len(els) != 3 {
		return UnsignedRangeParams{}, fmt.Errorf("%w: unsigned-range fields", bacnet.ErrMalformed)
	}
	u0, err := bacnet.ContextUnsigned(els[0])
	if err != nil {
		return UnsignedRangeParams{}, err
	}
	flags, err := bacnet.ContextBitString(els[1])
	if err != nil {
		return UnsignedRangeParams{}, err
	}
	u2, err := bacnet.ContextUnsigned(els[2])
	if err != nil {
		return UnsignedRangeParams{}, err
	}
	return UnsignedRangeParams{ExceedingValue: uint32(u0), StatusFlags: flags, ExceededLimit: uint32(u2)}, nil
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
