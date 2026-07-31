// SPDX-License-Identifier: MIT

package apdu_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

func TestConfirmedAndComplexACK(t *testing.T) {
	req := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		SegmentedResponseAccepted: true,
		MaxAPDU:                   3,
		InvokeID:                  7,
		ServiceChoice:             apdu.ServiceReadProperty,
		Payload:                   []byte{0x0C, 0x00},
	})
	pdu, err := apdu.Parse(req, bacnet.DefaultDecodeLimits())
	if err != nil || pdu.ConfirmedRequest == nil || pdu.ConfirmedRequest.InvokeID != 7 {
		t.Fatalf("%v %#v", err, pdu)
	}
	ack := apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID:      7,
		ServiceChoice: apdu.ServiceReadProperty,
		Payload:       []byte{0x01},
	})
	pdu, err = apdu.Parse(ack, bacnet.DefaultDecodeLimits())
	if err != nil || pdu.ComplexACK == nil {
		t.Fatal(err)
	}
}

func TestRejectAbortError(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	r := apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: 1, ServiceChoice: 15})
	// reject
	raw := []byte{0x60, 0x02, 0x09}
	pdu, err := apdu.Parse(raw, limits)
	if err != nil || pdu.Reject.Reason != 9 {
		t.Fatalf("%v %#v", err, pdu)
	}
	_ = r
	raw = []byte{0x71, 0x03, 0x05} // abort server
	pdu, err = apdu.Parse(raw, limits)
	if err != nil || !pdu.Abort.Server || pdu.Abort.Reason != 5 {
		t.Fatalf("%v %#v", err, pdu)
	}
}
