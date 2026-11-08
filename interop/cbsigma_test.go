//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// cB-Sigma peer, bacnet-interop adapter cbsigma (new in bacnet-interop
// 0.13.0): CBSIGMA_ROLE=device is the device package of
// github.com/cB-Sigma/bacnet, CBSIGMA_ROLE=sim github.com/cB-Sigma/bacnet-sim.
// Both are Go, by one author, independent of go-bacnet. The simulator has no
// Device object and switches that make it misbehave (CBSIGMA_* below); see
// bacnet-interop adapters/cbsigma/README.md.

const (
	defaultCBSigmaImage      = "bacnet-interop-cbsigma:local"
	cbsigmaMinAdapterVersion = "0.13.0"
)

func cbsigmaImage() string { return getEnv("CBSIGMA_IMAGE", defaultCBSigmaImage) }

func startCBSigma(t *testing.T, env ...string) *peerHandle {
	t.Helper()
	requireAdapterVersion(t, cbsigmaImage(), cbsigmaMinAdapterVersion)
	return startPeer(t, cbsigmaImage(), "cbsigma", env...)
}

func cbsigmaSim(t *testing.T, env ...string) *peerHandle {
	t.Helper()
	return startCBSigma(t, append([]string{"CBSIGMA_ROLE=sim"}, env...)...)
}

func requireBACnetError(t *testing.T, what string, err error, class, code uint16) {
	t.Helper()
	var er *bacnet.ErrorResponse
	if !errors.As(err, &er) || er.Class != class || er.Code != code {
		t.Fatalf("%s: got %v, want error class %d code %d", what, err, class, code)
	}
}

func requireReject(t *testing.T, what string, err error, reason uint8) {
	t.Helper()
	var rj *bacnet.RejectError
	if !errors.As(err, &rj) || rj.Reason != reason {
		t.Fatalf("%s: got %v, want reject reason %d", what, err, reason)
	}
}

