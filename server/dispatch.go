// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"sort"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// result is the outcome of a confirmed service.
type result struct {
	silent  bool   // no answer at all
	payload []byte // ComplexACK service-ack; nil with simple
	simple  bool
	err     error
	// after runs once the reply was sent (an initial COV notification must
	// follow the SimpleACK of its subscription).
	after func()
}

func ack() result                { return result{simple: true} }
func complexACK(p []byte) result { return result{payload: p} }
func fail(err error) result      { return result{err: err} }

type rejectError struct{ reason uint8 }

func (e *rejectError) Error() string { return bacnet.RejectReasonName(e.reason) }

type abortError struct{ reason uint8 }

func (e *abortError) Error() string { return bacnet.AbortReasonName(e.reason) }

// createObjectError is a CreateObject-Error: errorType [0] and
// firstFailedElementNumber [1] (clause 15.3).
type createObjectError struct {
	class, code uint16
	first       uint32
}

func (e *createObjectError) Error() string { return "create object failed" }

// rejectFor maps a request decode failure to a Reject.
func rejectFor(err error) result {
	switch {
	case errors.Is(err, bacnet.ErrLimitExceeded):
		return fail(&rejectError{bacnet.RejectReasonBufferOverflow})
	case errors.Is(err, bacnet.ErrTrailingData):
		return fail(&rejectError{bacnet.RejectReasonTooManyArguments})
	case errors.Is(err, service.ErrUndefinedEnumeration):
		return fail(&rejectError{bacnet.RejectReasonUndefinedEnumeration})
	default:
		return fail(&rejectError{bacnet.RejectReasonInvalidTag})
	}
}

// encode returns the reply APDU, and for a ComplexACK its payload.
func (r result) encode(invokeID, svc uint8) (reply, payload []byte, complex bool) {
	switch {
	case r.err != nil:
		var rj *rejectError
		var ab *abortError
		var wpm *service.WritePropertyMultipleError
		var co *createObjectError
		var cm *covMultipleError
		var body []byte
		var err error
		switch {
		case errors.As(r.err, &rj):
			return apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: invokeID, Reason: rj.reason}), nil, false
		case errors.As(r.err, &ab):
			return apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: invokeID, Reason: ab.reason}), nil, false
		case errors.As(r.err, &wpm):
			body, err = service.EncodeWritePropertyMultipleError(*wpm)
		case errors.As(r.err, &cm):
			body, err = cm.encode()
		case errors.As(r.err, &co):
			body = []byte{0x0E}
			body, err = bacnet.EncodeBACnetError(body, co.class, co.code)
			if err == nil {
				body = append(body, 0x0F)
				body, err = bacnet.AppendContextUnsigned(body, 1, uint64(co.first))
			}
		default:
			e := object.AsError(r.err)
			body, err = bacnet.EncodeBACnetError(nil, e.Class, e.Code)
		}
		if err != nil {
			body, _ = bacnet.EncodeBACnetError(nil, bacnet.ErrorClassDevice, bacnet.ErrorCodeOperationalProblem)
		}
		return apdu.AppendError(nil, apdu.ErrorPDU{InvokeID: invokeID, ServiceChoice: svc, Payload: body}), nil, false
	case r.simple:
		return apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: invokeID, ServiceChoice: svc}), nil, false
	default:
		return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: invokeID, ServiceChoice: svc, Payload: r.payload}), r.payload, true
	}
}

type confirmedHandler func(s *Server, ctx context.Context, req *apdu.ConfirmedRequest, p peer) result

type unconfirmedHandler func(s *Server, ctx context.Context, req *apdu.UnconfirmedRequest, p peer)

