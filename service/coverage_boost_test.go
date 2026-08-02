// SPDX-License-Identifier: MIT

package service

import (
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestCoverageBoostDecodeBranches(t *testing.T) {
	// Floating limit wrong tag numbers / bad reals
	if _, err := decodeFloatingLimit([]bacnet.Element{
		{Context: true, TagNumber: 9},
	}); err == nil {
		t.Fatal("fl")
	}
	if _, err := decodeCommandFailure([]bacnet.Element{
		{}, {}, {Context: true, TagNumber: 1}, // bad flags mid
	}); err == nil {
		t.Fatal("cf")
	}
	if _, err := decodeUnsignedRange([]bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{1}}},
		{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{0x04, 0x00}}},
		{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueReal}},
	}); err == nil {
		t.Fatal("ur")
	}
	// Buffer ready with unsigned parse errors via empty context
	if _, err := decodeBufferReady([]bacnet.Element{
		{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext}},
	}); err == nil {
		t.Fatal("br")
	}
	if _, err := decodeBufferReady([]bacnet.Element{
		{Context: true, TagNumber: 2, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext}},
	}); err == nil {
		t.Fatal("br2")
	}
	got, err := decodeBufferReady([]bacnet.Element{
		{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}},
	})
	if err != nil || len(got.BufferProperty) != 1 {
		t.Fatalf("%v %+v", err, got)
	}

	// File stream/record field errors
	var ack AtomicReadFileACK
	if err := decodeFileStreamResult(nil, &ack); err == nil {
		t.Fatal("stream nil")
	}
	if err := decodeFileStreamResult([]bacnet.Element{{}, {}}, &ack); err == nil {
		t.Fatal("stream kinds")
	}
	if err := decodeFileRecordResult([]bacnet.Element{{Value: bacnet.SignedValue(1)}, {Value: bacnet.RealValue(1)}}, &ack); err == nil {
		t.Fatal("record count")
	}
	if err := decodeFileRecordResult([]bacnet.Element{
		{Value: bacnet.SignedValue(1)}, {Value: bacnet.UnsignedValue(1)}, {Value: bacnet.RealValue(1)},
	}, &ack); err == nil {
		t.Fatal("record data")
	}

	// parseContextUnsignedElement success already covered; force parse failure via huge path is hard.
	if _, err := parseContextUnsignedElement(0, 1); err != nil {
		t.Fatal(err)
	}

	// Life safety / VT / messaging / list unexpected tags
	if _, err := DecodeLifeSafetyOperation([]byte{0x09, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("life tag")
	}
	if _, err := DecodeVTData([]byte{0x21, 0x01, 0x21, 0x02}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("vt len")
	}
	if _, err := DecodePrivateTransfer([]byte{0x09, 0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("pt")
	}
	if _, err := DecodeTextMessage([]byte{0x09, 0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("tm")
	}
	if _, err := DecodeWriteGroup([]byte{0x09, 0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("wg")
	}
	if _, err := DecodeListElementRequest([]byte{0x09, 0xff}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("list")
	}
	if _, err := DecodeAtomicReadFileACK([]byte{0x21, 0x01}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("file ack")
	}
	if _, err := DecodeAtomicWriteFileACK([]byte{0x2e, 0x2f}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("wack empty")
	}
	if _, err := DecodeCreateObjectACK([]byte{0x21, 0x01, 0x21, 0x02}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("create ack len")
	}
	if _, err := DecodeDeleteObject([]byte{0x21, 0x01, 0x21, 0x02}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("delete len")
	}
	if _, err := DecodeVTOpenACK(nil, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("vtopen empty")
	}
	if _, err := DecodeVTOpenACK([]byte{0x44, 0x00, 0x00, 0x00, 0x00}, bacnet.DefaultDecodeLimits()); err == nil {
		t.Fatal("vtopen wrong type")
	}
}

func TestCoverageBoostEncodeNPErrors(t *testing.T) {
	// Empty buffer-ready still encodes counts.
	els, err := EncodeNotificationParameters(NotificationParameters{BufferReady: &BufferReadyParams{}})
	if err != nil || len(els) != 1 {
		t.Fatalf("%v %v", err, els)
	}
	got, err := DecodeNotificationParameters(els)
	if err != nil || got.BufferReady == nil {
		t.Fatalf("%v %+v", err, got)
	}
	els, err = EncodeNotificationParameters(NotificationParameters{FloatingLimit: &FloatingLimitParams{
		StatusFlags: bacnet.BitString{UnusedBits: 4, Bytes: []byte{0}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeNotificationParameters(els); err != nil {
		t.Fatal(err)
	}
}