func readReal(t *testing.T, ctx context.Context, c *client.Client, peer *peerHandle, o bacnet.ObjectIdentifier) float32 {
	t.Helper()
	v, err := c.ReadProperty(ctx, peer.target, o, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	if err != nil {
		t.Fatalf("ReadProperty %v present-value: %v", o, err)
	}
	f, err := bacnet.AsReal(v)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func whoIsFinds(t *testing.T, c *client.Client, peer *peerHandle, instance uint32, broadcast bool) {
	t.Helper()
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	low, high := instance, instance
	if err := c.SendWhoIs(ctx, peer.endpoint, broadcast, client.DiscoveryOptions{LowLimit: &low, HighLimit: &high}); err != nil {
		t.Fatalf("SendWhoIs: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, obs := range c.Devices() {
			if obs.Instance == instance {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no I-Am for %d; devices=%v", instance, c.Devices())
}

// ---- device role ----------------------------------------------------------

func TestCBSigmaDeviceWhoIsReadAndObjectList(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startCBSigma(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	whoIsFinds(t, c, peer, dev.DeviceInstance, false)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()
	name, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	if err != nil {
		t.Fatalf("device object-name: %v", err)
	}
	if got := characterString(t, name); got != dev.DeviceName {
		t.Fatalf("object-name %q, want %q", got, dev.DeviceName)
	}
	list, err := c.ReadObjectList(ctx, peer.target, deviceObject(dev), 0)
	if err != nil {
		t.Fatalf("ReadObjectList: %v", err)
	}
	av, wantPV := analogValueObject(dev)
	if !containsObject(list, deviceObject(dev)) || !containsObject(list, av) || len(list) != len(dev.Objects) {
		t.Fatalf("object-list %v, want the %d fixture objects", list, len(dev.Objects))
	}
	if pv := readReal(t, ctx, c, peer, av); math.Abs(float64(pv-wantPV)) > 0.01 {
		t.Fatalf("AV present-value %v, want %v", pv, wantPV)
	}
	_, err = c.ReadProperty(ctx, peer.target, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 999},
		bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
	requireBACnetError(t, "unknown object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	_, err = c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: unknownPropertyID})
	requireBACnetError(t, "unknown property", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
}

func TestCBSigmaDeviceRPMAndSegmentation(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startCBSigma(t)
	if peer.assertedByReexec {
		return
	}
	av, wantPV := analogValueObject(dev)
	specs := []service.ReadAccessSpecification{
		{Object: deviceObject(dev), Properties: []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName}, {Identifier: bacnet.PropertyObjectIdentifier},
			{Identifier: bacnet.PropertyObjectType}, {Identifier: bacnet.PropertyVendorIdentifier},
			{Identifier: bacnet.PropertyModelName}, {Identifier: bacnet.PropertySystemStatus},
			{Identifier: bacnet.PropertyProtocolVersion}, {Identifier: bacnet.PropertyProtocolRevision},
			{Identifier: bacnet.PropertyMaxAPDULength}, {Identifier: bacnet.PropertySegmentation},
			{Identifier: unknownPropertyID},
		}},
		{Object: av, Properties: []bacnet.PropertyReference{
			{Identifier: bacnet.PropertyObjectName}, {Identifier: bacnet.PropertyPresentValue},
			{Identifier: bacnet.PropertyStatusFlags}, {Identifier: bacnet.PropertyUnits},
		}},
	}
	for _, maxAPDU := range []uint16{1476, 50} {
		c := newClientWithAdvertisedMaxAPDU(t, maxAPDU)
		ctx, cancel := withTimeout(t, 15*time.Second)
		results, err := c.ReadPropertyMultiple(ctx, peer.target, specs)
		cancel()
		if err != nil {
			t.Fatalf("RPM (client max APDU %d): %v", maxAPDU, err)
		}
		if got := characterString(t, findPropertyValue(t, results[0], bacnet.PropertyObjectName)); got != dev.DeviceName {
			t.Fatalf("object-name %q", got)
		}
		var er *bacnet.ErrorResponse
		if e := results[0].Properties[10].Err; !errors.As(e, &er) || er.Code != bacnet.ErrorCodeUnknownProperty {
			t.Fatalf("unknown property in RPM: %v", e)
		}
		pv, err := bacnet.AsReal(findPropertyValue(t, results[1], bacnet.PropertyPresentValue))
		if err != nil || math.Abs(float64(pv-wantPV)) > 0.01 {
			t.Fatalf("AV present-value %v %v", pv, err)
		}
	}
}

func TestCBSigmaDeviceWritePriorityAndCOV(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startCBSigma(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()
	av, baseline := analogValueObject(dev)
	sub, err := c.SubscribeCOV(ctx, peer.target, av, client.COVOptions{Lifetime: 60, BufferSize: 8})
	if err != nil {
		t.Fatalf("SubscribeCOV: %v", err)
	}
	defer func() { _ = sub.Close() }()
	if err := c.WritePriority(ctx, peer.target, av, 8, bacnet.RealValue(42)); err != nil {
		t.Fatalf("WritePriority 8: %v", err)
	}
	if pv := readReal(t, ctx, c, peer, av); pv != 42 {
		t.Fatalf("present-value after write %v, want 42", pv)
	}
	deadline := time.Now().Add(8 * time.Second)
	for {
		note := waitCOVNotification(t, sub, time.Until(deadline))
		if note.MonitoredObject != av {
			t.Fatalf("COV object %v", note.MonitoredObject)
		}
		if len(note.Values) > 0 && note.Values[0].Value.Kind == bacnet.ValueReal && note.Values[0].Value.Real == 42 {
			break
		}
	}
	if err := c.RelinquishPriority(ctx, peer.target, av, 8); err != nil {
		t.Fatalf("RelinquishPriority 8: %v", err)
	}
	if pv := readReal(t, ctx, c, peer, av); math.Abs(float64(pv-baseline)) > 0.01 {
		t.Fatalf("present-value after relinquish %v, want %v", pv, baseline)
	}
	err = c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
		bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "x"}}, nil)
	requireBACnetError(t, "write object-name", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied)
	if err := sub.Close(); err != nil {
		t.Fatalf("cancel COV: %v", err)
	}
}

func TestCBSigmaDeviceWritePropertyMultiple(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startCBSigma(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	prio := uint8(9)
	err := c.WritePropertyMultiple(ctx, peer.target, []service.WriteAccessSpecification{{
		Object: av,
		Properties: []service.WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Value: bacnet.RealValue(33), Priority: &prio,
		}},
	}})
	if err != nil {
		t.Fatalf("WPM: %v", err)
	}
	if pv := readReal(t, ctx, c, peer, av); pv != 33 {
		t.Fatalf("present-value after WPM %v", pv)
	}
}

func TestCBSigmaDeviceWhoHasByName(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startCBSigma(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	name := bacnet.CharacterString{Value: "AV-1"}
	if err := c.SendWhoHas(ctx, peer.endpoint, false, client.WhoHasOptions{Name: &name}); err != nil {
		t.Fatalf("SendWhoHas: %v", err)
	}
	av, _ := analogValueObject(dev)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, o := range c.Objects() {
			if o.Object == av {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("no I-Have for AV-1: %v", c.Objects())
}

// ---- sim role -------------------------------------------------------------

func TestCBSigmaSimReadWriteCOV(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := cbsigmaSim(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	whoIsFinds(t, c, peer, dev.DeviceInstance, false)
	ctx, cancel := withTimeout(t, 30*time.Second)
	defer cancel()
	av, baseline := analogValueObject(dev)
	if pv := readReal(t, ctx, c, peer, av); math.Abs(float64(pv-baseline)) > 0.01 {
		t.Fatalf("present-value %v, want %v", pv, baseline)
	}
	// The simulator has no Device object and no object-name.
	_, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
	requireBACnetError(t, "device object-name", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
	results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{{Object: av, Properties: []bacnet.PropertyReference{
		{Identifier: bacnet.PropertyPresentValue}, {Identifier: bacnet.PropertyStatusFlags}, {Identifier: bacnet.PropertyUnits},
		{Identifier: unknownPropertyID},
	}}})
	if err != nil {
		t.Fatalf("RPM: %v", err)
	}
	if e := results[0].Properties[3].Err; e == nil {
		t.Fatal("unknown property in RPM has no error")
	}
	sub, err := c.SubscribeCOV(ctx, peer.target, av, client.COVOptions{Lifetime: 120, BufferSize: 8})
	if err != nil {
		t.Fatalf("SubscribeCOV: %v", err)
	}
	defer func() { _ = sub.Close() }()
	prio := uint8(8)
	if err := c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, bacnet.RealValue(55.5), &prio); err != nil {
		t.Fatalf("WriteProperty: %v", err)
	}
	note := waitCOVNotification(t, sub, 8*time.Second)
	if note.MonitoredObject != av || note.ProcessIdentifier == 0 {
		t.Fatalf("COV notification %+v", note)
	}
	awaitOperation(t, peer, "write-property", 3*time.Second)
}

func TestCBSigmaSimReadRange(t *testing.T) {
	dev := loadDeviceFixture(t)
	tl, ok := trendLogObject(dev)
	if !ok {
		failOrSkip(t, "fixture has no trend log")
	}
	peer := cbsigmaSim(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 20*time.Second)
	defer cancel()
	ref := bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}
	byPos, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 3})
	if err != nil || byPos.ItemCount != 3 || len(byPos.LogRecords) != 3 || byPos.LogRecords[2].Datum.Real != 22 {
		t.Fatalf("by position: %+v %v", byPos, err)
	}
	bySeq, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeBySequenceNumber, ReferenceIndex: 10, Count: 4})
	if err != nil || bySeq.ItemCount != 4 || bySeq.FirstSequence == nil || *bySeq.FirstSequence != 10 || bySeq.LogRecords[0].Datum.Real != 29 {
		t.Fatalf("by sequence: %+v %v", bySeq, err)
	}
	byTime, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeByTime,
		ReferenceTime: bacnetDateTime(time.Date(2024, 1, 1, 0, 30, 0, 0, time.UTC)), Count: 2})
	if err != nil || byTime.ItemCount != 2 || byTime.LogRecords[0].Datum.Real != 23 {
		t.Fatalf("by time (after record 3): %+v %v", byTime, err)
	}
	all, err := c.ReadRangeAll(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: ref, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 10}, client.ReadRangePageOptions{})
	if err != nil || len(all.LogRecords) != 36 {
		t.Fatalf("ReadRangeAll: %d records, %v", len(all.LogRecords), err)
	}
}

