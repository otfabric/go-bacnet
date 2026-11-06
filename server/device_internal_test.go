// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

func dcc(t *testing.T, mode service.EnableDisable, minutes *uint16, password string) []byte {
	req := service.DeviceCommunicationControlRequest{EnableDisable: mode, TimeDuration: minutes}
	if password != "" {
		req.Password = &bacnet.CharacterString{Value: password}
	}
	return mustEncode(t)(service.EncodeDeviceCommunicationControl(req))
}

func TestDeviceCommunicationControl(t *testing.T) {
	h := newHarness(t, WithPassword("secret"))
	// Wrong and missing passwords.
	h.confirmed(1, apdu.ServiceDeviceCommunicationControl, dcc(t, service.EnableDisableDisableInitiation, nil, "wrong"))
	h.expectError(bacnet.ErrorClassSecurity, bacnet.ErrorCodePasswordFailure)
	h.confirmed(2, apdu.ServiceDeviceCommunicationControl, dcc(t, service.EnableDisableDisableInitiation, nil, ""))
	h.expectError(bacnet.ErrorClassSecurity, bacnet.ErrorCodePasswordFailure)
	// DISABLE is refused at protocol revision 31.
	h.confirmed(3, apdu.ServiceDeviceCommunicationControl, dcc(t, service.EnableDisableDisable, nil, "secret"))
	h.expectError(bacnet.ErrorClassServices, bacnet.ErrorCodeServiceRequestDenied)
	// An undefined enumeration is rejected.
	bad := []byte{0x19, 0x07} // [1] enable-disable 7
	h.confirmed(4, apdu.ServiceDeviceCommunicationControl, bad)
	h.expectReject(bacnet.RejectReasonUndefinedEnumeration)
	h.confirmed(5, apdu.ServiceDeviceCommunicationControl, []byte{0xFF})
	h.expectType(apdu.TypeReject)

	// disable-initiation: the server still answers, but sends no I-Am.
	h.confirmed(6, apdu.ServiceDeviceCommunicationControl, dcc(t, service.EnableDisableDisableInitiation, nil, "secret"))
	h.expectType(apdu.TypeSimpleACK)
	if err := h.s.IAm(); err != nil {
		t.Fatal(err)
	}
	h.quiet(30 * time.Millisecond)
	// Who-Is is still answered (clause 16.1).
	h.unconfirmed(apdu.ServiceWhoIs, nil)
	if got := h.expectType(apdu.TypeUnconfirmedRequest); got.pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceIAm {
		t.Fatal("no I-Am")
	}
	h.confirmed(7, apdu.ServiceDeviceCommunicationControl, dcc(t, service.EnableDisableEnable, nil, "secret"))
	h.expectType(apdu.TypeSimpleACK)
	if err := h.s.IAm(); err != nil {
		t.Fatal(err)
	}
	if got := h.next(); !got.pkt.Broadcast {
		t.Fatal("I-Am not broadcast")
	}
}

func TestDCCDisableAndDuration(t *testing.T) {
	// While communication is disabled only DCC and ReinitializeDevice are
	// answered (clause 16.1).
	h := newHarness(t)
	h.s.dcc.set(service.EnableDisableDisable, time.Now().Add(time.Hour))
	h.confirmed(1, apdu.ServiceReadProperty, readPV(t))
	h.unconfirmed(apdu.ServiceWhoIs, nil)
	h.quiet(30 * time.Millisecond)
	h.confirmed(2, apdu.ServiceReinitializeDevice, mustEncode(t)(service.EncodeReinitializeDevice(service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart})))
	h.expectType(apdu.TypeSimpleACK)
	// The duration ends the state.
	h.s.dcc.set(service.EnableDisableDisable, time.Now().Add(-time.Second))
	h.s.dcc.expire(time.Now())
	h.confirmed(3, apdu.ServiceReadProperty, readPV(t))
	h.expectType(apdu.TypeComplexACK)
	// A DCC with a duration sets the end time.
	minutes := uint16(5)
	h.confirmed(4, apdu.ServiceDeviceCommunicationControl, dcc(t, service.EnableDisableDisableInitiation, &minutes, ""))
	h.expectType(apdu.TypeSimpleACK)
	h.s.dcc.mu.Lock()
	until := h.s.dcc.until
	h.s.dcc.mu.Unlock()
	if d := time.Until(until); d < 4*time.Minute || d > 6*time.Minute {
		t.Fatalf("until %v", until)
	}
}

