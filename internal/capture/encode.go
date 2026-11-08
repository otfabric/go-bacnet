// SPDX-License-Identifier: MIT

package capture

import (
	"bytes"
	"fmt"

	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/npdu"
	"github.com/otfabric/go-bacnet/service"
)

// Reencode encodes a decoded message again with go-bacnet's encoders, layer
// by layer, and reports the first layer whose bytes differ from the
// recording. Layer "" means every layer go-bacnet can encode was equal;
// encodable reports whether the service had an encoder at all.
func Reencode(m Message) (layer Layer, encodable bool, err error) {
	if m.Err != nil {
		return "", false, m.Err
	}
	b, err := bvlc.Append(nil, m.BVLC)
	if err != nil {
		return LayerBVLC, false, err
	}
	if !bytes.Equal(b, m.Data) {
		return LayerBVLC, false, fmt.Errorf("bvlc: % x, recorded % x", b, m.Data)
	}
	if m.NPDU == nil {
		return "", true, nil
	}
	n, err := npdu.Append(nil, *m.NPDU)
	if err != nil {
		return LayerNPDU, false, err
	}
	if !bytes.Equal(n, m.BVLC.Payload) {
		return LayerNPDU, false, fmt.Errorf("npdu: % x, recorded % x", n, m.BVLC.Payload)
	}
	if m.APDU == nil {
		return "", true, nil
	}
	a := appendAPDU(*m.APDU)
	if !bytes.Equal(a, m.NPDU.APDU) {
		return LayerAPDU, false, fmt.Errorf("apdu: % x, recorded % x", a, m.NPDU.APDU)
	}
	if m.Service == nil {
		return "", false, nil
	}
	enc, ok := encoders(m.Service)
	if !ok {
		return "", false, nil
	}
	p, err := enc()
	if err != nil {
		return LayerService, true, err
	}
	if !bytes.Equal(p, m.Payload) {
		return LayerService, true, fmt.Errorf("service %T: % x, recorded % x", m.Service, p, m.Payload)
	}
	return "", true, nil
}

func appendAPDU(p apdu.PDU) []byte {
	switch p.Type {
	case apdu.TypeConfirmedRequest:
		return apdu.AppendConfirmedRequest(nil, *p.ConfirmedRequest)
	case apdu.TypeUnconfirmedRequest:
		return apdu.AppendUnconfirmedRequest(nil, *p.UnconfirmedRequest)
	case apdu.TypeSimpleACK:
		return apdu.AppendSimpleACK(nil, *p.SimpleACK)
	case apdu.TypeComplexACK:
		return apdu.AppendComplexACK(nil, *p.ComplexACK)
	case apdu.TypeSegmentACK:
		return apdu.AppendSegmentACK(nil, *p.SegmentACK)
	case apdu.TypeError:
		return apdu.AppendError(nil, *p.Error)
	case apdu.TypeReject:
		return apdu.AppendReject(nil, *p.Reject)
	case apdu.TypeAbort:
		return apdu.AppendAbort(nil, *p.Abort)
	}
	return nil
}

// encoders returns the encoder of a decoded service value.
func encoders(v any) (func() ([]byte, error), bool) {
	switch v := v.(type) {
	case service.ReadPropertyRequest:
		return func() ([]byte, error) { return service.EncodeReadProperty(v) }, true
	case service.ReadPropertyACK:
		return func() ([]byte, error) { return service.EncodeReadPropertyACK(v) }, true
	case []service.ReadAccessSpecification:
		return func() ([]byte, error) { return service.EncodeReadPropertyMultiple(v) }, true
	case []service.ReadAccessResult:
		return func() ([]byte, error) { return service.EncodeReadPropertyMultipleACK(v) }, true
	case service.WritePropertyRequest:
		return func() ([]byte, error) { return service.EncodeWriteProperty(v) }, true
	case []service.WriteAccessSpecification:
		return func() ([]byte, error) { return service.EncodeWritePropertyMultiple(v) }, true
	case service.WhoIs:
		return func() ([]byte, error) { return service.EncodeWhoIs(v) }, true
	case service.IAm:
		return func() ([]byte, error) { return service.EncodeIAm(v) }, true
	case service.WhoHas:
		return func() ([]byte, error) { return service.EncodeWhoHas(v) }, true
	case service.IHave:
		return func() ([]byte, error) { return service.EncodeIHave(v) }, true
	case service.SubscribeCOVRequest:
		return func() ([]byte, error) { return service.EncodeSubscribeCOV(v) }, true
	case service.COVNotification:
		return func() ([]byte, error) { return service.EncodeCOVNotification(v) }, true
	case service.ReadRangeRequest:
		return func() ([]byte, error) { return service.EncodeReadRange(v) }, true
	case service.ReadRangeACK:
		return func() ([]byte, error) { return service.EncodeReadRangeACK(v) }, true
	case service.AtomicReadFileRequest:
		return func() ([]byte, error) { return service.EncodeAtomicReadFile(v) }, true
	case service.AtomicReadFileACK:
		return func() ([]byte, error) { return service.EncodeAtomicReadFileACK(v) }, true
	case service.AtomicWriteFileRequest:
		return func() ([]byte, error) { return service.EncodeAtomicWriteFile(v) }, true
	case service.AtomicWriteFileACK:
		return func() ([]byte, error) { return service.EncodeAtomicWriteFileACK(v) }, true
	case service.TimeSynchronization:
		return func() ([]byte, error) { return service.EncodeTimeSynchronization(v) }, true
	case service.ReinitializeDeviceRequest:
		return func() ([]byte, error) { return service.EncodeReinitializeDevice(v) }, true
	case service.DeviceCommunicationControlRequest:
		return func() ([]byte, error) { return service.EncodeDeviceCommunicationControl(v) }, true
	case service.PrivateTransfer:
		return func() ([]byte, error) { return service.EncodePrivateTransfer(v) }, true
	case service.EventNotification:
		return func() ([]byte, error) { return service.EncodeEventNotification(v) }, true
	case service.TextMessage:
		return func() ([]byte, error) { return service.EncodeTextMessage(v) }, true
	}
	return nil, false
}