func TestCBSigmaSimReadRangeBySequenceRefused(t *testing.T) {
	dev := loadDeviceFixture(t)
	tl, _ := trendLogObject(dev)
	peer := cbsigmaSim(t, "CBSIGMA_REJECT_SEQ=1")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	_, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By: service.ReadRangeBySequenceNumber, ReferenceIndex: 1, Count: 2})
	requireBACnetError(t, "by sequence refused", err, bacnet.ErrorClassServices, 45)
}

// Segmented ReadRange answers (36 records in 128-octet APDUs, 6 segments)
// with go-bacnet's receive window 1 (the default) and 4.
//
// A segment the simulator leaves out once (CBSIGMA_DROP_SEG) is recovered
// only when the gap is inside a window: the client sees the next segment out
// of order and NAKs (clause 5.4.4.3), and the simulator resends from there.
// At the end of a window (always, with window 1) nothing arrives after it;
// the simulator never retransmits on its own segment timeout, so the client
// times out. A stalled answer (CBSIGMA_STALL_SEG) always times out. Either
// way the next request must succeed.
func TestCBSigmaSimSegmentation(t *testing.T) {
	dev := loadDeviceFixture(t)
	tl, _ := trendLogObject(dev)
	req := service.ReadRangeRequest{Object: tl, Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer},
		By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 36}
	for _, tc := range []struct {
		name   string
		window uint8 // go-bacnet receive window
		env    []string
		ok     bool
	}{
		{"plain/window-1", 1, []string{"CBSIGMA_MAX_APDU=128"}, true},
		{"plain/window-4", 4, []string{"CBSIGMA_MAX_APDU=128"}, true},
		{"sim-window-1/window-4", 4, []string{"CBSIGMA_MAX_APDU=128", "CBSIGMA_WINDOW=1"}, true},
		{"drop-segment-2/window-1", 1, []string{"CBSIGMA_MAX_APDU=128", "CBSIGMA_DROP_SEG=2"}, false},
		{"drop-segment-2/window-4", 4, []string{"CBSIGMA_MAX_APDU=128", "CBSIGMA_DROP_SEG=2"}, true},
		{"drop-segment-3/window-4", 4, []string{"CBSIGMA_MAX_APDU=128", "CBSIGMA_DROP_SEG=3"}, true},
		{"stall-before-segment-2/window-1", 1, []string{"CBSIGMA_MAX_APDU=128", "CBSIGMA_STALL_SEG=2"}, false},
		{"stall-before-segment-2/window-4", 4, []string{"CBSIGMA_MAX_APDU=128", "CBSIGMA_STALL_SEG=2"}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			peer := cbsigmaSim(t, tc.env...)
			if peer.assertedByReexec {
				return
			}
			c := newClientOpts(t, client.WithSegmentWindow(tc.window))
			ctx, cancel := withTimeout(t, 20*time.Second)
			defer cancel()
			start := time.Now()
			ack, err := c.ReadRange(ctx, peer.target, req)
			if tc.ok {
				if err != nil || len(ack.LogRecords) != 36 || ack.LogRecords[35].Datum.Real != 55 {
					t.Fatalf("segmented ReadRange: %d records, %v", len(ack.LogRecords), err)
				}
			} else {
				var ab *bacnet.AbortError
				if !errors.As(err, &ab) || ab.Reason != bacnet.AbortReasonTSMTimeout {
					t.Fatalf("incomplete segmented answer: got %v, want Abort tsm-timeout", err)
				}
				t.Logf("incomplete answer: %v after %v", err, time.Since(start))
			}
			// The peer still answers afterwards.
			if _, err := c.ReadRange(ctx, peer.target, service.ReadRangeRequest{Object: tl,
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1}); err != nil {
				t.Fatalf("ReadRange after: %v", err)
			}
		})
	}
}

