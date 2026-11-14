// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// --- registry maintenance cost ---

func boundsObservation(i uint32) DeviceObservation {
	ep := bip.NewEndpoint(netip.AddrPortFrom(netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)}), 47808))
	return DeviceObservation{Instance: i, Address: bacnet.LocalStation(bacnet.MAC{}), Origin: ep, ImmediatePeer: ep}
}

// Expiry and eviction look only at the front of the recency list, so the
// entries examined per Upsert do not depend on how many are retained.
func TestRegistryUpsertMaintenanceIndependentOfSize(t *testing.T) {
	t.Parallel()
	for _, size := range []uint32{64, 4096} {
		clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
		maxObs := int(size)
		reg := newRegistry(diag.Discard{}, clk, RegistryOptions{MaxObservations: maxObs, ObservationTTL: time.Hour})
		for i := range size {
			reg.Upsert(boundsObservation(i))
			clk.Advance(time.Millisecond)
		}
		if reg.Len() != maxObs {
			t.Fatalf("size %d: Len=%d", size, reg.Len())
		}
		const extra = 1000
		reg.maintSteps = 0
		for i := range uint32(extra) {
			reg.Upsert(boundsObservation(1_000_000 + i))
			clk.Advance(time.Millisecond)
		}
		if reg.Len() != maxObs {
			t.Fatalf("size %d: Len=%d after evictions", size, reg.Len())
		}
		// One look for expiry, one eviction per new key.
		if per := float64(reg.maintSteps) / extra; per > 2 {
			t.Fatalf("size %d: %.1f entries examined per Upsert, want <= 2", size, per)
		}

		obj := newObjectRegistry(diag.Discard{}, clk, RegistryOptions{MaxObservations: maxObs, ObservationTTL: time.Hour})
		for i := range size + extra {
			obj.Upsert(ObjectObservation{DeviceInstance: i})
			clk.Advance(time.Millisecond)
		}
		if per := float64(obj.maintSteps) / float64(size+extra); per > 2 {
			t.Fatalf("size %d: %.1f object entries examined per Upsert, want <= 2", size, per)
		}
		if len(obj.Observations()) != maxObs {
			t.Fatalf("size %d: object registry len=%d", size, len(obj.Observations()))
		}
	}
}

// Expiring many observations at once costs one step each, so the amortised
// cost per Upsert stays constant.
func TestRegistryExpiryAmortised(t *testing.T) {
	t.Parallel()
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	reg := newRegistry(diag.Discard{}, clk, RegistryOptions{MaxObservations: 8192, ObservationTTL: time.Minute})
	const n = 2048
	for i := range uint32(n) {
		reg.Upsert(boundsObservation(i))
	}
	clk.Advance(2 * time.Minute)
	reg.Upsert(boundsObservation(n))
	if reg.Len() != 1 {
		t.Fatalf("Len=%d want 1 after expiry", reg.Len())
	}
	if per := float64(reg.maintSteps) / (n + 1); per > 3 {
		t.Fatalf("%.1f entries examined per Upsert, want <= 3", per)
	}
}

// Refreshing an observation makes it the most recently seen.
func TestRegistryRefreshMovesToBack(t *testing.T) {
	t.Parallel()
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	reg := newRegistry(diag.Discard{}, clk, RegistryOptions{MaxObservations: 2, ObservationTTL: -1})
	reg.Upsert(boundsObservation(1))
	clk.Advance(time.Second)
	reg.Upsert(boundsObservation(2))
	clk.Advance(time.Second)
	reg.Upsert(boundsObservation(1)) // refresh
	clk.Advance(time.Second)
	reg.Upsert(boundsObservation(3)) // evicts 2, the least recently seen
	if len(reg.ByInstance(1)) != 1 || len(reg.ByInstance(2)) != 0 || len(reg.ByInstance(3)) != 1 {
		t.Fatalf("want 1 and 3 retained, got %+v", reg.Observations())
	}

	obj := newObjectRegistry(diag.Discard{}, clk, RegistryOptions{MaxObservations: 2, ObservationTTL: -1})
	obj.Upsert(ObjectObservation{DeviceInstance: 1})
	clk.Advance(time.Second)
	obj.Upsert(ObjectObservation{DeviceInstance: 2})
	clk.Advance(time.Second)
	obj.Upsert(ObjectObservation{DeviceInstance: 1})
	clk.Advance(time.Second)
	obj.Upsert(ObjectObservation{DeviceInstance: 3})
	var got []uint32
	for _, o := range obj.Observations() {
		got = append(got, o.DeviceInstance)
	}
	slices.Sort(got)
	if !slices.Equal(got, []uint32{1, 3}) {
		t.Fatalf("object registry retained %v, want [1 3]", got)
	}
}

