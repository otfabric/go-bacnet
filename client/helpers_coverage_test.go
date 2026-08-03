// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/virtual"
	"github.com/otfabric/go-bacnet/service"
)

func TestDecodeObjectAndPropertyLists(t *testing.T) {
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 3}
	ids, err := DecodeObjectIdentifierList(bacnet.ObjectIDValue(id), 0, bacnet.DefaultDecodeLimits())
	if err != nil || len(ids) != 1 || ids[0] != id {
		t.Fatalf("%v %#v", err, ids)
	}
	ids, err = DecodeObjectIdentifierList(bacnet.ApplicationValue{
		Kind:     bacnet.ValueConstructed,
		Elements: []bacnet.Element{{Value: bacnet.ObjectIDValue(id)}, {Value: bacnet.ObjectIDValue(id)}},
	}, 1, bacnet.DefaultDecodeLimits())
	if err != nil || len(ids) != 1 {
		t.Fatal(err)
	}
	if _, err := DecodeObjectIdentifierList(bacnet.ApplicationValue{
		Kind:     bacnet.ValueConstructed,
		Elements: []bacnet.Element{{Value: bacnet.BoolValue(true)}},
	}, 10, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected object-list element error")
	}
	props, err := DecodePropertyIdentifierList(bacnet.EnumValue(uint32(bacnet.PropertyPresentValue)), 0, bacnet.DefaultDecodeLimits())
	if err != nil || props[0] != bacnet.PropertyPresentValue {
		t.Fatal(err)
	}
	props, err = DecodePropertyIdentifierList(bacnet.ApplicationValue{
		Kind:     bacnet.ValueConstructed,
		Elements: []bacnet.Element{{Value: bacnet.EnumValue(uint32(bacnet.PropertyObjectName))}, {Value: bacnet.EnumValue(1)}},
	}, 1, bacnet.DefaultDecodeLimits())
	if err != nil || len(props) != 1 || props[0] != bacnet.PropertyObjectName {
		t.Fatal(err)
	}
	if _, err := DecodePropertyIdentifierList(bacnet.ApplicationValue{
		Kind:     bacnet.ValueConstructed,
		Elements: []bacnet.Element{{Value: bacnet.BoolValue(true)}},
	}, 10, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected property-list element error")
	}
	if _, err := DecodeObjectIdentifierList(bacnet.BoolValue(true), 1, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected unsupported")
	}
	if _, err := DecodePropertyIdentifierList(bacnet.BoolValue(true), 1, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("expected unsupported property-list")
	}
}

func TestWritePriorityValidation(t *testing.T) {
	env := newVirtualPair(t)
	if err := env.Client.WritePriority(context.Background(), env.Target,
		bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}, 0, bacnet.RealValue(1)); err == nil {
		t.Fatal("expected bad priority")
	}
}

func TestWritePriorityAndRelinquish(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	if err := env.Client.WritePriority(ctx, env.Target, obj, 8, bacnet.RealValue(21)); err != nil {
		t.Fatal(err)
	}
	if err := env.Client.RelinquishPriority(ctx, env.Target, obj, 8); err != nil {
		t.Fatal(err)
	}
}

func TestReadObjectListAndPropertyListErrors(t *testing.T) {
	env := newVirtualPair(t)
	_ = env.Client.Close()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	if _, err := env.Client.ReadObjectList(context.Background(), env.Target, obj, 1); err == nil {
		t.Fatal("object-list")
	}
	if _, err := env.Client.ReadPropertyList(context.Background(), env.Target, obj, 1); err == nil {
		t.Fatal("property-list")
	}
}

func TestReadObjectListAndPropertyList(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	av := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		if choice != apdu.ServiceReadProperty {
			return nil, errors.New("unexpected")
		}
		return service.EncodeReadPropertyACK(service.ReadPropertyACK{
			Object:   obj,
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList},
			Value: bacnet.ApplicationValue{
				Kind:     bacnet.ValueConstructed,
				Elements: []bacnet.Element{{Value: bacnet.ObjectIDValue(av)}},
			},
		})
	})
	ids, err := env.Client.ReadObjectList(ctx, env.Target, obj, 10)
	if err != nil || len(ids) != 1 {
		t.Fatalf("%v %#v", err, ids)
	}

	env2 := newVirtualPair(t)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go serveComplexACK(ctx2, env2.PeerTr, env2.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeReadPropertyACK(service.ReadPropertyACK{
			Object:   obj,
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPropertyList},
			Value: bacnet.ApplicationValue{
				Kind:     bacnet.ValueConstructed,
				Elements: []bacnet.Element{{Value: bacnet.EnumValue(uint32(bacnet.PropertyPresentValue))}},
			},
		})
	})
	props, err := env2.Client.ReadPropertyList(ctx2, env2.Target, obj, 10)
	if err != nil || len(props) != 1 || props[0] != bacnet.PropertyPresentValue {
		t.Fatalf("%v %#v", err, props)
	}
}