func TestCBSigmaSimRejectRPMAndWPM(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := cbsigmaSim(t, "CBSIGMA_REJECT_RPM=1", "CBSIGMA_REJECT_WPM=1")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	_, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{{Object: av,
		Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}}}})
	requireReject(t, "RPM", err, bacnet.RejectReasonUnrecognizedService)
	err = c.WritePropertyMultiple(ctx, peer.target, []service.WriteAccessSpecification{{Object: av,
		Properties: []service.WritePropertyValue{{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}, Value: bacnet.RealValue(1)}}}})
	requireReject(t, "WPM", err, bacnet.RejectReasonUnrecognizedService)
}

func TestCBSigmaSimAlarmsAndEvents(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := cbsigmaSim(t, "CBSIGMA_EVENTS=1")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()
	av, _ := analogValueObject(dev)
	sum, err := c.GetAlarmSummary(ctx, peer.target)
	if err != nil || len(sum.Entries) != 1 || sum.Entries[0].Object != av || sum.Entries[0].AlarmState != 3 {
		t.Fatalf("GetAlarmSummary: %+v %v", sum, err)
	}
	info, err := c.GetEventInformation(ctx, peer.target, service.GetEventInformationRequest{})
	if err != nil || len(info.Summaries) != 1 || info.Summaries[0].Object != av || info.Summaries[0].EventState != 3 || info.MoreEvents {
		t.Fatalf("GetEventInformation: %+v %v", info, err)
	}
	if err := c.AcknowledgeAlarm(ctx, peer.target, service.AcknowledgeAlarmRequest{
		ProcessIdentifier: 1, EventObject: av, EventStateAcked: 3,
		TimeStamp:            eventTimeStamp(t, info.Summaries[0].EventTimeStamps, 0),
		AcknowledgmentSource: bacnet.CharacterString{Value: "go-bacnet interop"},
		TimeOfAcknowledgment: service.TimeStamp{Choice: service.TimeStampSequence, Sequence: 2},
	}); err != nil {
		t.Fatalf("AcknowledgeAlarm: %v", err)
	}
	awaitOperation(t, peer, "acknowledge-alarm", 3*time.Second)
}

