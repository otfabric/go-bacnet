// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// AuditAddress is a BACnetAddress: a network number and a MAC address.
type AuditAddress struct {
	Network uint16
	MAC     []byte
}

// AuditRecipient is a BACnetRecipient CHOICE: exactly one of Device
// (device [0]) and Address (address [1]) is set.
type AuditRecipient struct {
	Device  *bacnet.ObjectIdentifier
	Address *AuditAddress
}

// AuditDevice returns the recipient that names a device object.
func AuditDevice(id bacnet.ObjectIdentifier) AuditRecipient {
	return AuditRecipient{Device: &id}
}

// AuditResult is the result [16] Error of an audit notification.
type AuditResult struct {
	Class uint32
	Code  uint32
	// Bare is set by the decoder when the peer sent result as a single
	// context-tagged enumeration (the error code alone) instead of the Error
	// sequence, as bacnet-stack 1.6 does; Class is then zero. The encoder
	// always writes the Error sequence.
	Bare bool
}

// AuditNotification is a BACnetAuditNotification (Clause 21):
//
//	source-timestamp [0] BACnetTimeStamp OPTIONAL
//	target-timestamp [1] BACnetTimeStamp OPTIONAL
//	source-device    [2] BACnetRecipient
//	source-object    [3] BACnetObjectIdentifier OPTIONAL
//	operation        [4] BACnetAuditOperation
//	source-comment   [5] CharacterString OPTIONAL
//	target-comment   [6] CharacterString OPTIONAL
//	invoke-id        [7] Unsigned8 OPTIONAL
//	source-user-id   [8] Unsigned16 OPTIONAL
//	source-user-role [9] Unsigned8 OPTIONAL
//	target-device    [10] BACnetRecipient
//	target-object    [11] BACnetObjectIdentifier OPTIONAL
//	target-property  [12] BACnetPropertyReference OPTIONAL
//	target-priority  [13] Unsigned (1..16) OPTIONAL
//	target-value     [14] ABSTRACT-SYNTAX.&Type OPTIONAL
//	current-value    [15] ABSTRACT-SYNTAX.&Type OPTIONAL
//	result           [16] Error OPTIONAL
//
// The layout is the one bacnet-stack 1.6 (bacaudit.c) and BACpypes3
// (basetypes.AuditNotification) encode and decode; the interop suite checks
// both directions against them.
type AuditNotification struct {
	SourceTimestamp *TimeStamp
	TargetTimestamp *TimeStamp
	SourceDevice    AuditRecipient
	SourceObject    *bacnet.ObjectIdentifier
	Operation       uint32 // BACnetAuditOperation
	SourceComment   *string
	TargetComment   *string
	InvokeID        *uint8
	SourceUserID    *uint16
	SourceUserRole  *uint8
	TargetDevice    AuditRecipient
	TargetObject    *bacnet.ObjectIdentifier
	TargetProperty  *bacnet.PropertyReference
	TargetPriority  *uint8
	// TargetValue and CurrentValue hold a single application value, or a
	// ValueConstructed value with the elements of anything else.
	TargetValue  *bacnet.ApplicationValue
	CurrentValue *bacnet.ApplicationValue
	Result       *AuditResult
}

func appendAuditRecipient(dst []byte, tag uint8, r AuditRecipient, name string) ([]byte, error) {
	var el bacnet.Element
	switch {
	case r.Device != nil && r.Address == nil:
		el = bacnet.Element{Context: true, TagNumber: 0, Value: bacnet.ObjectIDValue(*r.Device)}
	case r.Address != nil && r.Device == nil:
		el = bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{
			Kind: bacnet.ValueConstructed, Elements: auditAddressElements(*r.Address)}}
	default:
		return dst, fmt.Errorf("%w: AuditNotification %s needs exactly one of device and address", bacnet.ErrMalformed, name)
	}
	return bacnet.AppendContextTagged(dst, tag, []bacnet.Element{el})
}

func auditAddressElements(a AuditAddress) []bacnet.Element {
	return []bacnet.Element{
		{Value: bacnet.UnsignedValue(uint64(a.Network))},
		{Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: a.MAC}},
	}
}

func appendAuditAny(dst []byte, tag uint8, v bacnet.ApplicationValue) ([]byte, error) {
	if v.Kind == bacnet.ValueConstructed {
		return bacnet.AppendContextTagged(dst, tag, v.Elements)
	}
	return bacnet.AppendContextTagged(dst, tag, []bacnet.Element{{Value: v}})
}