func TestReadFileStreamAndRecords(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		switch choice {
		case apdu.ServiceAtomicReadFile:
			return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
				EndOfFile: true, Access: service.FileAccessStream, Data: []byte("abc"),
			})
		default:
			return nil, errors.New("bad choice")
		}
	})
	data, err := env.Client.ReadFileStream(ctx, env.Target, file, FileReadOptions{ChunkSize: 16, MaxTotal: 100})
	if err != nil || string(data) != "abc" {
		t.Fatalf("%v %q", err, data)
	}

	env2 := newVirtualPair(t)
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	go serveComplexACK(ctx2, env2.PeerTr, env2.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeAtomicReadFileACK(service.AtomicReadFileACK{
			EndOfFile: true, Access: service.FileAccessRecord,
			Records: [][]byte{[]byte("r1")}, RecordCount: 1,
		})
	})
	recs, err := env2.Client.ReadFileRecords(ctx2, env2.Target, file, 0, FileReadOptions{ChunkSize: 4, MaxTotal: 10})
	if err != nil || len(recs) != 1 {
		t.Fatalf("%v %#v", err, recs)
	}
}

func TestWriteFileStream(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	file := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeFile, Instance: 1}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeAtomicWriteFileACK(service.AtomicWriteFileACK{
			Access: service.FileAccessStream, StartPosition: 0,
		})
	})
	outcomes, err := env.Client.WriteFileStream(ctx, env.Target, file, 0, []byte("hello-world"), FileWriteOptions{ChunkSize: 5})
	if err != nil || len(outcomes) != 3 {
		t.Fatalf("%v %#v", err, outcomes)
	}
}

func TestReadRangeAllPages(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectList}
	n := 0
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		n++
		more := n == 1
		return service.EncodeReadRangeACK(service.ReadRangeACK{
			Object: obj, Property: prop,
			ResultFlags: service.EncodeResultFlags(n == 1, !more, more),
			ItemCount:   1,
			ItemData:    []bacnet.ApplicationValue{bacnet.UnsignedValue(uint64(n))},
		})
	})
	got, err := env.Client.ReadRangeAll(ctx, env.Target, service.ReadRangeRequest{
		Object: obj, Property: prop, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1,
	}, ReadRangePageOptions{PageCount: 1, MaxItems: 10, MaxPages: 5})
	if err != nil || len(got.Items) != 2 {
		t.Fatalf("%v items=%d pages=%d", err, len(got.Items), len(got.Pages))
	}
}

func TestRPMBatchedEmptyAndSingle(t *testing.T) {
	env := newVirtualPair(t)
	if _, err := env.Client.ReadPropertyMultipleBatched(context.Background(), env.Target, nil, RPMBatchOptions{}); err == nil {
		t.Fatal("expected empty error")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeReadPropertyMultipleACK([]service.ReadAccessResult{{
			Object: obj,
			Properties: []service.PropertyResult{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.RealValue(1.5),
			}},
		}})
	})
	got, err := env.Client.ReadPropertyMultipleBatched(ctx, env.Target, []service.ReadAccessSpecification{{
		Object:     obj,
		Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}},
	}}, RPMBatchOptions{})
	if err != nil || len(got) != 1 {
		t.Fatalf("%v %#v", err, got)
	}
}

func TestWPMBatched(t *testing.T) {
	env := newVirtualPair(t)
	if _, err := env.Client.WritePropertyMultipleBatched(context.Background(), env.Target, nil, WPMBatchOptions{}); err == nil {
		t.Fatal("expected empty")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveSimpleACK(ctx, env.PeerTr, env.Local)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	res, err := env.Client.WritePropertyMultipleBatched(ctx, env.Target, []service.WriteAccessSpecification{{
		Object: obj,
		Properties: []service.WritePropertyValue{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(2),
		}},
	}}, WPMBatchOptions{MaxSpecsPerBatch: 1})
	if err != nil || len(res.Batches) != 1 || res.Batches[0].State != BatchWriteCompleted {
		t.Fatalf("%v %#v", err, res)
	}
}