func TestReinitializeDevice(t *testing.T) {
	var got atomic.Int32
	h := newHarness(t, WithPassword("pw"), WithHooks(Hooks{Reinitialize: func(_ context.Context, st service.ReinitializedStateOfDevice) error {
		got.Store(int32(st) + 1)
		if st == service.ReinitializedStartBackup {
			return object.Err(bacnet.ErrorClassDevice, bacnet.ErrorCodeConfigurationInProgress)
		}
		return nil
	}}))
	pw := &bacnet.CharacterString{Value: "pw"}
	h.confirmed(1, apdu.ServiceReinitializeDevice, mustEncode(t)(service.EncodeReinitializeDevice(service.ReinitializeDeviceRequest{State: service.ReinitializedActivateChanges, Password: pw})))
	h.expectType(apdu.TypeSimpleACK)
	if got.Load() != 8 {
		t.Fatal("hook not called with activate-changes")
	}
	h.confirmed(2, apdu.ServiceReinitializeDevice, mustEncode(t)(service.EncodeReinitializeDevice(service.ReinitializeDeviceRequest{State: service.ReinitializedStartBackup, Password: pw})))
	h.expectError(bacnet.ErrorClassDevice, bacnet.ErrorCodeConfigurationInProgress)
	h.confirmed(3, apdu.ServiceReinitializeDevice, mustEncode(t)(service.EncodeReinitializeDevice(service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart})))
	h.expectError(bacnet.ErrorClassSecurity, bacnet.ErrorCodePasswordFailure)
	h.confirmed(4, apdu.ServiceReinitializeDevice, []byte{0x09, 0x09})
	h.expectReject(bacnet.RejectReasonUndefinedEnumeration)
	h.confirmed(5, apdu.ServiceReinitializeDevice, []byte{0xFF})
	h.expectType(apdu.TypeReject)
}