// appendAuditNotification appends one BACnetAuditNotification.
func appendAuditNotification(dst []byte, n AuditNotification) ([]byte, error) {
	var err error
	if n.SourceTimestamp != nil {
		if dst, err = appendTimeStampField(dst, 0, *n.SourceTimestamp); err != nil {
			return nil, err
		}
	}
	if n.TargetTimestamp != nil {
		if dst, err = appendTimeStampField(dst, 1, *n.TargetTimestamp); err != nil {
			return nil, err
		}
	}
	if dst, err = appendAuditRecipient(dst, 2, n.SourceDevice, "source-device"); err != nil {
		return nil, err
	}
	if n.SourceObject != nil {
		if dst, err = bacnet.AppendContextObjectID(dst, 3, *n.SourceObject); err != nil {
			return nil, err
		}
	}
	if dst, err = bacnet.AppendContextUnsigned(dst, 4, uint64(n.Operation)); err != nil {
		return nil, err
	}
	if n.SourceComment != nil {
		if dst, err = bacnet.AppendContextCharacterString(dst, 5, bacnet.CharacterString{Value: *n.SourceComment}); err != nil {
			return nil, err
		}
	}
	if n.TargetComment != nil {
		if dst, err = bacnet.AppendContextCharacterString(dst, 6, bacnet.CharacterString{Value: *n.TargetComment}); err != nil {
			return nil, err
		}
	}
	if n.InvokeID != nil {
		if dst, err = bacnet.AppendContextUnsigned(dst, 7, uint64(*n.InvokeID)); err != nil {
			return nil, err
		}
	}
	if n.SourceUserID != nil {
		if dst, err = bacnet.AppendContextUnsigned(dst, 8, uint64(*n.SourceUserID)); err != nil {
			return nil, err
		}
	}
	if n.SourceUserRole != nil {
		if dst, err = bacnet.AppendContextUnsigned(dst, 9, uint64(*n.SourceUserRole)); err != nil {
			return nil, err
		}
	}
	if dst, err = appendAuditRecipient(dst, 10, n.TargetDevice, "target-device"); err != nil {
		return nil, err
	}
	if n.TargetObject != nil {
		if dst, err = bacnet.AppendContextObjectID(dst, 11, *n.TargetObject); err != nil {
			return nil, err
		}
	}
	if n.TargetProperty != nil {
		ref := []bacnet.Element{{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(uint64(n.TargetProperty.Identifier))}}
		if n.TargetProperty.ArrayIndex != nil {
			ref = append(ref, bacnet.Element{Context: true, TagNumber: 1, Value: bacnet.UnsignedValue(uint64(*n.TargetProperty.ArrayIndex))})
		}
		if dst, err = bacnet.AppendContextTagged(dst, 12, ref); err != nil {
			return nil, err
		}
	}
	if n.TargetPriority != nil {
		if dst, err = bacnet.AppendContextUnsigned(dst, 13, uint64(*n.TargetPriority)); err != nil {
			return nil, err
		}
	}
	if n.TargetValue != nil {
		if dst, err = appendAuditAny(dst, 14, *n.TargetValue); err != nil {
			return nil, err
		}
	}
	if n.CurrentValue != nil {
		if dst, err = appendAuditAny(dst, 15, *n.CurrentValue); err != nil {
			return nil, err
		}
	}
	if n.Result != nil {
		dst, err = bacnet.AppendContextTagged(dst, 16, []bacnet.Element{
			{Value: bacnet.EnumValue(n.Result.Class)},
			{Value: bacnet.EnumValue(n.Result.Code)},
		})
		if err != nil {
			return nil, err
		}
	}
	return dst, nil
}

// EncodeAuditNotificationRequest encodes the service request of
// ConfirmedAuditNotification and UnconfirmedAuditNotification:
//
//	notifications [0] SEQUENCE OF BACnetAuditNotification
func EncodeAuditNotificationRequest(notes []AuditNotification) ([]byte, error) {
	if len(notes) == 0 {
		return nil, fmt.Errorf("%w: AuditNotification requires a notification", bacnet.ErrMalformed)
	}
	dst := []byte{0x0E}
	var err error
	for _, n := range notes {
		if dst, err = appendAuditNotification(dst, n); err != nil {
			return nil, err
		}
	}
	return append(dst, 0x0F), nil
}

// DecodeAuditNotificationRequest decodes the service request of
// ConfirmedAuditNotification and UnconfirmedAuditNotification (see
// EncodeAuditNotificationRequest). The returned notifications own their
// memory.
func DecodeAuditNotificationRequest(payload []byte, limits bacnet.DecodeLimits) ([]AuditNotification, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return nil, err
	}
	if n != len(payload) {
		return nil, fmt.Errorf("%w: AuditNotification trailing", bacnet.ErrTrailingData)
	}
	if len(els) != 1 || els[0].TagNumber != 0 || !bacnet.IsContextConstructed(els[0]) {
		return nil, fmt.Errorf("%w: AuditNotification notifications [0]", bacnet.ErrMalformed)
	}
	notes, err := decodeAuditNotifications(els[0].Value.Elements)
	if err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return nil, fmt.Errorf("%w: AuditNotification without notification", bacnet.ErrMalformed)
	}
	return notes, nil
}