// Bit positions of BACnetServicesSupported (clause 21) for each served
// service.
var confirmedServices = map[uint8]struct {
	bit int
	h   confirmedHandler
}{
	apdu.ServiceReadProperty:                 {12, (*Server).readProperty},
	apdu.ServiceReadPropertyMultiple:         {14, (*Server).readPropertyMultiple},
	apdu.ServiceWriteProperty:                {15, (*Server).writeProperty},
	apdu.ServiceWritePropertyMultiple:        {16, (*Server).writePropertyMultiple},
	apdu.ServiceDeviceCommunicationControl:   {17, (*Server).deviceCommunicationControl},
	apdu.ServiceConfirmedPrivateTransfer:     {18, (*Server).confirmedPrivateTransfer},
	apdu.ServiceConfirmedTextMessage:         {19, (*Server).confirmedTextMessage},
	apdu.ServiceReinitializeDevice:           {20, (*Server).reinitializeDevice},
	apdu.ServiceReadRange:                    {35, (*Server).readRange},
	apdu.ServiceSubscribeCOV:                 {5, (*Server).subscribeCOV},
	apdu.ServiceSubscribeCOVProperty:         {38, (*Server).subscribeCOVProperty},
	apdu.ServiceSubscribeCOVPropertyMultiple: {41, (*Server).subscribeCOVPropertyMultiple},
	apdu.ServiceAtomicReadFile:               {6, (*Server).atomicReadFile},
	apdu.ServiceAtomicWriteFile:              {7, (*Server).atomicWriteFile},
	apdu.ServiceAddListElement:               {8, (*Server).addListElement},
	apdu.ServiceRemoveListElement:            {9, (*Server).removeListElement},
	apdu.ServiceCreateObject:                 {10, (*Server).createObject},
	apdu.ServiceDeleteObject:                 {11, (*Server).deleteObject},
	apdu.ServiceAcknowledgeAlarm:             {0, (*Server).acknowledgeAlarm},
	apdu.ServiceGetAlarmSummary:              {3, (*Server).getAlarmSummary},
	apdu.ServiceGetEnrollmentSummary:         {4, (*Server).getEnrollmentSummary},
	apdu.ServiceGetEventInformation:          {39, (*Server).getEventInformation},
}

var unconfirmedServices = map[uint8]struct {
	bit int
	h   unconfirmedHandler
}{
	apdu.ServiceWhoIs:                      {34, (*Server).whoIs},
	apdu.ServiceWhoHas:                     {33, (*Server).whoHas},
	apdu.ServiceTimeSynchronization:        {32, (*Server).timeSynchronization},
	apdu.ServiceUTCTimeSynchronization:     {36, (*Server).utcTimeSynchronization},
	apdu.ServiceUnconfirmedPrivateTransfer: {30, (*Server).unconfirmedPrivateTransfer},
	apdu.ServiceUnconfirmedTextMessage:     {31, (*Server).unconfirmedTextMessage},
	apdu.ServiceWriteGroup:                 {40, (*Server).writeGroup},
	// I-Am is executed to bind event recipients; bit 26 is also initiated.
	apdu.ServiceIAm: {26, (*Server).iAm},
}

// Services the server initiates: I-Am (26), I-Have (27), COV notifications
// (1, 28, 42, 43) and event notifications (2, 29).
var initiatedServiceBits = []int{1, 2, 26, 27, 28, 29, 42, 43}

// serviceEnabled reports whether a service is served. The services that
// act only through an application hook are served when it is set. Without it they are neither advertised in
// Protocol_Services_Supported nor executed: a confirmed request gets
// Reject unrecognized-service, as bacnet-stack answers a service it has no
// handler for.
func (s *Server) serviceEnabled(confirmed bool, choice uint8) bool {
	h := s.cfg.hooks
	if confirmed {
		switch choice {
		case apdu.ServiceConfirmedPrivateTransfer:
			return h.PrivateTransfer != nil
		case apdu.ServiceConfirmedTextMessage:
			return h.TextMessage != nil
		}
		return true
	}
	switch choice {
	case apdu.ServiceUnconfirmedPrivateTransfer:
		return h.PrivateTransfer != nil
	case apdu.ServiceUnconfirmedTextMessage:
		return h.TextMessage != nil
	case apdu.ServiceTimeSynchronization, apdu.ServiceUTCTimeSynchronization:
		return h.TimeSync != nil
	case apdu.ServiceWriteGroup:
		return h.WriteGroup != nil
	}
	return true
}

