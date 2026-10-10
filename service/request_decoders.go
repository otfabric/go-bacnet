// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// DecodeGetAlarmSummary decodes an empty GetAlarmSummary request payload.
func DecodeGetAlarmSummary(payload []byte, limits bacnet.DecodeLimits) error {
	if len(payload) != 0 {
		return fmt.Errorf("%w: GetAlarmSummary trailing", bacnet.ErrTrailingData)
	}
	return nil
}

// DecodeGetEnrollmentSummary decodes a GetEnrollmentSummary request.
func DecodeGetEnrollmentSummary(payload []byte, limits bacnet.DecodeLimits) (GetEnrollmentSummaryRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return GetEnrollmentSummaryRequest{}, err
	}
	if n != len(payload) {
		return GetEnrollmentSummaryRequest{}, fmt.Errorf("%w: GetEnrollmentSummary trailing", bacnet.ErrTrailingData)
	}
	var req GetEnrollmentSummaryRequest
	var haveAck bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				req.AcknowledgmentFilter = EnrollmentFilter(u)
				haveAck = true
			}
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned32(el)
			err = e
			if err == nil {
				v := u
				req.EventStateFilter = &v
			}
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned32(el)
			err = e
			if err == nil {
				v := u
				req.EventTypeFilter = &v
			}
		case el.TagNumber == 4 && bacnet.IsContextConstructed(el):
			// priorityFilter [4] SEQUENCE { minPriority [0] Unsigned8, maxPriority [1] Unsigned8 }
			inner := el.Value.Elements
			if len(inner) != 2 || inner[0].TagNumber != 0 || inner[1].TagNumber != 1 {
				return GetEnrollmentSummaryRequest{}, fmt.Errorf("%w: GetEnrollmentSummary priorityFilter", bacnet.ErrMalformed)
			}
			lo, e1 := bacnet.ContextUnsigned(inner[0])
			hi, e2 := bacnet.ContextUnsigned(inner[1])
			if e1 != nil || e2 != nil || lo > 255 || hi > 255 {
				return GetEnrollmentSummaryRequest{}, fmt.Errorf("%w: GetEnrollmentSummary priorityFilter", bacnet.ErrMalformed)
			}
			pri := [2]uint32{uint32(lo), uint32(hi)}
			req.PriorityFilter = &pri
		case el.TagNumber == 5 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned32(el)
			err = e
			if err == nil {
				v := u
				req.NotificationClassFilter = &v
			}
		}
		if err != nil {
			return GetEnrollmentSummaryRequest{}, err
		}
	}
	if !haveAck {
		return GetEnrollmentSummaryRequest{}, fmt.Errorf("%w: GetEnrollmentSummary acknowledgmentFilter", bacnet.ErrMalformed)
	}
	return req, nil
}

// DecodeAtomicReadFile decodes an AtomicReadFile request.
//
// Accepts only the Clause-17 shape: application ObjectIdentifier plus exactly
// one access CHOICE ([0] stream or [1] record). Rejects the pre-v0.2.3 form
// that used context-tagged fileIdentifier and CHOICE tags 1/2.
func DecodeAtomicReadFile(payload []byte, limits bacnet.DecodeLimits) (AtomicReadFileRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AtomicReadFileRequest{}, err
	}
	if n != len(payload) {
		return AtomicReadFileRequest{}, fmt.Errorf("%w: AtomicReadFile trailing", bacnet.ErrTrailingData)
	}
	var req AtomicReadFileRequest
	var haveFile, haveAccess bool
	for _, el := range els {
		switch {
		case !el.Context && el.Value.Kind == bacnet.ValueObjectID:
			if haveFile {
				return AtomicReadFileRequest{}, fmt.Errorf("%w: AtomicReadFile duplicate fileIdentifier", bacnet.ErrMalformed)
			}
			req.File = el.Value.ObjectID
			haveFile = true
		case el.TagNumber == 0 && bacnet.IsContextConstructed(el):
			if haveAccess {
				return AtomicReadFileRequest{}, fmt.Errorf("%w: AtomicReadFile duplicate accessMethod", bacnet.ErrMalformed)
			}
			req.Access = FileAccessStream
			err = decodeFileAccessParams(el.Value.Elements, &req)
			haveAccess = true
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el):
			if haveAccess {
				return AtomicReadFileRequest{}, fmt.Errorf("%w: AtomicReadFile duplicate accessMethod", bacnet.ErrMalformed)
			}
			req.Access = FileAccessRecord
			err = decodeFileAccessParams(el.Value.Elements, &req)
			haveAccess = true
		default:
			return AtomicReadFileRequest{}, fmt.Errorf("%w: AtomicReadFile tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return AtomicReadFileRequest{}, err
		}
	}
	if !haveFile || !haveAccess {
		return AtomicReadFileRequest{}, fmt.Errorf("%w: AtomicReadFile missing fields", bacnet.ErrMalformed)
	}
	return req, nil
}