func TestMessagingHooks(t *testing.T) {
	var syncs, groups, texts, transfers atomic.Int32
	h := newHarness(t, WithHooks(Hooks{
		TimeSync: func(_ service.TimeSynchronization, utc bool) {
			if utc {
				syncs.Add(10)
			} else {
				syncs.Add(1)
			}
		},
		WriteGroup: func(context.Context, service.WriteGroup) { groups.Add(1) },
		TextMessage: func(_ context.Context, m service.TextMessage, confirmed bool) error {
			texts.Add(1)
			if m.Message == "no" {
				return errors.New("refused")
			}
			return nil
		},
		PrivateTransfer: func(_ context.Context, p service.PrivateTransfer, confirmed bool) ([]bacnet.Element, error) {
			transfers.Add(1)
			if p.ServiceNumber == 9 {
				return nil, object.Err(bacnet.ErrorClassServices, bacnet.ErrorCodeOptionalFunctionalityNotSupported)
			}
			return []bacnet.Element{{Value: bacnet.UnsignedValue(7)}}, nil
		},
	}))
	ts := mustEncode(t)(service.EncodeTimeSynchronization(service.TimeSynchronization{Date: bacnet.Date{Year: 126, Month: 1, Day: 1, Weekday: 4}, Time: bacnet.Time{Hour: 1}}))
	h.unconfirmed(apdu.ServiceTimeSynchronization, ts)
	h.unconfirmed(apdu.ServiceUTCTimeSynchronization, ts)
	h.unconfirmed(apdu.ServiceTimeSynchronization, []byte{0xFF})
	wg := mustEncode(t)(service.EncodeWriteGroup(service.WriteGroup{GroupNumber: 1, WritePriority: 8, ChangeList: []bacnet.Element{{Context: true, TagNumber: 0, Value: bacnet.UnsignedValue(1)}}}))
	h.unconfirmed(apdu.ServiceWriteGroup, wg)
	h.unconfirmed(apdu.ServiceWriteGroup, []byte{0xFF})
	msg := func(text string) []byte {
		return mustEncode(t)(service.EncodeTextMessage(service.TextMessage{TextMessageSourceDevice: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}, Message: text}))
	}
	h.unconfirmed(apdu.ServiceUnconfirmedTextMessage, msg("hi"))
	h.unconfirmed(apdu.ServiceUnconfirmedTextMessage, []byte{0xFF})
	pt := func(n uint32) []byte {
		return mustEncode(t)(service.EncodePrivateTransfer(service.PrivateTransfer{VendorID: 999, ServiceNumber: n}))
	}
	h.unconfirmed(apdu.ServiceUnconfirmedPrivateTransfer, pt(1))
	h.unconfirmed(apdu.ServiceUnconfirmedPrivateTransfer, []byte{0xFF})

	h.confirmed(1, apdu.ServiceConfirmedTextMessage, msg("hello"))
	h.expectType(apdu.TypeSimpleACK)
	h.confirmed(2, apdu.ServiceConfirmedTextMessage, msg("no"))
	h.expectError(bacnet.ErrorClassDevice, bacnet.ErrorCodeOperationalProblem)
	h.confirmed(3, apdu.ServiceConfirmedTextMessage, []byte{0xFF})
	h.expectType(apdu.TypeReject)
	h.confirmed(4, apdu.ServiceConfirmedPrivateTransfer, pt(1))
	ack := h.expectType(apdu.TypeComplexACK)
	if len(ack.pdu.ComplexACK.Payload) < 6 {
		t.Fatalf("private transfer ack %x", ack.pdu.ComplexACK.Payload)
	}
	h.confirmed(5, apdu.ServiceConfirmedPrivateTransfer, pt(9))
	h.expectError(bacnet.ErrorClassServices, bacnet.ErrorCodeOptionalFunctionalityNotSupported)
	h.confirmed(6, apdu.ServiceConfirmedPrivateTransfer, []byte{0xFF})
	h.expectType(apdu.TypeReject)

	waitUntil(t, func() bool {
		return syncs.Load() == 11 && groups.Load() == 1 && texts.Load() == 3 && transfers.Load() == 3
	})
	bits := h.s.servicesSupported()
	for _, want := range []int{18, 19, 30, 31, 32, 36, 40} {
		if !containsInt(bits, want) {
			t.Errorf("service bit %d not advertised with its hook", want)
		}
	}
}

func TestHookServicesNotAdvertised(t *testing.T) {
	h := newHarness(t)
	bits := h.s.servicesSupported()
	for _, bit := range []int{18, 19, 30, 31, 32, 36, 40} {
		if containsInt(bits, bit) {
			t.Errorf("service bit %d advertised without a hook", bit)
		}
	}
	for _, bit := range []int{12, 14, 15, 26, 34} {
		if !containsInt(bits, bit) {
			t.Errorf("service bit %d not advertised", bit)
		}
	}
	h.confirmed(1, apdu.ServiceConfirmedTextMessage, nil)
	h.expectReject(bacnet.RejectReasonUnrecognizedService)
	h.confirmed(2, apdu.ServiceConfirmedPrivateTransfer, nil)
	h.expectReject(bacnet.RejectReasonUnrecognizedService)
	h.unconfirmed(apdu.ServiceWriteGroup, nil)
	h.quiet(20 * time.Millisecond)
}

