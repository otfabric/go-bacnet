// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func TestLifeSafetyOperationRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeLifeSafetyPoint, Instance: 1}
	raw, err := service.EncodeLifeSafetyOperation(service.LifeSafetyOperationRequest{
		RequestingProcessIdentifier: 1,
		RequestingSource:            "ops",
		Request:                     1,
		Object:                      &obj,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := service.DecodeLifeSafetyOperation(raw, limits)
	if err != nil || got.Object == nil || *got.Object != obj {
		t.Fatalf("%+v %v", got, err)
	}

	noObj, err := service.EncodeLifeSafetyOperation(service.LifeSafetyOperationRequest{
		RequestingProcessIdentifier: 2,
		RequestingSource:            "ops",
		Request:                     0,
	})
	if err != nil {
		t.Fatal(err)
	}
	gotNoObj, err := service.DecodeLifeSafetyOperation(noObj, limits)
	if err != nil || gotNoObj.Object != nil {
		t.Fatalf("got %+v err=%v", gotNoObj, err)
	}
}

func TestLifeSafetyOperationDecodeErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	if _, err := service.DecodeLifeSafetyOperation([]byte{0xff}, limits); err == nil {
		t.Fatal("expected parse error")
	}
	partial, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeLifeSafetyOperation(partial, limits); err == nil {
		t.Fatal("expected missing fields")
	}
	badTag, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	badTag, err = bacnet.AppendContextCharacterString(badTag, 1, bacnet.CharacterString{Value: "s"})
	if err != nil {
		t.Fatal(err)
	}
	badTag, err = bacnet.AppendContextUnsigned(badTag, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	badTag, err = bacnet.AppendContextUnsigned(badTag, 9, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeLifeSafetyOperation(badTag, limits); err == nil {
		t.Fatal("expected bad tag")
	}
	badSource, err := bacnet.AppendContextUnsigned(nil, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	badSource, err = bacnet.AppendContextTagged(badSource, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	badSource, err = bacnet.AppendContextUnsigned(badSource, 2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeLifeSafetyOperation(badSource, limits); err == nil {
		t.Fatal("expected source error")
	}
}

func TestLifeSafetyOperationEncodeErrors(t *testing.T) {
	badInst := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: bacnet.MaxObjectInstance + 1}
	if _, err := service.EncodeLifeSafetyOperation(service.LifeSafetyOperationRequest{
		RequestingProcessIdentifier: 1, RequestingSource: "ops", Request: 1, Object: &badInst,
	}); err == nil {
		t.Fatal("expected bad object")
	}
}

func TestVTOpenCloseDataRoundTrip(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	open, err := service.EncodeVTOpen(service.VTOpenRequest{VTClass: 1, LocalVTSessionIdentifier: 3})
	if err != nil || len(open) == 0 {
		t.Fatal(err)
	}
	ackRaw, err := service.EncodeVTOpenACK(service.VTOpenACK{RemoteVTSessionIdentifier: 9})
	if err != nil {
		t.Fatal(err)
	}
	ack, err := service.DecodeVTOpenACK(ackRaw, limits)
	if err != nil || ack.RemoteVTSessionIdentifier != 9 {
		t.Fatalf("ack=%+v err=%v", ack, err)
	}
	closeRaw, err := service.EncodeVTClose(service.VTCloseRequest{RemoteVTSessionIdentifiers: []uint8{9, 10}})
	if err != nil || len(closeRaw) == 0 {
		t.Fatal(err)
	}
	if _, err := service.EncodeVTClose(service.VTCloseRequest{}); err == nil {
		t.Fatal("expected empty VT-Close error")
	}
	dataRaw, err := service.EncodeVTData(service.VTDataRequest{
		VTSessionIdentifier: 9, VTNewData: []byte{1, 2, 3}, VTDataFlag: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	gotData, err := service.DecodeVTData(dataRaw, limits)
	if err != nil || gotData.VTSessionIdentifier != 9 || string(gotData.VTNewData) != "\x01\x02\x03" {
		t.Fatalf("got %+v err=%v", gotData, err)
	}
}

func TestVTDecodeErrors(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	if _, err := service.DecodeVTOpenACK(nil, limits); err == nil {
		t.Fatal("expected VTOpenACK length")
	}
	if _, err := service.DecodeVTOpenACK([]byte{0xff}, limits); err == nil {
		t.Fatal("expected VTOpenACK parse")
	}
	vtBad, err := bacnet.AppendApplicationValue(nil, bacnet.ObjectIDValue(bacnet.ObjectIdentifier{
		Type: bacnet.ObjectTypeDevice, Instance: 1,
	}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeVTOpenACK(vtBad, limits); err == nil {
		t.Fatal("expected VTOpenACK type")
	}
	if _, err := service.DecodeVTData(nil, limits); err == nil {
		t.Fatal("expected VTData length")
	}
	if _, err := service.DecodeVTData([]byte{0xff}, limits); err == nil {
		t.Fatal("expected VTData parse")
	}
	vtDataBad, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	vtDataBad, err = bacnet.AppendApplicationValue(vtDataBad, bacnet.UnsignedValue(2))
	if err != nil {
		t.Fatal(err)
	}
	vtDataBad, err = bacnet.AppendApplicationValue(vtDataBad, bacnet.UnsignedValue(3))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeVTData(vtDataBad, limits); err == nil {
		t.Fatal("expected VTData octet")
	}
	vtDataFlag, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	vtDataFlag, err = bacnet.AppendApplicationValue(vtDataFlag, bacnet.ApplicationValue{
		Kind: bacnet.ValueOctetString, OctetString: []byte{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	vtDataFlag, err = bacnet.AppendApplicationValue(vtDataFlag, bacnet.RealValue(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeVTData(vtDataFlag, limits); err == nil {
		t.Fatal("expected VTData flag type")
	}
	vtDataSID, err := bacnet.AppendApplicationValue(nil, bacnet.RealValue(1))
	if err != nil {
		t.Fatal(err)
	}
	vtDataSID, err = bacnet.AppendApplicationValue(vtDataSID, bacnet.ApplicationValue{
		Kind: bacnet.ValueOctetString, OctetString: []byte{1},
	})
	if err != nil {
		t.Fatal(err)
	}
	vtDataSID, err = bacnet.AppendApplicationValue(vtDataSID, bacnet.UnsignedValue(1))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.DecodeVTData(vtDataSID, limits); err == nil {
		t.Fatal("expected VTData sid type")
	}
}