// decodeAuditNotifications splits the elements of a SEQUENCE OF
// BACnetAuditNotification. The items carry no delimiter: the field tags of
// one notification strictly increase, so a tag that does not is the first
// field of the next.
func decodeAuditNotifications(els []bacnet.Element) ([]AuditNotification, error) {
	var out []AuditNotification
	start := 0
	for i := 1; i <= len(els); i++ {
		if i < len(els) && els[i].TagNumber > els[i-1].TagNumber {
			continue
		}
		note, err := decodeAuditNotification(els[start:i])
		if err != nil {
			return nil, err
		}
		out = append(out, note)
		start = i
	}
	return out, nil
}

func decodeAuditRecipient(el bacnet.Element) (AuditRecipient, error) {
	if !bacnet.IsContextConstructed(el) || len(el.Value.Elements) != 1 {
		return AuditRecipient{}, fmt.Errorf("%w: AuditNotification recipient [%d]", bacnet.ErrMalformed, el.TagNumber)
	}
	inner := el.Value.Elements[0]
	switch {
	case inner.Context && inner.TagNumber == 0 && !bacnet.IsContextConstructed(inner):
		id, err := bacnet.ContextObjectID(inner)
		if err != nil {
			return AuditRecipient{}, err
		}
		return AuditRecipient{Device: &id}, nil
	case inner.TagNumber == 1 && bacnet.IsContextConstructed(inner):
		addr, err := decodeAuditAddress(inner.Value.Elements)
		if err != nil {
			return AuditRecipient{}, err
		}
		return AuditRecipient{Address: &addr}, nil
	}
	return AuditRecipient{}, fmt.Errorf("%w: AuditNotification recipient choice %d", bacnet.ErrMalformed, inner.TagNumber)
}

func decodeAuditAddress(els []bacnet.Element) (AuditAddress, error) {
	if len(els) != 2 || els[0].Context || els[1].Context ||
		els[0].Value.Kind != bacnet.ValueUnsigned || els[0].Value.Unsigned > 0xFFFF ||
		els[1].Value.Kind != bacnet.ValueOctetString {
		return AuditAddress{}, fmt.Errorf("%w: BACnetAddress", bacnet.ErrMalformed)
	}
	return AuditAddress{
		Network: uint16(els[0].Value.Unsigned),
		MAC:     append([]byte{}, els[1].Value.OctetString...),
	}, nil
}

func decodeAuditAny(el bacnet.Element) (*bacnet.ApplicationValue, error) {
	if !bacnet.IsContextConstructed(el) {
		return nil, fmt.Errorf("%w: AuditNotification value [%d]", bacnet.ErrMalformed, el.TagNumber)
	}
	var v bacnet.ApplicationValue
	if len(el.Value.Elements) == 1 && !el.Value.Elements[0].Context {
		v = el.Value.Elements[0].Value.Clone()
	} else {
		v = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: el.Value.Elements}.Clone()
	}
	return &v, nil
}

func auditSmallUnsigned(el bacnet.Element, max uint32) (uint32, error) {
	if bacnet.IsContextConstructed(el) {
		return 0, fmt.Errorf("%w: AuditNotification field [%d]", bacnet.ErrMalformed, el.TagNumber)
	}
	u, err := bacnet.ContextUnsigned32(el)
	if err != nil {
		return 0, err
	}
	if u > max {
		return 0, fmt.Errorf("%w: AuditNotification field [%d] out of range", bacnet.ErrMalformed, el.TagNumber)
	}
	return u, nil
}