func TestCBSigmaSimNoEvents(t *testing.T) {
	peer := cbsigmaSim(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	sum, err := c.GetAlarmSummary(ctx, peer.target)
	if err != nil || len(sum.Entries) != 0 {
		t.Fatalf("empty GetAlarmSummary: %+v %v", sum, err)
	}
	_, err = c.GetEventInformation(ctx, peer.target, service.GetEventInformationRequest{})
	requireReject(t, "GetEventInformation", err, bacnet.RejectReasonUnrecognizedService)
}

func TestCBSigmaSimForeignDevice(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := cbsigmaSim(t, "BACNET_BBMD=1")
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithForeignDevice(client.ForeignDeviceConfig{BBMD: peer.endpoint, TTL: 60 * time.Second}))
	deadline := time.Now().Add(8 * time.Second)
	for !c.ForeignDeviceRegistered() {
		if time.Now().After(deadline) {
			t.Fatal("foreign-device registration did not succeed")
		}
		time.Sleep(50 * time.Millisecond)
	}
	awaitOperation(t, peer, "register-foreign-device", 3*time.Second)
	whoIsFinds(t, c, peer, dev.DeviceInstance, true) // Distribute-Broadcast-To-Network
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	av, want := analogValueObject(dev)
	if pv := readReal(t, ctx, c, peer, av); math.Abs(float64(pv-want)) > 0.01 {
		t.Fatalf("present-value via FD %v", pv)
	}
}