func decodeFileAccessParams(els []bacnet.Element, req *AtomicReadFileRequest) error {
	if len(els) != 2 {
		return fmt.Errorf("%w: file access params", bacnet.ErrMalformed)
	}
	if els[0].Context || els[0].Value.Kind != bacnet.ValueSigned {
		return fmt.Errorf("%w: start position", bacnet.ErrMalformed)
	}
	req.StartPosition = int32(els[0].Value.Signed)
	u, err := bacnet.AsUnsigned(els[1].Value)
	if err != nil {
		return fmt.Errorf("%w: count", bacnet.ErrMalformed)
	}
	req.Count = uint32(u)
	return nil
}

// DecodeAtomicWriteFile decodes an AtomicWriteFile request.
//
// Accepts only application ObjectIdentifier plus access CHOICE [0]/[1].
func DecodeAtomicWriteFile(payload []byte, limits bacnet.DecodeLimits) (AtomicWriteFileRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AtomicWriteFileRequest{}, err
	}
	if n != len(payload) {
		return AtomicWriteFileRequest{}, fmt.Errorf("%w: AtomicWriteFile trailing", bacnet.ErrTrailingData)
	}
	var req AtomicWriteFileRequest
	var haveFile, haveAccess bool
	for _, el := range els {
		switch {
		case !el.Context && el.Value.Kind == bacnet.ValueObjectID:
			if haveFile {
				return AtomicWriteFileRequest{}, fmt.Errorf("%w: AtomicWriteFile duplicate fileIdentifier", bacnet.ErrMalformed)
			}
			req.File = el.Value.ObjectID
			haveFile = true
		case el.TagNumber == 0 && bacnet.IsContextConstructed(el):
			if haveAccess {
				return AtomicWriteFileRequest{}, fmt.Errorf("%w: AtomicWriteFile duplicate accessMethod", bacnet.ErrMalformed)
			}
			req.Access = FileAccessStream
			err = decodeWriteStreamParams(el.Value.Elements, &req)
			haveAccess = true
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el):
			if haveAccess {
				return AtomicWriteFileRequest{}, fmt.Errorf("%w: AtomicWriteFile duplicate accessMethod", bacnet.ErrMalformed)
			}
			req.Access = FileAccessRecord
			err = decodeWriteRecordParams(el.Value.Elements, &req)
			haveAccess = true
		default:
			return AtomicWriteFileRequest{}, fmt.Errorf("%w: AtomicWriteFile tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return AtomicWriteFileRequest{}, err
		}
	}
	if !haveFile || !haveAccess {
		return AtomicWriteFileRequest{}, fmt.Errorf("%w: AtomicWriteFile missing fields", bacnet.ErrMalformed)
	}
	return req, nil
}

func decodeWriteStreamParams(els []bacnet.Element, req *AtomicWriteFileRequest) error {
	if len(els) != 2 {
		return fmt.Errorf("%w: stream write params", bacnet.ErrMalformed)
	}
	if els[0].Context || els[0].Value.Kind != bacnet.ValueSigned {
		return fmt.Errorf("%w: start position", bacnet.ErrMalformed)
	}
	req.StartPosition = int32(els[0].Value.Signed)
	if els[1].Context || els[1].Value.Kind != bacnet.ValueOctetString {
		return fmt.Errorf("%w: fileData", bacnet.ErrMalformed)
	}
	req.Data = append([]byte(nil), els[1].Value.OctetString...)
	return nil
}