// decodeAuditNotification decodes the elements of one BACnetAuditNotification.
func decodeAuditNotification(els []bacnet.Element) (AuditNotification, error) {
	var note AuditNotification
	var haveSource, haveOperation, haveTarget bool
	fail := func(err error) (AuditNotification, error) { return AuditNotification{}, err }
	for _, el := range els {
		if !el.Context {
			return fail(fmt.Errorf("%w: AuditNotification application-tagged field", bacnet.ErrMalformed))
		}
		constructed := bacnet.IsContextConstructed(el)
		switch el.TagNumber {
		case 0, 1:
			if !constructed || len(el.Value.Elements) != 1 {
				return fail(fmt.Errorf("%w: AuditNotification timestamp [%d]", bacnet.ErrMalformed, el.TagNumber))
			}
			ts, err := DecodeTimeStamp(el.Value.Elements[0])
			if err != nil {
				return fail(err)
			}
			if el.TagNumber == 0 {
				note.SourceTimestamp = &ts
			} else {
				note.TargetTimestamp = &ts
			}
		case 2, 10:
			r, err := decodeAuditRecipient(el)
			if err != nil {
				return fail(err)
			}
			if el.TagNumber == 2 {
				note.SourceDevice, haveSource = r, true
			} else {
				note.TargetDevice, haveTarget = r, true
			}
		case 3, 11:
			if constructed {
				return fail(fmt.Errorf("%w: AuditNotification object [%d]", bacnet.ErrMalformed, el.TagNumber))
			}
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return fail(err)
			}
			if el.TagNumber == 3 {
				note.SourceObject = &id
			} else {
				note.TargetObject = &id
			}
		case 4:
			u, err := auditSmallUnsigned(el, 0xFFFFFFFF)
			if err != nil {
				return fail(err)
			}
			note.Operation, haveOperation = u, true
		case 5, 6:
			if constructed {
				return fail(fmt.Errorf("%w: AuditNotification comment [%d]", bacnet.ErrMalformed, el.TagNumber))
			}
			cs, err := bacnet.ContextCharacterString(el)
			if err != nil {
				return fail(err)
			}
			text := cs.Value
			if el.TagNumber == 5 {
				note.SourceComment = &text
			} else {
				note.TargetComment = &text
			}
		case 7, 9, 13:
			u, err := auditSmallUnsigned(el, 0xFF)
			if err != nil {
				return fail(err)
			}
			b := uint8(u)
			switch el.TagNumber {
			case 7:
				note.InvokeID = &b
			case 9:
				note.SourceUserRole = &b
			default:
				note.TargetPriority = &b
			}
		case 8:
			u, err := auditSmallUnsigned(el, 0xFFFF)
			if err != nil {
				return fail(err)
			}
			id := uint16(u)
			note.SourceUserID = &id
		case 12:
			ref, err := decodeAuditPropertyReference(el)
			if err != nil {
				return fail(err)
			}
			note.TargetProperty = &ref
		case 14, 15:
			v, err := decodeAuditAny(el)
			if err != nil {
				return fail(err)
			}
			if el.TagNumber == 14 {
				note.TargetValue = v
			} else {
				note.CurrentValue = v
			}
		case 16:
			res, err := decodeAuditResult(el)
			if err != nil {
				return fail(err)
			}
			note.Result = &res
		default:
			return fail(fmt.Errorf("%w: AuditNotification tag %d", bacnet.ErrMalformed, el.TagNumber))
		}
	}
	if !haveSource || !haveOperation || !haveTarget {
		return fail(fmt.Errorf("%w: AuditNotification missing source-device, operation or target-device", bacnet.ErrMalformed))
	}
	return note, nil
}

func decodeAuditPropertyReference(el bacnet.Element) (bacnet.PropertyReference, error) {
	bad := fmt.Errorf("%w: AuditNotification target-property", bacnet.ErrMalformed)
	if !bacnet.IsContextConstructed(el) {
		return bacnet.PropertyReference{}, bad
	}
	in := el.Value.Elements
	if len(in) < 1 || len(in) > 2 || !in[0].Context || in[0].TagNumber != 0 || bacnet.IsContextConstructed(in[0]) {
		return bacnet.PropertyReference{}, bad
	}
	id, err := bacnet.ContextUnsigned32(in[0])
	if err != nil {
		return bacnet.PropertyReference{}, err
	}
	ref := bacnet.PropertyReference{Identifier: bacnet.PropertyIdentifier(id)}
	if len(in) == 2 {
		if !in[1].Context || in[1].TagNumber != 1 || bacnet.IsContextConstructed(in[1]) {
			return bacnet.PropertyReference{}, bad
		}
		idx, err := bacnet.ContextUnsigned32(in[1])
		if err != nil {
			return bacnet.PropertyReference{}, err
		}
		ref.ArrayIndex = &idx
	}
	return ref, nil
}

func decodeAuditResult(el bacnet.Element) (AuditResult, error) {
	if !bacnet.IsContextConstructed(el) {
		// bacnet-stack 1.6: the error code as a bare enumeration.
		code, err := bacnet.ContextUnsigned32(el)
		if err != nil {
			return AuditResult{}, err
		}
		return AuditResult{Code: code, Bare: true}, nil
	}
	in := el.Value.Elements
	if len(in) != 2 || in[0].Context || in[1].Context ||
		in[0].Value.Kind != bacnet.ValueEnumerated || in[1].Value.Kind != bacnet.ValueEnumerated {
		return AuditResult{}, fmt.Errorf("%w: AuditNotification result", bacnet.ErrMalformed)
	}
	return AuditResult{Class: in[0].Value.Enumerated, Code: in[1].Value.Enumerated}, nil
}

// AuditSuccessFilter is the successful-actions-only parameter of an
// AuditLogQuery. It is a BACnetSuccessFilter enumeration; revisions that
// define it as BOOLEAN encode FALSE and TRUE as the same octets as All and
// SuccessesOnly.
type AuditSuccessFilter uint32

// AuditSuccessFilter values.
const (
	AuditSuccessAll           AuditSuccessFilter = 0
	AuditSuccessSuccessesOnly AuditSuccessFilter = 1
	AuditSuccessFailuresOnly  AuditSuccessFilter = 2
)