func (s *Server) servicesSupported() []int {
	var bits []int
	for choice, v := range confirmedServices {
		if s.serviceEnabled(true, choice) {
			bits = append(bits, v.bit)
		}
	}
	for choice, v := range unconfirmedServices {
		if s.serviceEnabled(false, choice) && v.bit != 26 {
			bits = append(bits, v.bit)
		}
	}
	bits = append(bits, initiatedServiceBits...)
	sort.Ints(bits)
	return bits
}

// dispatch runs on the receive loop.
func (s *Server) dispatch(pdu apdu.PDU, p peer) {
	switch pdu.Type {
	case apdu.TypeConfirmedRequest:
		req := pdu.ConfirmedRequest
		if s.dcc.respondsTo(s.clock.Now(), req.ServiceChoice) {
			s.resp.confirmed(req, p)
		}
	case apdu.TypeUnconfirmedRequest:
		req := pdu.UnconfirmedRequest
		if !s.dcc.respondsTo(s.clock.Now(), 0xFF) {
			return
		}
		h, ok := unconfirmedServices[req.ServiceChoice]
		if !ok || !s.serviceEnabled(false, req.ServiceChoice) {
			return
		}
		r := *req
		r.Payload = append([]byte(nil), req.Payload...)
		if !s.submit(func(ctx context.Context) { h.h(s, ctx, &r, p) }) {
			s.report(diag.KindQueueDrop, "unconfirmed request dropped: workers busy", map[string]any{"service": req.ServiceChoice})
		}
	case apdu.TypeSegmentACK:
		s.resp.deliverSegmentACK(pdu.SegmentACK, p)
	case apdu.TypeAbort:
		if pdu.Abort.Server {
			s.req.response(pdu, p)
		} else {
			s.resp.abortFromClient(pdu.Abort.InvokeID, p)
		}
	case apdu.TypeSimpleACK, apdu.TypeComplexACK, apdu.TypeError, apdu.TypeReject:
		s.req.response(pdu, p)
	}
}

// handleConfirmed runs on a worker.
func (s *Server) handleConfirmed(ctx context.Context, req *apdu.ConfirmedRequest, p peer) result {
	h, ok := confirmedServices[req.ServiceChoice]
	if !ok || !s.serviceEnabled(true, req.ServiceChoice) {
		return fail(&rejectError{bacnet.RejectReasonUnrecognizedService})
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.apduTimeout)
	defer cancel()
	return h.h(s, ctx, req, p)
}

// runtimeInfo exposes the server's protocol settings to the Device object.
type runtimeInfo struct{ s *Server }

func (r runtimeInfo) ServicesSupported() []int { return r.s.servicesSupported() }

func (r runtimeInfo) MaxAPDULengthAccepted() uint16 { return r.s.cfg.maxAPDU }

func (r runtimeInfo) Segmentation() uint32 {
	if r.s.cfg.segmentation {
		return 0 // segmented-both
	}
	return 3 // no-segmentation
}

func (r runtimeInfo) MaxSegmentsAccepted() uint32 {
	if !r.s.cfg.segmentation {
		return 1
	}
	return uint32(r.s.cfg.maxSegments)
}

func (r runtimeInfo) APDUTimeout() time.Duration        { return r.s.cfg.apduTimeout }
func (r runtimeInfo) APDUSegmentTimeout() time.Duration { return r.s.cfg.segmentTimeout }
func (r runtimeInfo) APDURetries() int                  { return r.s.cfg.retries }
func (r runtimeInfo) Now() time.Time                    { return r.s.clock.Now() }

func (r runtimeInfo) ActiveCOVSubscriptions() []bacnet.Element { return r.s.cov.activeSubscriptions() }