func TestReadFDTAndDeleteFDTVirtual(t *testing.T) {
	clk := clock.NewManual(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))
	local := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.2:47808"))
	bbmd := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.1:47808"))
	fd := bip.NewEndpoint(netip.MustParseAddrPort("192.168.1.9:47808"))
	tr := virtual.New(local, clk, 16)
	c, err := New(WithTransport(AdaptVirtual(tr)), withClock(clk), WithTransactionOptions(200*time.Millisecond, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()

	go func() {
		for {
			out := tr.Outbox()
			if len(out) == 0 {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			msg, err := bvlc.Parse(out[len(out)-1].Data, bacnet.DefaultDecodeLimits())
			if err != nil {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			switch msg.Function {
			case bvlc.FunctionReadForeignDeviceTable:
				payload, _ := bvlc.EncodeFDTEntries(nil, []bvlc.FDTEntry{{Address: fd, TTL: 60, Remaining: 30}})
				frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionReadForeignDeviceTableAck, Payload: payload})
				tr.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: bbmd, ReceivedAt: clk.Now()})
				return
			default:
				time.Sleep(5 * time.Millisecond)
			}
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	entries, err := c.ReadForeignDeviceTable(ctx, bbmd)
	if err != nil || len(entries) != 1 {
		t.Fatalf("%v %#v", err, entries)
	}

	tr2 := virtual.New(local, clk, 16)
	c2, err := New(WithTransport(AdaptVirtual(tr2)), withClock(clk), WithTransactionOptions(200*time.Millisecond, 0, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c2.Close() }()
	go func() {
		for {
			out := tr2.Outbox()
			if len(out) == 0 {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			msg, err := bvlc.Parse(out[len(out)-1].Data, bacnet.DefaultDecodeLimits())
			if err != nil || msg.Function != bvlc.FunctionDeleteForeignDeviceTableEntry {
				time.Sleep(5 * time.Millisecond)
				continue
			}
			frame, _ := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: 0})
			tr2.Inject(virtual.InboundPacket{Data: frame, ImmediatePeer: bbmd, ReceivedAt: clk.Now()})
			return
		}
	}()
	if err := c2.DeleteForeignDeviceTableEntry(ctx, bbmd, fd); err != nil {
		t.Fatal(err)
	}
}

func TestBVLCOperationErrorMethods(t *testing.T) {
	e := &BVLCOperationError{Operation: "Write-BDT", Code: 1}
	if e.Error() == "" || !errors.Is(e, bacnet.ErrProtocolViolation) {
		t.Fatal(e)
	}
}

func TestWPMBatchedUnknownAfterTimeout(t *testing.T) {
	env := newVirtualPair(t, WithTransactionOptions(50*time.Millisecond, 0, 0))
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	errCh := make(chan error, 1)
	var res BatchWriteResult
	go func() {
		var err error
		res, err = env.Client.WritePropertyMultipleBatched(context.Background(), env.Target, []service.WriteAccessSpecification{{
			Object: obj,
			Properties: []service.WritePropertyValue{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.RealValue(2),
			}},
		}}, WPMBatchOptions{})
		errCh <- err
	}()
	_, _ = waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	env.Clk.Advance(100 * time.Millisecond)
	err := <-errCh
	if err == nil || len(res.Batches) != 1 {
		t.Fatalf("%v %#v", err, res)
	}
	if res.Batches[0].State != BatchWriteUnknown && res.Batches[0].State != BatchWriteFailed {
		t.Fatalf("state=%v", res.Batches[0].State)
	}
}

func TestRPMBatchedShrinkOnAbort(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	specs := []service.ReadAccessSpecification{
		{Object: obj, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}}},
		{Object: obj, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyObjectName}}},
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := env.Client.ReadPropertyMultipleBatched(context.Background(), env.Target, specs, RPMBatchOptions{MaxPropertiesPerBatch: 2})
		errCh <- err
	}()
	invokeID, _ := waitConfirmedInvokeID(t, env.ClientTr, time.Second)
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendAbort(nil, apdu.AbortPDU{
		Server: true, InvokeID: invokeID, Reason: 4,
	}))
	invokeID, _ = waitConfirmedInvokeIDSince(t, env.ClientTr, 1, time.Second)
	payload, err := service.EncodeReadPropertyMultipleACK([]service.ReadAccessResult{{
		Object: obj,
		Properties: []service.PropertyResult{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
			Value:    bacnet.RealValue(1),
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceReadPropertyMultiple, Payload: payload,
	}))
	// Second property batch.
	invokeID, _ = waitConfirmedInvokeIDSince(t, env.ClientTr, 2, time.Second)
	payload, err = service.EncodeReadPropertyMultipleACK([]service.ReadAccessResult{{
		Object: obj,
		Properties: []service.PropertyResult{{
			Property: bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName},
			Value:    bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: "av"}},
		}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	injectUnicastNPDU(t, env.ClientTr, env.Peer, env.Clk.Now(), apdu.AppendComplexACK(nil, apdu.ComplexACK{
		InvokeID: invokeID, ServiceChoice: apdu.ServiceReadPropertyMultiple, Payload: payload,
	}))
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
}