func TestCBSigmaSimForeignDeviceNAK(t *testing.T) {
	peer := cbsigmaSim(t, "BACNET_BBMD=1", "CBSIGMA_FD_NAK=1")
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithForeignDevice(client.ForeignDeviceConfig{BBMD: peer.endpoint, TTL: 60 * time.Second}))
	awaitOperation(t, peer, "register-foreign-device", 5*time.Second)
	time.Sleep(time.Second)
	if c.ForeignDeviceRegistered() {
		t.Fatal("registered although the BBMD answered Register-Foreign-Device NAK")
	}
}

func TestCBSigmaSimDeviceManagementAndTimeSync(t *testing.T) {
	peer := cbsigmaSim(t)
	if peer.assertedByReexec {
		return
	}
	c := newClientOpts(t, client.WithDeviceManagementEnabled(client.DeviceManagementConfirm))
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	if err := c.ReinitializeDevice(ctx, peer.target, service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart}); err != nil {
		t.Fatalf("ReinitializeDevice: %v", err)
	}
	awaitOperation(t, peer, "reinitialize-device", 3*time.Second)
	wrong := bacnet.CharacterString{Value: "wrong"}
	err := c.DeviceCommunicationControl(ctx, peer.target, service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableEnable, Password: &wrong})
	requireBACnetError(t, "DCC wrong password", err, bacnet.ErrorClassSecurity, bacnet.ErrorCodePasswordFailure)
	if err := c.TimeSynchronization(ctx, peer.endpoint, false, func() service.TimeSynchronization {
		dt := bacnetDateTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
		return service.TimeSynchronization(dt)
	}()); err != nil {
		t.Fatalf("TimeSynchronization: %v", err)
	}
	awaitOperation(t, peer, "time-synchronization", 3*time.Second)
}

// Segmented confirmed requests (the client segments for a 50-octet peer).
func TestCBSigmaSegmentedRequests(t *testing.T) {
	for _, role := range []string{"device", "sim"} {
		t.Run(role, func(t *testing.T) {
			dev := loadDeviceFixture(t)
			peer := startCBSigma(t, "CBSIGMA_ROLE="+role)
			if peer.assertedByReexec {
				return
			}
			c := newDiscoveryClient(t)
			whoIsFinds(t, c, peer, dev.DeviceInstance, false)
			ctx, cancel := withTimeout(t, 20*time.Second)
			defer cancel()
			av, _ := analogValueObject(dev)
			target := peer.target
			target.MaxAPDU = 50
			var specs []service.WriteAccessSpecification
			if role == "device" {
				specs = []service.WriteAccessSpecification{{Object: av, Properties: []service.WritePropertyValue{{
					Property: bacnet.PropertyReference{Identifier: bacnet.PropertyDescription},
					Value:    bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: strings.Repeat("x", 80)}},
				}}}}
			} else {
				prio := uint8(10)
				var vals []service.WritePropertyValue
				for i := 0; i < 8; i++ {
					vals = append(vals, service.WritePropertyValue{Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
						Value: bacnet.RealValue(float32(60 + i)), Priority: &prio})
				}
				specs = []service.WriteAccessSpecification{{Object: av, Properties: vals}}
			}
			if err := c.WritePropertyMultiple(ctx, target, specs); err != nil {
				t.Fatalf("segmented WPM: %v", err)
			}
			if role == "device" {
				v, err := c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyDescription})
				if err != nil || characterString(t, v) != strings.Repeat("x", 80) {
					t.Fatalf("description after segmented WPM: %v", err)
				}
			} else if pv := readReal(t, ctx, c, peer, av); pv != 67 {
				t.Fatalf("present-value after segmented WPM %v, want 67", pv)
			}
		})
	}
}