// AuditQueryByTarget is the by-target alternative of
// BACnetAuditLogQueryParameters:
//
//	target-device-identifier   [0] BACnetObjectIdentifier
//	target-device-address      [1] BACnetAddress OPTIONAL
//	target-object-identifier   [2] BACnetObjectIdentifier OPTIONAL
//	target-property-identifier [3] BACnetPropertyIdentifier OPTIONAL
//	target-array-index         [4] Unsigned OPTIONAL
//	target-priority            [5] Unsigned (1..16) OPTIONAL
//	operations                 [6] BACnetAuditOperationFlags OPTIONAL
//	successful-actions-only    [7]
type AuditQueryByTarget struct {
	TargetDevice     bacnet.ObjectIdentifier
	TargetAddress    *AuditAddress
	TargetObject     *bacnet.ObjectIdentifier
	TargetProperty   *bacnet.PropertyIdentifier
	TargetArrayIndex *uint32
	TargetPriority   *uint8
	Operations       *bacnet.BitString
	SuccessFilter    AuditSuccessFilter
}

// AuditQueryBySource is the by-source alternative of
// BACnetAuditLogQueryParameters:
//
//	source-device-identifier [0] BACnetObjectIdentifier
//	source-device-address    [1] BACnetAddress OPTIONAL
//	source-object-identifier [2] BACnetObjectIdentifier OPTIONAL
//	operations               [3] BACnetAuditOperationFlags OPTIONAL
//	successful-actions-only  [4]
type AuditQueryBySource struct {
	SourceDevice  bacnet.ObjectIdentifier
	SourceAddress *AuditAddress
	SourceObject  *bacnet.ObjectIdentifier
	Operations    *bacnet.BitString
	SuccessFilter AuditSuccessFilter
}

// AuditLogQueryRequest is an AuditLogQuery request:
//
//	audit-log                [0] BACnetObjectIdentifier
//	query-parameters         [1] CHOICE { by-target [0], by-source [1] }
//	start-at-sequence-number [2] Unsigned64 OPTIONAL
//	requested-count          [3] Unsigned16
//
// Exactly one of ByTarget and BySource is set. The layout is the one
// Wireshark dissects and BACpypes3's AuditLogQueryParameters decodes.
type AuditLogQueryRequest struct {
	AuditLog              bacnet.ObjectIdentifier
	ByTarget              *AuditQueryByTarget
	BySource              *AuditQueryBySource
	StartAtSequenceNumber *uint64
	RequestedCount        uint16
}

func auditQueryElement(tag uint8, v bacnet.ApplicationValue) bacnet.Element {
	return bacnet.Element{Context: true, TagNumber: tag, Value: v}
}

func auditQueryAddress(tag uint8, a AuditAddress) bacnet.Element {
	return auditQueryElement(tag, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: auditAddressElements(a)})
}

// EncodeAuditLogQuery encodes an AuditLogQuery request (see
// AuditLogQueryRequest for the layout).
func EncodeAuditLogQuery(req AuditLogQueryRequest) ([]byte, error) {
	dst, err := bacnet.AppendContextObjectID(nil, 0, req.AuditLog)
	if err != nil {
		return nil, err
	}
	var choice uint8
	var params []bacnet.Element
	switch {
	case req.ByTarget != nil && req.BySource == nil:
		q := req.ByTarget
		params = append(params, auditQueryElement(0, bacnet.ObjectIDValue(q.TargetDevice)))
		if q.TargetAddress != nil {
			params = append(params, auditQueryAddress(1, *q.TargetAddress))
		}
		if q.TargetObject != nil {
			params = append(params, auditQueryElement(2, bacnet.ObjectIDValue(*q.TargetObject)))
		}
		if q.TargetProperty != nil {
			params = append(params, auditQueryElement(3, bacnet.EnumValue(uint32(*q.TargetProperty))))
		}
		if q.TargetArrayIndex != nil {
			params = append(params, auditQueryElement(4, bacnet.UnsignedValue(uint64(*q.TargetArrayIndex))))
		}
		if q.TargetPriority != nil {
			params = append(params, auditQueryElement(5, bacnet.UnsignedValue(uint64(*q.TargetPriority))))
		}
		if q.Operations != nil {
			params = append(params, auditQueryElement(6, bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: *q.Operations}))
		}
		params = append(params, auditQueryElement(7, bacnet.EnumValue(uint32(q.SuccessFilter))))
	case req.BySource != nil && req.ByTarget == nil:
		choice = 1
		q := req.BySource
		params = append(params, auditQueryElement(0, bacnet.ObjectIDValue(q.SourceDevice)))
		if q.SourceAddress != nil {
			params = append(params, auditQueryAddress(1, *q.SourceAddress))
		}
		if q.SourceObject != nil {
			params = append(params, auditQueryElement(2, bacnet.ObjectIDValue(*q.SourceObject)))
		}
		if q.Operations != nil {
			params = append(params, auditQueryElement(3, bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: *q.Operations}))
		}
		params = append(params, auditQueryElement(4, bacnet.EnumValue(uint32(q.SuccessFilter))))
	default:
		return nil, fmt.Errorf("%w: AuditLogQuery needs exactly one of by-target and by-source", bacnet.ErrMalformed)
	}
	dst, err = bacnet.AppendContextTagged(dst, 1, []bacnet.Element{
		auditQueryElement(choice, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: params}),
	})
	if err != nil {
		return nil, err
	}
	if req.StartAtSequenceNumber != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 2, *req.StartAtSequenceNumber)
		if err != nil {
			return nil, err
		}
	}
	return bacnet.AppendContextUnsigned(dst, 3, uint64(req.RequestedCount))
}