func decodeWriteRecordParams(els []bacnet.Element, req *AtomicWriteFileRequest) error {
	if len(els) < 2 {
		return fmt.Errorf("%w: record write params", bacnet.ErrMalformed)
	}
	if els[0].Context || els[0].Value.Kind != bacnet.ValueSigned {
		return fmt.Errorf("%w: start position", bacnet.ErrMalformed)
	}
	req.StartPosition = int32(els[0].Value.Signed)
	count, err := bacnet.AsUnsigned(els[1].Value)
	if err != nil {
		return fmt.Errorf("%w: recordCount", bacnet.ErrMalformed)
	}
	recs := els[2:]
	if uint64(len(recs)) != count {
		return fmt.Errorf("%w: recordCount mismatch", bacnet.ErrMalformed)
	}
	for _, el := range recs {
		if el.Context || el.Value.Kind != bacnet.ValueOctetString {
			return fmt.Errorf("%w: record data", bacnet.ErrMalformed)
		}
		req.Records = append(req.Records, append([]byte(nil), el.Value.OctetString...))
	}
	return nil
}

// DecodeCreateObject decodes a CreateObject request.
//
// Accepts only the Clause 21 shape: constructed objectSpecifier [0] containing
// CHOICE [0]/[1], optional listOfInitialValues [1]. Rejects the pre-v0.2.3
// bare-CHOICE + [2] list form.
func DecodeCreateObject(payload []byte, limits bacnet.DecodeLimits) (CreateObjectRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return CreateObjectRequest{}, err
	}
	if n != len(payload) {
		return CreateObjectRequest{}, fmt.Errorf("%w: CreateObject trailing", bacnet.ErrTrailingData)
	}
	var req CreateObjectRequest
	var haveSpec bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && bacnet.IsContextConstructed(el):
			if haveSpec {
				return CreateObjectRequest{}, fmt.Errorf("%w: CreateObject duplicate objectSpecifier", bacnet.ErrMalformed)
			}
			inner := el.Value.Elements
			if len(inner) != 1 {
				return CreateObjectRequest{}, fmt.Errorf("%w: CreateObject objectSpecifier CHOICE", bacnet.ErrMalformed)
			}
			choice := inner[0]
			switch {
			case choice.TagNumber == 0 && !bacnet.IsContextConstructed(choice):
				u, e := bacnet.ContextUnsigned(choice)
				err = e
				if err == nil {
					ot := bacnet.ObjectType(u)
					req.ObjectType = &ot
					haveSpec = true
				}
			case choice.TagNumber == 1 && !bacnet.IsContextConstructed(choice):
				id, e := bacnet.ContextObjectID(choice)
				err = e
				if err == nil {
					req.ObjectIdentifier = &id
					haveSpec = true
				}
			default:
				return CreateObjectRequest{}, fmt.Errorf("%w: CreateObject objectSpecifier CHOICE tag %d", bacnet.ErrMalformed, choice.TagNumber)
			}
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el):
			req.InitialValues, err = decodeCOVValues(el.Value.Elements)
		default:
			return CreateObjectRequest{}, fmt.Errorf("%w: CreateObject tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return CreateObjectRequest{}, err
		}
	}
	if !haveSpec {
		return CreateObjectRequest{}, fmt.Errorf("%w: CreateObject objectSpecifier required", bacnet.ErrMalformed)
	}
	return req, nil
}

// DecodeSubscribeCOVPropertyMultiple decodes SubscribeCOVPropertyMultiple.
// The non-standard layout this library emitted up to v0.3.0 (subscription
// list at context [3]) is rejected as malformed.
func DecodeSubscribeCOVPropertyMultiple(payload []byte, limits bacnet.DecodeLimits) (SubscribeCOVPropertyMultipleRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return SubscribeCOVPropertyMultipleRequest{}, err
	}
	if n != len(payload) {
		return SubscribeCOVPropertyMultipleRequest{}, fmt.Errorf("%w: SubscribeCOVPropertyMultiple trailing", bacnet.ErrTrailingData)
	}
	var req SubscribeCOVPropertyMultipleRequest
	var havePID, haveSubs, haveConfirmed bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			req.SubscriberProcessIdentifier, err = bacnet.ContextUnsigned32(el)
			havePID = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			req.IssueConfirmedNotifications, err = bacnet.ContextBool(el)
			haveConfirmed = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			v, e := bacnet.ContextUnsigned32(el)
			err = e
			if err == nil {
				req.LifetimeRemaining = &v
			}
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			v, e := bacnet.ContextUnsigned32(el)
			err = e
			if err == nil {
				req.MaxNotificationDelay = &v
			}
		case el.TagNumber == 4 && bacnet.IsContextConstructed(el):
			if haveSubs {
				return SubscribeCOVPropertyMultipleRequest{}, fmt.Errorf("%w: duplicate subscription list", bacnet.ErrMalformed)
			}
			req.Subscriptions, err = decodeCOVMultipleSubscriptions(el.Value.Elements)
			haveSubs = true
		default:
			return SubscribeCOVPropertyMultipleRequest{}, fmt.Errorf("%w: SubscribeCOVPropertyMultiple tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return SubscribeCOVPropertyMultipleRequest{}, err
		}
	}
	if !havePID || !haveSubs {
		return SubscribeCOVPropertyMultipleRequest{}, fmt.Errorf("%w: SubscribeCOVPropertyMultiple missing fields", bacnet.ErrMalformed)
	}
	req.Cancellation = !haveConfirmed && req.LifetimeRemaining == nil
	return req, nil
}