func containsInt(xs []int, x int) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestBVLCWithoutBBMD(t *testing.T) {
	h := newHarness(t)
	for fn, code := range map[bvlc.Function]uint16{
		bvlc.FunctionRegisterForeignDevice:           bvlc.ResultRegisterForeignDeviceNAK,
		bvlc.FunctionReadBroadcastDistributionTable:  bvlc.ResultReadBroadcastDistributionTableNAK,
		bvlc.FunctionWriteBroadcastDistributionTable: bvlc.ResultWriteBroadcastDistributionTableNAK,
		bvlc.FunctionReadForeignDeviceTable:          bvlc.ResultReadForeignDeviceTableNAK,
		bvlc.FunctionDeleteForeignDeviceTableEntry:   bvlc.ResultDeleteForeignDeviceTableEntryNAK,
		bvlc.FunctionDistributeBroadcastToNetwork:    bvlc.ResultDistributeBroadcastToNetworkNAK,
	} {
		msg := bvlc.Message{Function: fn}
		if fn == bvlc.FunctionRegisterForeignDevice {
			msg.TTL = 60
		}
		frame := mustEncode(t)(bvlc.Append(nil, msg))
		h.injectRaw(frame)
		got := h.next()
		if got.bvlc.Function != bvlc.FunctionResult || got.bvlc.ResultCode != code {
			t.Fatalf("%#x: %+v", fn, got.bvlc)
		}
	}
	// A Forwarded-NPDU is answered to its origin through the forwarder.
	origin := [4]byte{10, 9, 9, 9}
	raw := mustEncode(t)(npdu.Append(nil, npdu.NPDU{Version: npdu.Version1, APDU: apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceWhoIs})}))
	h.injectRaw(mustEncode(t)(bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionForwardedNPDU, OriginIP: origin, OriginPort: 47808, Payload: raw})))
	if got := h.next(); !got.pkt.Broadcast || got.pdu.UnconfirmedRequest.ServiceChoice != apdu.ServiceIAm {
		t.Fatalf("forwarded Who-Is: %+v", got)
	}
	// Ignored: BVLC-Result, malformed BVLC, network messages, other DNETs,
	// empty NPDU, malformed NPDU, a broadcast confirmed request.
	h.injectRaw(mustEncode(t)(bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult})))
	h.injectRaw([]byte{0x81, 0x0A, 0x00})
	h.injectFrom(clientEP, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1, NetworkMessage: true, NetMsgType: 1})
	h.injectFrom(clientEP, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1, Destination: bacnet.RemoteBroadcast(9), HopCount: 255, APDU: readPV(t)})
	h.injectFrom(clientEP, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1})
	h.injectRaw(mustEncode(t)(bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionOriginalUnicastNPDU, Payload: []byte{0x02}})))
	h.injectFrom(clientEP, bvlc.FunctionOriginalBroadcastNPDU, npdu.NPDU{Version: npdu.Version1, APDU: apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{InvokeID: 1, ServiceChoice: apdu.ServiceReadProperty, Payload: readPV(t)})})
	h.quiet(30 * time.Millisecond)
	if !h.diagnosed("malformed") {
		t.Fatal("malformed input not diagnosed")
	}
	// A malformed confirmed request gets a Reject.
	h.inject([]byte{0x00, 0x05, 0x09, 0x0C, 0xFF})
	h.expectReject(bacnet.RejectReasonInvalidTag)
	// A global-broadcast Who-Is from a remote network gets a remote
	// broadcast I-Am through the router it came from.
	h.injectFrom(clientEP, bvlc.FunctionOriginalUnicastNPDU, npdu.NPDU{Version: npdu.Version1, Destination: bacnet.GlobalBroadcast(), HopCount: 255,
		Source: bacnet.RemoteStation(7, bacnet.MustMAC([]byte{1})), APDU: apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceWhoIs})})
	got := h.next()
	if got.pkt.Broadcast || got.npdu.Destination.Scope() != bacnet.AddressRemoteBroadcast || got.npdu.Destination.Network() != 7 {
		t.Fatalf("remote I-Am: %+v", got)
	}
}