// AuditLogRecordResult is one record of an AuditLogQuery-ACK: the sequence
// number and a BACnetAuditLogRecord, whose log-datum is exactly one of
// LogStatus, Notification and TimeChange.
type AuditLogRecordResult struct {
	SequenceNumber uint64
	Timestamp      DateTime
	LogStatus      *bacnet.BitString  // log-status [0] BACnetLogStatus
	Notification   *AuditNotification // audit-notification [1]
	TimeChange     *float32           // time-change [2] REAL
}

// AuditLogQueryACK is an AuditLogQuery-ACK:
//
//	audit-log     [0] BACnetObjectIdentifier
//	records       [1] SEQUENCE OF SEQUENCE {
//	  sequence-number [0] Unsigned64
//	  log-record      [1] BACnetAuditLogRecord {
//	    timestamp [0] BACnetDateTime
//	    log-datum [1] CHOICE { log-status [0], audit-notification [1], time-change [2] } } }
//	no-more-items [2] BOOLEAN
//
// The interop suite decodes ACKs whose records bacnet-stack 1.6
// (bacnet_audit_log_record_encode) and BACpypes3 (AuditLogRecordResult)
// encode.
type AuditLogQueryACK struct {
	AuditLog    bacnet.ObjectIdentifier
	Records     []AuditLogRecordResult
	NoMoreItems bool
}

// EncodeAuditLogQueryACK encodes an AuditLogQuery-ACK.
func EncodeAuditLogQueryACK(ack AuditLogQueryACK) ([]byte, error) {
	dst, err := bacnet.AppendContextObjectID(nil, 0, ack.AuditLog)
	if err != nil {
		return nil, err
	}
	dst = append(dst, 0x1E) // opening records [1]
	for _, rec := range ack.Records {
		if dst, err = bacnet.AppendContextUnsigned(dst, 0, rec.SequenceNumber); err != nil {
			return nil, err
		}
		dst = append(dst, 0x1E) // opening log-record [1]
		dst, err = bacnet.AppendContextTagged(dst, 0, []bacnet.Element{
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueDate, Date: rec.Timestamp.Date}},
			{Value: bacnet.ApplicationValue{Kind: bacnet.ValueTime, Time: rec.Timestamp.Time}},
		})
		if err != nil {
			return nil, err
		}
		dst = append(dst, 0x1E) // opening log-datum [1]
		switch {
		case rec.LogStatus != nil && rec.Notification == nil && rec.TimeChange == nil:
			dst, err = bacnet.AppendContextBitString(dst, 0, *rec.LogStatus)
		case rec.Notification != nil && rec.LogStatus == nil && rec.TimeChange == nil:
			dst = append(dst, 0x1E)
			if dst, err = appendAuditNotification(dst, *rec.Notification); err == nil {
				dst = append(dst, 0x1F)
			}
		case rec.TimeChange != nil && rec.LogStatus == nil && rec.Notification == nil:
			dst, err = bacnet.AppendContextReal(dst, 2, *rec.TimeChange)
		default:
			err = fmt.Errorf("%w: AuditLogRecord needs exactly one log-datum", bacnet.ErrMalformed)
		}
		if err != nil {
			return nil, err
		}
		dst = append(dst, 0x1F, 0x1F) // closing log-datum, log-record
	}
	dst = append(dst, 0x1F)
	return bacnet.AppendContextBool(dst, 2, ack.NoMoreItems)
}