func decodeCOVMultipleSubscriptions(els []bacnet.Element) ([]COVMultipleSubscription, error) {
	var out []COVMultipleSubscription
	i := 0
	for i < len(els) {
		el := els[i]
		if el.TagNumber != 0 || bacnet.IsContextConstructed(el) {
			return nil, fmt.Errorf("%w: subscription object", bacnet.ErrMalformed)
		}
		obj, err := bacnet.ContextObjectID(el)
		if err != nil {
			return nil, err
		}
		i++
		if i >= len(els) || els[i].TagNumber != 1 || !bacnet.IsContextConstructed(els[i]) {
			return nil, fmt.Errorf("%w: listOfCOVReferences", bacnet.ErrMalformed)
		}
		props, err := decodeCOVReferences(els[i].Value.Elements)
		if err != nil {
			return nil, err
		}
		out = append(out, COVMultipleSubscription{Object: obj, Properties: props})
		i++
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: subscriptions required", bacnet.ErrMalformed)
	}
	return out, nil
}

// decodeCOVReferences decodes the standard list-of-cov-references: each entry
// is monitored-property [0] (constructed BACnetPropertyReference), optional
// cov-increment [1] REAL and optional timestamped [2] BOOLEAN (default FALSE).
func decodeCOVReferences(els []bacnet.Element) ([]COVPropertyReference, error) {
	var out []COVPropertyReference
	var haveInc, haveStamp bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && bacnet.IsContextConstructed(el):
			prop, err := decodePropertyReference(el.Value.Elements)
			if err != nil {
				return nil, err
			}
			out = append(out, COVPropertyReference{Property: prop})
			haveInc, haveStamp = false, false
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if len(out) == 0 || haveInc || haveStamp {
				return nil, fmt.Errorf("%w: misplaced cov-increment", bacnet.ErrMalformed)
			}
			inc, err := bacnet.ContextReal(el)
			if err != nil {
				return nil, err
			}
			out[len(out)-1].COVIncrement = &inc
			haveInc = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			if len(out) == 0 || haveStamp {
				return nil, fmt.Errorf("%w: misplaced timestamped", bacnet.ErrMalformed)
			}
			b, err := bacnet.ContextBool(el)
			if err != nil {
				return nil, err
			}
			out[len(out)-1].Timestamped = b
			haveStamp = true
		default:
			return nil, fmt.Errorf("%w: unexpected COV reference tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: COV property references", bacnet.ErrMalformed)
	}
	return out, nil
}

// DecodeAuditLogQuery decodes an AuditLogQuery request (see
// AuditLogQueryRequest for the layout).
func DecodeAuditLogQuery(payload []byte, limits bacnet.DecodeLimits) (AuditLogQueryRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AuditLogQueryRequest{}, err
	}
	if n != len(payload) {
		return AuditLogQueryRequest{}, fmt.Errorf("%w: AuditLogQuery trailing", bacnet.ErrTrailingData)
	}
	var req AuditLogQueryRequest
	var haveLog, haveQuery, haveCount bool
	for _, el := range els {
		constructed := bacnet.IsContextConstructed(el)
		switch {
		case !el.Context:
			err = fmt.Errorf("%w: AuditLogQuery application-tagged field", bacnet.ErrMalformed)
		case el.TagNumber == 0 && !constructed:
			req.AuditLog, err = bacnet.ContextObjectID(el)
			haveLog = true
		case el.TagNumber == 1 && constructed:
			err = decodeAuditQueryParameters(el.Value.Elements, &req)
			haveQuery = true
		case el.TagNumber == 2 && !constructed:
			var seq uint64
			seq, err = bacnet.ContextUnsigned(el)
			req.StartAtSequenceNumber = &seq
		case el.TagNumber == 3 && !constructed:
			var count uint32
			count, err = bacnet.ContextUnsigned32(el)
			if err == nil && count > 0xFFFF {
				err = fmt.Errorf("%w: AuditLogQuery requested-count", bacnet.ErrMalformed)
			}
			req.RequestedCount = uint16(count)
			haveCount = true
		default:
			err = fmt.Errorf("%w: AuditLogQuery tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return AuditLogQueryRequest{}, err
		}
	}
	if !haveLog || !haveQuery || !haveCount {
		return AuditLogQueryRequest{}, fmt.Errorf("%w: AuditLogQuery missing audit-log, query-parameters or requested-count", bacnet.ErrMalformed)
	}
	return req, nil
}