// --- Forwarded-NPDU with a non-IPv4 immediate peer ---

func injectForwardedSimpleACK(t *testing.T, env *virtualPair, immediate bip.Endpoint, invokeID, svc uint8) {
	t.Helper()
	nraw, err := npdu.Append(nil, npdu.NPDU{Version: npdu.Version1,
		APDU: apdu.AppendSimpleACK(nil, apdu.SimpleACK{InvokeID: invokeID, ServiceChoice: svc})})
	if err != nil {
		t.Fatal(err)
	}
	// The BVLC origin is the target itself.
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionForwardedNPDU,
		OriginIP: [4]byte{10, 0, 0, 2}, OriginPort: 47808, Payload: nraw})
	if err != nil {
		t.Fatal(err)
	}
	env.ClientTr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: immediate, ReceivedAt: env.Clk.Now()})
}

// A Forwarded-NPDU whose immediate peer is not a valid IPv4 endpoint is
// discarded with a BVLC diagnostic and never completes a transaction; the
// same frame from the target's own endpoint does.
func TestInboundForwardedNonIPv4ImmediateIgnored(t *testing.T) {
	for _, tc := range []struct {
		name string
		ep   bip.Endpoint
	}{
		{"ipv6", bip.NewEndpoint(netip.MustParseAddrPort("[2001:db8::99]:47808"))},
		{"ipv4-mapped-ipv6", bip.NewEndpoint(netip.MustParseAddrPort("[::ffff:10.0.0.2]:47808"))},
		{"zero", bip.Endpoint{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var kinds []string
			env := newVirtualPair(t, WithDiagnosticFunc(func(d Diagnostic) {
				mu.Lock()
				kinds = append(kinds, d.Kind)
				mu.Unlock()
			}))
			type res struct {
				pdu apdu.PDU
				err error
			}
			done := make(chan res, 1)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			wp := []byte{0x0c, 0x00, 0x00, 0x00, 0x01, 0x19, 0x55, 0x3e, 0x44, 0x42, 0x28, 0x00, 0x00, 0x3f}
			go func() {
				p, err := env.Client.InvokeConfirmed(ctx, env.Target, apdu.ServiceWriteProperty, wp, ConfirmedInvokeOptions{})
				done <- res{p, err}
			}()
			id, svc := waitConfirmedInvokeID(t, env.ClientTr, 2*time.Second)

			injectForwardedSimpleACK(t, env, tc.ep, id, svc)
			select {
			case r := <-done:
				t.Fatalf("transaction completed by a frame from %q: %v %v", tc.ep.String(), r.pdu.Type, r.err)
			case <-time.After(100 * time.Millisecond):
			}
			mu.Lock()
			sawBVLC := slices.Contains(kinds, string(diag.KindBVLC))
			mu.Unlock()
			if !sawBVLC {
				t.Fatalf("no BVLC diagnostic for immediate peer %q", tc.ep.String())
			}

			// The same reply from the target's endpoint completes it.
			injectForwardedSimpleACK(t, env, env.Peer, id, svc)
			select {
			case r := <-done:
				if r.err != nil || r.pdu.Type != apdu.TypeSimpleACK {
					t.Fatalf("valid reply: %v %v", r.pdu.Type, r.err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("valid reply did not complete the transaction")
			}
		})
	}
}

// matchTargetSource never accepts a source without a valid immediate peer
// when the target names an endpoint.
func TestMatchTargetSourceInvalidImmediate(t *testing.T) {
	t.Parallel()
	peer := bip.NewEndpoint(netip.MustParseAddrPort("10.0.0.2:47808"))
	addr := bacnet.LocalStation(bacnet.MustMAC([]byte{10, 0, 0, 2, 0xBA, 0xC0}))
	remote := bacnet.RemoteStation(5, bacnet.MustMAC([]byte{7}))
	v6 := bip.NewEndpoint(netip.MustParseAddrPort("[2001:db8::1]:47808"))
	for _, tc := range []struct {
		name   string
		target Target
		src    packetSource
	}{
		{"local", Target{Address: addr, Endpoint: peer}, packetSource{bacnetAddress: addr, origin: peer, immediate: v6}},
		{"local-zero", Target{Address: addr, Endpoint: peer}, packetSource{bacnetAddress: addr, origin: peer}},
		{"forwarded", Target{Address: addr, Endpoint: peer, Origin: peer}, packetSource{bacnetAddress: addr, origin: peer, immediate: v6}},
		{"remote", Target{Address: remote, Endpoint: peer}, packetSource{bacnetAddress: remote, origin: peer, immediate: v6}},
	} {
		if matchTargetSource(tc.target, tc.src) {
			t.Errorf("%s: matched with immediate %q", tc.name, tc.src.immediate.String())
		}
		ok := tc.src
		ok.immediate = peer
		if !matchTargetSource(tc.target, ok) {
			t.Errorf("%s: valid immediate did not match", tc.name)
		}
	}
}

// --- ReadRangeAll MaxItems ---

// MaxItems bounds the items retained even when the peer reports a smaller
// ItemCount than the items it sends.
func TestReadRangeAllMaxItemsCountsRetainedItems(t *testing.T) {
	env := newVirtualPair(t)
	ctx := t.Context()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}
	items := make([]bacnet.ApplicationValue, 400)
	for i := range items {
		items[i] = bacnet.NullValue()
	}
	payload, err := service.EncodeReadRangeACK(service.ReadRangeACK{
		Object: obj, Property: prop,
		ResultFlags: service.EncodeResultFlags(false, false, true), // MORE_ITEMS
		ItemCount:   1,
		ItemData:    items,
	})
	if err != nil {
		t.Fatal(err)
	}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(uint8) ([]byte, error) { return payload, nil })

	const maxItems = 250
	res, err := env.Client.ReadRangeAll(ctx, env.Target, service.ReadRangeRequest{
		Object: obj, Property: prop, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 50,
	}, ReadRangePageOptions{MaxItems: maxItems, MaxPages: 16})
	if err != nil {
		t.Fatal(err)
	}
	retained := 0
	for _, p := range res.Pages {
		retained += len(p.ItemData)
	}
	if len(res.Items) > maxItems || retained > maxItems {
		t.Fatalf("len(Items)=%d, ItemData across pages=%d, MaxItems=%d", len(res.Items), retained, maxItems)
	}
	if len(res.Items) != maxItems || !res.Partial || res.StoppedReason != "MaxItems" || len(res.Pages) != 1 {
		t.Fatalf("items=%d pages=%d partial=%v reason=%q", len(res.Items), len(res.Pages), res.Partial, res.StoppedReason)
	}
	if cap(res.Pages[0].ItemData) >= len(items) {
		t.Fatalf("truncated page keeps the full backing array (cap %d)", cap(res.Pages[0].ItemData))
	}
}