func TestCBSigmaSimUnconfirmedMessaging(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := cbsigmaSim(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()
	if err := c.UnconfirmedTextMessage(ctx, peer.endpoint, false, service.TextMessage{TextMessageSourceDevice: deviceObject(dev), Message: "hello"}); err != nil {
		t.Fatal(err)
	}
	awaitOperation(t, peer, "unconfirmed-text-message", 3*time.Second)
	if err := c.UnconfirmedPrivateTransfer(ctx, peer.endpoint, false, service.PrivateTransfer{VendorID: 999, ServiceNumber: 1}); err != nil {
		t.Fatal(err)
	}
	awaitOperation(t, peer, "unconfirmed-private-transfer", 3*time.Second)
	if err := c.UTCTimeSynchronization(ctx, peer.endpoint, false, func() service.TimeSynchronization {
		dt := bacnetDateTime(time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
		return service.TimeSynchronization(dt)
	}()); err != nil {
		t.Fatal(err)
	}
	awaitOperation(t, peer, "utc-time-synchronization", 3*time.Second)
	err := c.ConfirmedTextMessage(ctx, peer.target, service.TextMessage{TextMessageSourceDevice: deviceObject(dev), Message: "hello"})
	requireReject(t, "ConfirmedTextMessage", err, bacnet.RejectReasonUnrecognizedService)
}

// Regression (fixed in go-bacnet v0.6.0): with a receive window above 1 the
// client did not acknowledge segment 0 of a segmented ComplexACK (clause
// 5.4.4.2), so every segmented answer timed out. Independent senders: the
// cB-Sigma device role, BACpypes3 and legacy BACpypes.
func TestSegmentedAnswerWithReceiveWindow(t *testing.T) {
	for _, p := range []struct {
		name, image string
		start       func(t *testing.T) *peerHandle
	}{
		{"cbsigma", cbsigmaImage(), func(t *testing.T) *peerHandle { return startCBSigma(t) }},
		{"bacpypes3", getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), func(t *testing.T) *peerHandle {
			return startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
		}},
		{"bacpypes", bacpypesImage(), func(t *testing.T) *peerHandle { return startBACpypes(t) }},
	} {
		for _, w := range []uint8{1, 2, 4, 16} {
			t.Run(fmt.Sprintf("%s/window-%d", p.name, w), func(t *testing.T) {
				dev := loadDeviceFixture(t)
				peer := p.start(t)
				if peer.assertedByReexec {
					return
				}
				c := newClientOpts(t, client.WithAdvertisedMaxAPDU(50), client.WithSegmentWindow(w))
				ctx, cancel := withTimeout(t, 15*time.Second)
				defer cancel()
				av, _ := analogValueObject(dev)
				results, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
					{Object: deviceObject(dev), Properties: []bacnet.PropertyReference{
						{Identifier: bacnet.PropertyObjectName}, {Identifier: bacnet.PropertyObjectIdentifier},
						{Identifier: bacnet.PropertyObjectType}, {Identifier: bacnet.PropertyVendorIdentifier},
						{Identifier: bacnet.PropertyModelName}, {Identifier: bacnet.PropertySystemStatus},
						{Identifier: bacnet.PropertyProtocolVersion}, {Identifier: bacnet.PropertyProtocolRevision},
						{Identifier: bacnet.PropertyMaxAPDULength}, {Identifier: bacnet.PropertySegmentation}}},
					{Object: av, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyObjectName}, {Identifier: bacnet.PropertyPresentValue}}},
				})
				if err != nil {
					t.Fatalf("segmented RPM answer, receive window %d: %v", w, err)
				}
				if got := characterString(t, findPropertyValue(t, results[0], bacnet.PropertyObjectName)); got != dev.DeviceName {
					t.Fatalf("object-name %q, want %q", got, dev.DeviceName)
				}
			})
		}
	}
}