// DecodeAuditLogQueryACK decodes an AuditLogQuery-ACK (see AuditLogQueryACK
// for the layout). The result owns its memory.
func DecodeAuditLogQueryACK(payload []byte, limits bacnet.DecodeLimits) (AuditLogQueryACK, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AuditLogQueryACK{}, err
	}
	if n != len(payload) {
		return AuditLogQueryACK{}, fmt.Errorf("%w: AuditLogQueryACK trailing", bacnet.ErrTrailingData)
	}
	bad := func(what string) (AuditLogQueryACK, error) {
		return AuditLogQueryACK{}, fmt.Errorf("%w: AuditLogQueryACK %s", bacnet.ErrMalformed, what)
	}
	if len(els) != 3 {
		return bad("needs audit-log, records and no-more-items")
	}
	if !els[0].Context || els[0].TagNumber != 0 || bacnet.IsContextConstructed(els[0]) {
		return bad("audit-log [0]")
	}
	var ack AuditLogQueryACK
	if ack.AuditLog, err = bacnet.ContextObjectID(els[0]); err != nil {
		return AuditLogQueryACK{}, err
	}
	if els[1].TagNumber != 1 || !bacnet.IsContextConstructed(els[1]) {
		return bad("records [1]")
	}
	recs := els[1].Value.Elements
	if len(recs)%2 != 0 {
		return bad("records [1] contents")
	}
	for i := 0; i < len(recs); i += 2 {
		seq, rec := recs[i], recs[i+1]
		if !seq.Context || seq.TagNumber != 0 || bacnet.IsContextConstructed(seq) ||
			rec.TagNumber != 1 || !bacnet.IsContextConstructed(rec) {
			return bad("record")
		}
		out, err := decodeAuditLogRecord(rec.Value.Elements)
		if err != nil {
			return AuditLogQueryACK{}, err
		}
		if out.SequenceNumber, err = bacnet.ContextUnsigned(seq); err != nil {
			return AuditLogQueryACK{}, err
		}
		ack.Records = append(ack.Records, out)
	}
	if !els[2].Context || els[2].TagNumber != 2 || bacnet.IsContextConstructed(els[2]) {
		return bad("no-more-items [2]")
	}
	if ack.NoMoreItems, err = bacnet.ContextBool(els[2]); err != nil {
		return AuditLogQueryACK{}, err
	}
	return ack, nil
}

// decodeAuditLogRecord decodes the elements of a BACnetAuditLogRecord.
func decodeAuditLogRecord(els []bacnet.Element) (AuditLogRecordResult, error) {
	bad := func(what string) (AuditLogRecordResult, error) {
		return AuditLogRecordResult{}, fmt.Errorf("%w: AuditLogRecord %s", bacnet.ErrMalformed, what)
	}
	if len(els) != 2 || els[0].TagNumber != 0 || !bacnet.IsContextConstructed(els[0]) ||
		els[1].TagNumber != 1 || !bacnet.IsContextConstructed(els[1]) {
		return bad("needs timestamp [0] and log-datum [1]")
	}
	ts := els[0].Value.Elements
	if len(ts) != 2 || ts[0].Context || ts[1].Context ||
		ts[0].Value.Kind != bacnet.ValueDate || ts[1].Value.Kind != bacnet.ValueTime {
		return bad("timestamp")
	}
	rec := AuditLogRecordResult{Timestamp: DateTime{Date: ts[0].Value.Date, Time: ts[1].Value.Time}}
	if len(els[1].Value.Elements) != 1 || !els[1].Value.Elements[0].Context {
		return bad("log-datum")
	}
	datum := els[1].Value.Elements[0]
	constructed := bacnet.IsContextConstructed(datum)
	switch {
	case datum.TagNumber == 0 && !constructed:
		bs, err := bacnet.ContextBitString(datum)
		if err != nil {
			return AuditLogRecordResult{}, err
		}
		bs.Bytes = append([]byte{}, bs.Bytes...)
		rec.LogStatus = &bs
	case datum.TagNumber == 1 && constructed:
		note, err := decodeAuditNotification(datum.Value.Elements)
		if err != nil {
			return AuditLogRecordResult{}, err
		}
		rec.Notification = &note
	case datum.TagNumber == 2 && !constructed:
		r, err := bacnet.ContextReal(datum)
		if err != nil {
			return AuditLogRecordResult{}, err
		}
		rec.TimeChange = &r
	default:
		return bad("log-datum choice")
	}
	return rec, nil
}

// WhoAmI is an Unconfirmed Who-Am-I request.
//
// Wire form matches ASHRAE 135 Who-Am-I-Request SEQUENCE without context tags
// (application Unsigned16 + CharacterString + CharacterString), same as I-Am.
type WhoAmI struct {
	VendorID     uint16
	ModelName    string
	SerialNumber string
}

// EncodeWhoAmI encodes Who-Am-I.
func EncodeWhoAmI(w WhoAmI) ([]byte, error) {
	dst, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(uint64(w.VendorID)))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: w.ModelName},
	})
	if err != nil {
		return nil, err
	}
	return bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: w.SerialNumber},
	})
}