// Typed log records count once each and are truncated to the budget.
func TestReadRangeAllMaxItemsTruncatesLogRecords(t *testing.T) {
	t.Parallel()
	ack := service.ReadRangeACK{
		ItemCount:  5,
		ItemData:   make([]bacnet.ApplicationValue, 15),
		LogRecords: make([]service.LogRecord, 5),
	}
	if got := readRangePageItems(ack); got != 5 {
		t.Fatalf("page items=%d want 5", got)
	}
	cut := truncateReadRangeACK(ack, 2)
	if len(cut.LogRecords) != 2 || cut.ItemData != nil {
		t.Fatalf("records=%d itemData=%d", len(cut.LogRecords), len(cut.ItemData))
	}
	if got := readRangePageItems(service.ReadRangeACK{ItemCount: 1 << 31, ItemData: make([]bacnet.ApplicationValue, 3)}); got != 1<<31 {
		t.Fatalf("large ItemCount counted as %d", got)
	}
	if cut := truncateReadRangeACK(service.ReadRangeACK{ItemData: make([]bacnet.ApplicationValue, 3)}, -1); len(cut.ItemData) != 0 {
		t.Fatalf("negative keep retained %d", len(cut.ItemData))
	}
}

// --- RPM results bound to the request ---

func rpmACKReply(t *testing.T, results []service.ReadAccessResult) func(req *apdu.ConfirmedRequest) []byte {
	t.Helper()
	payload, err := service.EncodeReadPropertyMultipleACK(results)
	if err != nil {
		t.Fatal(err)
	}
	return func(req *apdu.ConfirmedRequest) []byte {
		return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: payload})
	}
}

