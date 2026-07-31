// SPDX-License-Identifier: MIT

package apdu_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

func TestUnconfirmedRoundTrip(t *testing.T) {
	raw := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceWhoIs,
		Payload:       []byte{0x09, 0x01},
	})
	pdu, err := apdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil || pdu.UnconfirmedRequest == nil {
		t.Fatalf("%v %#v", err, pdu)
	}
	if pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceWhoIs ||
		!bytes.Equal(pdu.UnconfirmedRequest.Payload, []byte{0x09, 0x01}) {
		t.Fatalf("%#v", pdu.UnconfirmedRequest)
	}
}

func TestSimpleACKRoundTrip(t *testing.T) {
	raw := apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: 9, ServiceChoice: apdu.ServiceWriteProperty})
	pdu, err := apdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil || pdu.SimpleACK == nil || pdu.SimpleACK.InvokeID != 9 {
		t.Fatalf("%v %#v", err, pdu)
	}
}

func TestSegmentACKRoundTrip(t *testing.T) {
	raw := apdu.AppendSegmentACK(nil, apdu.SegmentACK{
		NegativeACK: true, Server: true, InvokeID: 3, SequenceNumber: 4, ActualWindowSize: 5,
	})
	pdu, err := apdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil || pdu.SegmentACK == nil {
		t.Fatalf("%v %#v", err, pdu)
	}
	s := pdu.SegmentACK
	if !s.NegativeACK || !s.Server || s.InvokeID != 3 || s.SequenceNumber != 4 || s.ActualWindowSize != 5 {
		t.Fatalf("%#v", s)
	}
}

func TestErrorRejectAbortRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()

	errRaw := apdu.AppendError(nil, apdu.ErrorPDU{
		InvokeID: 2, ServiceChoice: apdu.ServiceReadProperty, Payload: []byte{0x91, 0x02, 0x91, 0x00},
	})
	pdu, err := apdu.Parse(errRaw, limits)
	if err != nil || pdu.Error == nil || pdu.Error.InvokeID != 2 {
		t.Fatalf("%v %#v", err, pdu)
	}
	class, code, err := apdu.DecodeErrorClassCode(pdu.Error.Payload, limits)
	if err != nil || class != 2 || code != 0 {
		t.Fatalf("class/code %d/%d err=%v", class, code, err)
	}

	rej := apdu.AppendReject(nil, apdu.RejectPDU{InvokeID: 4, Reason: 1})
	pdu, err = apdu.Parse(rej, limits)
	if err != nil || pdu.Reject == nil || pdu.Reject.Reason != 1 {
		t.Fatalf("%v %#v", err, pdu)
	}

	ab := apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: 5, Reason: 2})
	pdu, err = apdu.Parse(ab, limits)
	if err != nil || pdu.Abort == nil || !pdu.Abort.Server || pdu.Abort.Reason != 2 {
		t.Fatalf("%v %#v", err, pdu)
	}
}

func TestSegmentedConfirmedAndComplexACK(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	req := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		SegmentedMessage: true, MoreFollows: true, SegmentedResponseAccepted: true,
		MaxSegments: 2, MaxAPDU: 5, InvokeID: 1,
		SequenceNumber: 3, ProposedWindowSize: 4,
		ServiceChoice: apdu.ServiceReadPropertyMultiple, Payload: []byte{0xAA},
	})
	pdu, err := apdu.Parse(req, limits)
	if err != nil || pdu.ConfirmedRequest == nil {
		t.Fatalf("%v %#v", err, pdu)
	}
	cr := pdu.ConfirmedRequest
	if !cr.SegmentedMessage || !cr.MoreFollows || cr.SequenceNumber != 3 || cr.ProposedWindowSize != 4 {
		t.Fatalf("%#v", cr)
	}

	ack := apdu.AppendComplexACK(nil, apdu.ComplexACK{
		SegmentedMessage: true, MoreFollows: false, InvokeID: 1,
		SequenceNumber: 0, ProposedWindowSize: 2,
		ServiceChoice: apdu.ServiceReadPropertyMultiple, Payload: []byte{0xBB},
	})
	pdu, err = apdu.Parse(ack, limits)
	if err != nil || pdu.ComplexACK == nil || !pdu.ComplexACK.SegmentedMessage {
		t.Fatalf("%v %#v", err, pdu)
	}
}

func TestParseMalformedAndUnsupported(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	cases := []struct {
		name string
		raw  []byte
		want error
	}{
		{"empty", nil, bacnet.ErrMalformed},
		{"confirmed short", []byte{0x00, 0x05}, bacnet.ErrMalformed},
		{"segmented confirmed short", []byte{0x08, 0x05, 0x01, 0x00}, bacnet.ErrMalformed},
		{"unconfirmed short", []byte{0x10}, bacnet.ErrMalformed},
		{"simple ack len", []byte{0x20, 0x01}, bacnet.ErrMalformed},
		{"complex short", []byte{0x30}, bacnet.ErrMalformed},
		{"segment ack len", []byte{0x40, 0x01, 0x02}, bacnet.ErrMalformed},
		{"error short", []byte{0x50, 0x01}, bacnet.ErrMalformed},
		{"reject len", []byte{0x60, 0x01}, bacnet.ErrMalformed},
		{"abort len", []byte{0x70, 0x01}, bacnet.ErrMalformed},
		{"unsupported type", []byte{0x80, 0x00}, bacnet.ErrUnsupported},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := apdu.Parse(tc.raw, limits)
			if !errors.Is(err, tc.want) {
				t.Fatalf("err=%v want %v", err, tc.want)
			}
		})
	}

	tiny := bacnet.DecodeLimits{MaxAPDUSize: 2}
	if _, err := apdu.Parse([]byte{0x10, 0x08, 0x00}, tiny); !errors.Is(err, bacnet.ErrLimitExceeded) {
		t.Fatalf("limit err=%v", err)
	}
}
