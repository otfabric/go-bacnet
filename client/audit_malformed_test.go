// SPDX-License-Identifier: MIT

package client

import (
	"testing"
	"time"

	"github.com/otfabric/go-bacnet/apdu"
)

func TestMalformedAuditNotifications(t *testing.T) {
	env := newVirtualPair(t)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: 8, ServiceChoice: apdu.ServiceConfirmedAuditNotification, MaxAPDU: 5, Payload: []byte{0xff},
	}))
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedAuditNotification, Payload: []byte{0xff},
	}))
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceUnconfirmedCOVNotificationMultiple, Payload: []byte{0xff},
	}))
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
		InvokeID: 9, ServiceChoice: apdu.ServiceConfirmedCOVNotificationMultiple, MaxAPDU: 5, Payload: []byte{0xff},
	}))
	time.Sleep(50 * time.Millisecond)
}