// DecodeWhoAmI decodes Who-Am-I.
func DecodeWhoAmI(payload []byte, limits bacnet.DecodeLimits) (WhoAmI, error) {
	off := 0
	vendor, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return WhoAmI{}, err
	}
	off += n
	model, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return WhoAmI{}, err
	}
	off += n
	serial, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return WhoAmI{}, err
	}
	off += n
	if off != len(payload) {
		return WhoAmI{}, fmt.Errorf("%w: WhoAmI trailing", bacnet.ErrTrailingData)
	}
	if vendor.Kind != bacnet.ValueUnsigned || vendor.Unsigned > 0xffff {
		return WhoAmI{}, fmt.Errorf("%w: WhoAmI vendor-id", bacnet.ErrMalformed)
	}
	if model.Kind != bacnet.ValueCharacterString || serial.Kind != bacnet.ValueCharacterString {
		return WhoAmI{}, fmt.Errorf("%w: WhoAmI character strings", bacnet.ErrMalformed)
	}
	return WhoAmI{
		VendorID:     uint16(vendor.Unsigned),
		ModelName:    model.Character.Value,
		SerialNumber: serial.Character.Value,
	}, nil
}

// YouAre is an Unconfirmed You-Are request.
//
// Wire form matches ASHRAE 135 You-Are-Request SEQUENCE without context tags:
// application Unsigned16 + CharacterString + CharacterString, then the
// optional device-identifier ObjectIdentifier and the optional
// device-mac-address OctetString.
type YouAre struct {
	VendorID     uint16
	ModelName    string
	SerialNumber string
	// Device is the optional device-identifier. The zero value means absent:
	// a device identifier always has object type Device, so the zero value
	// (analog-input, 0) is never a valid one.
	Device bacnet.ObjectIdentifier
	// MAC is the optional device-mac-address; nil means absent.
	MAC []byte
}

// HasDevice reports whether the optional device-identifier is present.
func (y YouAre) HasDevice() bool { return y.Device != (bacnet.ObjectIdentifier{}) }

// EncodeYouAre encodes You-Are.
func EncodeYouAre(y YouAre) ([]byte, error) {
	dst, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(uint64(y.VendorID)))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: y.ModelName},
	})
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: y.SerialNumber},
	})
	if err != nil {
		return nil, err
	}
	if y.HasDevice() {
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.ObjectIDValue(y.Device))
		if err != nil {
			return nil, err
		}
	}
	if y.MAC != nil {
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: y.MAC})
		if err != nil {
			return nil, err
		}
	}
	return dst, nil
}

// DecodeYouAre decodes You-Are.
func DecodeYouAre(payload []byte, limits bacnet.DecodeLimits) (YouAre, error) {
	off := 0
	vendor, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return YouAre{}, err
	}
	off += n
	model, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return YouAre{}, err
	}
	off += n
	serial, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return YouAre{}, err
	}
	off += n
	if vendor.Kind != bacnet.ValueUnsigned || vendor.Unsigned > 0xffff {
		return YouAre{}, fmt.Errorf("%w: YouAre vendor-id", bacnet.ErrMalformed)
	}
	if model.Kind != bacnet.ValueCharacterString || serial.Kind != bacnet.ValueCharacterString {
		return YouAre{}, fmt.Errorf("%w: YouAre character strings", bacnet.ErrMalformed)
	}
	y := YouAre{
		VendorID:     uint16(vendor.Unsigned),
		ModelName:    model.Character.Value,
		SerialNumber: serial.Character.Value,
	}
	// Optional device-identifier, then optional device-mac-address.
	var haveDevice, haveMAC bool
	for off < len(payload) {
		v, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
		if err != nil {
			return YouAre{}, err
		}
		switch {
		case v.Kind == bacnet.ValueObjectID && !haveDevice && !haveMAC:
			y.Device = v.ObjectID
			haveDevice = true
		case v.Kind == bacnet.ValueOctetString && !haveMAC:
			y.MAC = append([]byte{}, v.OctetString...)
			haveMAC = true
		case haveMAC:
			return YouAre{}, fmt.Errorf("%w: YouAre trailing", bacnet.ErrTrailingData)
		default:
			return YouAre{}, fmt.Errorf("%w: YouAre optional field", bacnet.ErrMalformed)
		}
		off += n
	}
	return y, nil
}

// AuthRequest is a confirmed AuthRequest (opaque parameters preserved).
type AuthRequest struct {
	Parameters []bacnet.Element
}

// EncodeAuthRequest encodes AuthRequest as a constructed context [0] list when non-empty.
func EncodeAuthRequest(r AuthRequest) ([]byte, error) {
	if len(r.Parameters) == 0 {
		return nil, nil
	}
	return bacnet.AppendContextTagged(nil, 0, r.Parameters)
}

// DecodeAuthRequest decodes AuthRequest.
func DecodeAuthRequest(payload []byte, limits bacnet.DecodeLimits) (AuthRequest, error) {
	if len(payload) == 0 {
		return AuthRequest{}, nil
	}
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AuthRequest{}, err
	}
	if n != len(payload) {
		return AuthRequest{}, fmt.Errorf("%w: AuthRequest trailing", bacnet.ErrTrailingData)
	}
	return AuthRequest{Parameters: cloneElements(els)}, nil
}