// ReadPropertyMultipleBatched rejects results whose properties do not answer
// the explicit references requested.
func TestRPMBatchedRejectsUnrequestedProperties(t *testing.T) {
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	name := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName}
	idx := func(i uint32) *uint32 { return &i }
	result := func(refs ...bacnet.PropertyReference) []service.ReadAccessResult {
		r := service.ReadAccessResult{Object: obj}
		for _, ref := range refs {
			r.Properties = append(r.Properties, service.PropertyResult{Property: ref, Value: bacnet.RealValue(1)})
		}
		return []service.ReadAccessResult{r}
	}
	for _, tc := range []struct {
		name    string
		want    []bacnet.PropertyReference
		results []service.ReadAccessResult
		ok      bool
	}{
		{"exact", []bacnet.PropertyReference{pv, name}, result(pv, name), true},
		{"extra", []bacnet.PropertyReference{pv}, result(pv, name), false},
		{"missing", []bacnet.PropertyReference{pv, name}, result(pv), false},
		{"reordered", []bacnet.PropertyReference{pv, name}, result(name, pv), false},
		{"other-identifier", []bacnet.PropertyReference{pv}, result(name), false},
		{"array-index-added", []bacnet.PropertyReference{pv}, result(bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue, ArrayIndex: idx(1)}), false},
		{"array-index-differs", []bacnet.PropertyReference{{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: idx(1)}}, result(bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: idx(2)}), false},
		{"all-expands", []bacnet.PropertyReference{{Identifier: bacnet.PropertyAll}}, result(pv, name, bacnet.PropertyReference{Identifier: bacnet.PropertyStatusFlags}), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			env := newVirtualPair(t)
			ctx := t.Context()
			go servePeer(ctx, env, rpmACKReply(t, tc.results))
			got, err := env.Client.ReadPropertyMultipleBatched(ctx, env.Target,
				[]service.ReadAccessSpecification{{Object: obj, Properties: tc.want}}, RPMBatchOptions{})
			if tc.ok {
				if err != nil || len(got[0].Properties) != len(tc.results[0].Properties) {
					t.Fatalf("err=%v props=%d", err, len(got[0].Properties))
				}
				return
			}
			if !errors.Is(err, bacnet.ErrProtocolViolation) {
				t.Fatalf("err=%v, want ErrProtocolViolation", err)
			}
			if len(got) == 1 && len(got[0].Properties) != 0 {
				t.Fatalf("kept %d properties of a rejected result", len(got[0].Properties))
			}
		})
	}
}

// An ALL, REQUIRED or OPTIONAL reference expands to a bounded number of results.
func TestRPMResultSpecialReferenceBound(t *testing.T) {
	t.Parallel()
	all := []bacnet.PropertyReference{{Identifier: bacnet.PropertyRequired}, {Identifier: bacnet.PropertyPresentValue}}
	limit := 1 + rpmPropertiesPerSpecialReference
	if err := checkRPMResultProperties(all, make([]service.PropertyResult, limit)); err != nil {
		t.Fatalf("at the bound: %v", err)
	}
	if err := checkRPMResultProperties(all, make([]service.PropertyResult, limit+1)); err == nil {
		t.Fatal("over the bound accepted")
	}
}

// ReadObjectList read by index never returns more entries than requested,
// whatever the RPM reply holds.
func TestReadObjectListByIndexBoundedByMaxItems(t *testing.T) {
	dev := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	limits := bacnet.DefaultDecodeLimits()
	stuffed := service.ReadAccessResult{Object: dev}
	for i := range uint32(1000) {
		one := uint32(1)
		stuffed.Properties = append(stuffed.Properties, service.PropertyResult{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList, ArrayIndex: &one},
			Value:    bacnet.ObjectIDValue(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: i}),
		})
	}
	rpm := rpmACKReply(t, []service.ReadAccessResult{stuffed})
	for _, tc := range []struct {
		maxItems int
		count    uint64
	}{{10, 10}, {0, 4096}} {
		env := newVirtualPair(t)
		ctx := t.Context()
		go servePeer(ctx, env, func(req *apdu.ConfirmedRequest) []byte {
			switch req.ServiceChoice {
			case apdu.ServiceReadProperty:
				rp, err := service.DecodeReadProperty(req.Payload, limits)
				if err != nil || rp.Property.ArrayIndex == nil {
					return apdu.AppendAbort(nil, apdu.AbortPDU{Server: true, InvokeID: req.InvokeID, Reason: bacnet.AbortReasonBufferOverflow})
				}
				p, _ := service.EncodeReadPropertyACK(service.ReadPropertyACK{Object: dev, Property: rp.Property, Value: bacnet.UnsignedValue(tc.count)})
				return apdu.AppendComplexACK(nil, apdu.ComplexACK{InvokeID: req.InvokeID, ServiceChoice: req.ServiceChoice, Payload: p})
			case apdu.ServiceReadPropertyMultiple:
				return rpm(req)
			}
			return nil
		})
		got, err := env.Client.ReadObjectList(ctx, env.Target, dev, tc.maxItems)
		if !errors.Is(err, bacnet.ErrProtocolViolation) {
			t.Fatalf("maxItems=%d: err=%v, want ErrProtocolViolation", tc.maxItems, err)
		}
		if uint64(len(got)) > tc.count {
			t.Fatalf("maxItems=%d: %d entries", tc.maxItems, len(got))
		}
	}
}
