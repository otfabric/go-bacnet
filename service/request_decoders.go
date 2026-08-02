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
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				v := uint32(u)
				req.EventStateFilter = &v
			}
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				v := uint32(u)
				req.EventTypeFilter = &v
			}
		case el.TagNumber == 4 && bacnet.IsContextConstructed(el):
			if len(el.Value.Elements) == 2 {
				a, e1 := bacnet.AsUnsigned(el.Value.Elements[0].Value)
				b, e2 := bacnet.AsUnsigned(el.Value.Elements[1].Value)
				if e1 == nil && e2 == nil {
					pri := [2]uint32{uint32(a), uint32(b)}
					req.PriorityFilter = &pri
				} else if e1 != nil {
					err = e1
				} else {
					err = e2
				}
			}
		case el.TagNumber == 5 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				v := uint32(u)
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
func DecodeSubscribeCOVPropertyMultiple(payload []byte, limits bacnet.DecodeLimits) (SubscribeCOVPropertyMultipleRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return SubscribeCOVPropertyMultipleRequest{}, err
	}
	if n != len(payload) {
		return SubscribeCOVPropertyMultipleRequest{}, fmt.Errorf("%w: SubscribeCOVPropertyMultiple trailing", bacnet.ErrTrailingData)
	}
	var req SubscribeCOVPropertyMultipleRequest
	var havePID, haveIssue, haveSubs bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				req.SubscriberProcessIdentifier = uint32(u)
				havePID = true
			}
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			req.IssueConfirmedNotifications, err = bacnet.ContextBool(el)
			haveIssue = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				v := uint32(u)
				req.LifetimeRemaining = &v
			}
		case el.TagNumber == 3 && bacnet.IsContextConstructed(el):
			req.Subscriptions, err = decodeCOVMultipleSubscriptions(el.Value.Elements)
			haveSubs = true
		default:
			return SubscribeCOVPropertyMultipleRequest{}, fmt.Errorf("%w: SubscribeCOVPropertyMultiple tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return SubscribeCOVPropertyMultipleRequest{}, err
		}
	}
	if !havePID || !haveIssue || !haveSubs {
		return SubscribeCOVPropertyMultipleRequest{}, fmt.Errorf("%w: SubscribeCOVPropertyMultiple missing fields", bacnet.ErrMalformed)
	}
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
		props, err := decodeCOVPropertyReferences(els[i].Value.Elements)
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

func decodeCOVPropertyReferences(els []bacnet.Element) ([]COVPropertyReference, error) {
	var out []COVPropertyReference
	var cur COVPropertyReference
	var haveProp bool
	flush := func() {
		if haveProp {
			out = append(out, cur)
			cur = COVPropertyReference{}
			haveProp = false
		}
	}
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			flush()
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return nil, err
			}
			cur.Property.Identifier = bacnet.PropertyIdentifier(u)
			haveProp = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			u, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return nil, err
			}
			idx := uint32(u)
			cur.Property.ArrayIndex = &idx
		case el.TagNumber == 2 && bacnet.IsContextConstructed(el):
			if len(el.Value.Elements) == 1 {
				if r, err := bacnet.AsReal(el.Value.Elements[0].Value); err == nil {
					inc := float32(r)
					cur.COVIncrement = &inc
				}
			}
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			b, err := bacnet.ContextBool(el)
			if err != nil {
				return nil, err
			}
			cur.Timestamped = b
		}
	}
	flush()
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: COV property references", bacnet.ErrMalformed)
	}
	return out, nil
}

// DecodeAuditLogQuery decodes an AuditLogQuery request.
func DecodeAuditLogQuery(payload []byte, limits bacnet.DecodeLimits) (AuditLogQueryRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AuditLogQueryRequest{}, err
	}
	if n != len(payload) {
		return AuditLogQueryRequest{}, fmt.Errorf("%w: AuditLogQuery trailing", bacnet.ErrTrailingData)
	}
	var req AuditLogQueryRequest
	var haveLog bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			req.AuditLog, err = bacnet.ContextObjectID(el)
			haveLog = true
		case el.TagNumber == 1 && bacnet.IsContextConstructed(el):
			req.Query = cloneElements(el.Value.Elements)
		default:
			return AuditLogQueryRequest{}, fmt.Errorf("%w: AuditLogQuery tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return AuditLogQueryRequest{}, err
		}
	}
	if !haveLog {
		return AuditLogQueryRequest{}, fmt.Errorf("%w: AuditLogQuery missing audit log", bacnet.ErrMalformed)
	}
	return req, nil
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