func decodeAuditQueryParameters(els []bacnet.Element, req *AuditLogQueryRequest) error {
	bad := fmt.Errorf("%w: AuditLogQuery query-parameters", bacnet.ErrMalformed)
	if len(els) != 1 || !bacnet.IsContextConstructed(els[0]) || els[0].TagNumber > 1 {
		return bad
	}
	byTarget := els[0].TagNumber == 0
	var target AuditQueryByTarget
	var source AuditQueryBySource
	var haveDevice, haveFilter bool
	// Tags of the by-target fields; by-source has no property, index or priority.
	const (
		fDevice = iota
		fAddress
		fObject
		fProperty
		fIndex
		fPriority
		fOperations
		fFilter
	)
	for _, el := range els[0].Value.Elements {
		if !el.Context {
			return bad
		}
		field := int(el.TagNumber)
		if !byTarget && field >= 3 {
			field += 3
		}
		constructed := bacnet.IsContextConstructed(el)
		if constructed != (field == fAddress) {
			return bad
		}
		var err error
		switch field {
		case fDevice:
			var id bacnet.ObjectIdentifier
			id, err = bacnet.ContextObjectID(el)
			target.TargetDevice, source.SourceDevice = id, id
			haveDevice = true
		case fAddress:
			var addr AuditAddress
			addr, err = decodeAuditAddress(el.Value.Elements)
			target.TargetAddress, source.SourceAddress = &addr, &addr
		case fObject:
			var id bacnet.ObjectIdentifier
			id, err = bacnet.ContextObjectID(el)
			target.TargetObject, source.SourceObject = &id, &id
		case fProperty:
			var u uint32
			u, err = bacnet.ContextUnsigned32(el)
			prop := bacnet.PropertyIdentifier(u)
			target.TargetProperty = &prop
		case fIndex:
			var u uint32
			u, err = bacnet.ContextUnsigned32(el)
			target.TargetArrayIndex = &u
		case fPriority:
			var u uint32
			u, err = bacnet.ContextUnsigned32(el)
			if err == nil && u > 0xFF {
				err = bad
			}
			p := uint8(u)
			target.TargetPriority = &p
		case fOperations:
			var bs bacnet.BitString
			bs, err = bacnet.ContextBitString(el)
			bs.Bytes = append([]byte{}, bs.Bytes...)
			target.Operations, source.Operations = &bs, &bs
		case fFilter:
			var u uint32
			u, err = bacnet.ContextUnsigned32(el)
			target.SuccessFilter, source.SuccessFilter = AuditSuccessFilter(u), AuditSuccessFilter(u)
			haveFilter = true
		default:
			return bad
		}
		if err != nil {
			return err
		}
	}
	if !haveDevice || !haveFilter {
		return bad
	}
	if byTarget {
		req.ByTarget = &target
	} else {
		req.BySource = &source
	}
	return nil
}

// DecodeVTOpen decodes a VT-Open request.
func DecodeVTOpen(payload []byte, limits bacnet.DecodeLimits) (VTOpenRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return VTOpenRequest{}, err
	}
	if n != len(payload) {
		return VTOpenRequest{}, fmt.Errorf("%w: VTOpen trailing", bacnet.ErrTrailingData)
	}
	if len(els) != 2 {
		return VTOpenRequest{}, fmt.Errorf("%w: VTOpen length", bacnet.ErrMalformed)
	}
	cls, err := bacnet.AsEnumerated(els[0].Value)
	if err != nil {
		return VTOpenRequest{}, err
	}
	sid, err := bacnet.AsUnsigned(els[1].Value)
	if err != nil {
		return VTOpenRequest{}, err
	}
	return VTOpenRequest{VTClass: uint32(cls), LocalVTSessionIdentifier: uint8(sid)}, nil
}
