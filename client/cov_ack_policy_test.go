// SPDX-License-Identifier: MIT

package client

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestConfirmedCOVAcksUnknownProcessID(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	note := service.COVNotification{
		ProcessIdentifier: 99999, // no local subscription
		InitiatingDevice:  bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1},
		MonitoredObject:   obj,
		TimeRemaining:     10,
		Values: []service.PropertyValue{{
			Property: prop,
			Value:    bacnet.RealValue(1.0),
		}},
	}
	payload := encodeCOVNotification(t, note)
	apduBytes := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: 11, ServiceChoice: 1, MaxAPDU: 5, Payload: payload,
	})
	before := len(env.ClientTr.Outbox())
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apduBytes)

	limits := bacnet.DefaultDecodeLimits()
	deadline := time.Now().Add(time.Second)
	acked := false
	for time.Now().Before(deadline) && !acked {
		for _, pkt := range env.ClientTr.Outbox()[before:] {
			msg, err := bvlc.Parse(pkt.Data, limits)
			if err != nil {
				continue
			}
			n, _, err := npdu.Parse(msg.Payload, limits)
			if err != nil || len(n.APDU) == 0 {
				continue
			}
			pdu, err := apdu.Parse(n.APDU, limits)
			if err == nil && pdu.SimpleACK != nil && pdu.SimpleACK.InvokeID == 11 {
				acked = true
				break
			}
		}
		if !acked {
			time.Sleep(5 * time.Millisecond)
		}
	}
	if !acked {
		t.Fatal("confirmed COV with unknown process ID should still be SimpleACK'd")
	}
}
