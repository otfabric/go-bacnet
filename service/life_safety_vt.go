// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// LifeSafetyOperationRequest is LifeSafetyOperation.
type LifeSafetyOperationRequest struct {
	RequestingProcessIdentifier uint32
	RequestingSource            string
	Request                     uint32 // BACnetLifeSafetyOperation enum
	Object                      *bacnet.ObjectIdentifier
}

// EncodeLifeSafetyOperation encodes LifeSafetyOperation.
//
// requestingSource is a context-primitive CharacterString (ASHRAE [1]
// CharacterString), matching bacnet-stack and BACnet4J.
func EncodeLifeSafetyOperation(req LifeSafetyOperationRequest) ([]byte, error) {
	dst, err := bacnet.AppendContextUnsigned(nil, 0, uint64(req.RequestingProcessIdentifier))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextCharacterString(dst, 1, bacnet.CharacterString{Value: req.RequestingSource})
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 2, uint64(req.Request))
	if err != nil {
		return nil, err
	}
	if req.Object != nil {
		dst, err = bacnet.AppendContextObjectID(dst, 3, *req.Object)
	}
	return dst, err
}

// DecodeLifeSafetyOperation decodes LifeSafetyOperation.
func DecodeLifeSafetyOperation(payload []byte, limits bacnet.DecodeLimits) (LifeSafetyOperationRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return LifeSafetyOperationRequest{}, err
	}
	if n != len(payload) {
		return LifeSafetyOperationRequest{}, fmt.Errorf("%w: LifeSafetyOperation trailing", bacnet.ErrTrailingData)
	}
	var req LifeSafetyOperationRequest
	var havePID, haveSrc, haveReq bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				req.RequestingProcessIdentifier = uint32(u)
				havePID = true
			}
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			cs, e := bacnet.ContextCharacterString(el)
			err = e
			if err == nil {
				req.RequestingSource = cs.Value
				haveSrc = true
			}
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				req.Request = uint32(u)
				haveReq = true
			}
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			id, e := bacnet.ContextObjectID(el)
			err = e
			if err == nil {
				req.Object = &id
			}
		default:
			return LifeSafetyOperationRequest{}, fmt.Errorf("%w: LifeSafetyOperation tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
		if err != nil {
			return LifeSafetyOperationRequest{}, err
		}
	}
	if !havePID || !haveSrc || !haveReq {
		return LifeSafetyOperationRequest{}, fmt.Errorf("%w: LifeSafetyOperation missing fields", bacnet.ErrMalformed)
	}
	return req, nil
}

// VTOpenRequest is VT-Open.
type VTOpenRequest struct {
	VTClass                  uint32
	LocalVTSessionIdentifier uint8
}

// EncodeVTOpen encodes VT-Open.
func EncodeVTOpen(req VTOpenRequest) ([]byte, error) {
	dst, err := bacnet.AppendApplicationValue(nil, bacnet.EnumValue(req.VTClass))
	if err != nil {
		return nil, err
	}
	return bacnet.AppendApplicationValue(dst, bacnet.UnsignedValue(uint64(req.LocalVTSessionIdentifier)))
}

// VTOpenACK is VT-Open ComplexACK.
type VTOpenACK struct {
	RemoteVTSessionIdentifier uint8
}

// DecodeVTOpenACK decodes VT-Open ACK.
func DecodeVTOpenACK(payload []byte, limits bacnet.DecodeLimits) (VTOpenACK, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return VTOpenACK{}, err
	}
	if n != len(payload) {
		return VTOpenACK{}, fmt.Errorf("%w: VTOpenACK trailing", bacnet.ErrTrailingData)
	}
	if len(els) != 1 {
		return VTOpenACK{}, fmt.Errorf("%w: VTOpenACK length", bacnet.ErrMalformed)
	}
	u, err := bacnet.AsUnsigned(els[0].Value)
	if err != nil {
		return VTOpenACK{}, err
	}
	return VTOpenACK{RemoteVTSessionIdentifier: uint8(u)}, nil
}

// EncodeVTOpenACK encodes VT-Open ACK.
func EncodeVTOpenACK(ack VTOpenACK) ([]byte, error) {
	return bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(uint64(ack.RemoteVTSessionIdentifier)))
}

// VTCloseRequest is VT-Close (list of remote session identifiers).
type VTCloseRequest struct {
	RemoteVTSessionIdentifiers []uint8
}

// EncodeVTClose encodes VT-Close.
func EncodeVTClose(req VTCloseRequest) ([]byte, error) {
	if len(req.RemoteVTSessionIdentifiers) == 0 {
		return nil, fmt.Errorf("%w: VT-Close requires sessions", bacnet.ErrMalformed)
	}
	var dst []byte
	var err error
	for _, id := range req.RemoteVTSessionIdentifiers {
		dst, err = bacnet.AppendApplicationValue(dst, bacnet.UnsignedValue(uint64(id)))
		if err != nil {
			return nil, err
		}
	}
	return dst, nil
}

// VTDataRequest is VT-Data.
type VTDataRequest struct {
	VTSessionIdentifier uint8
	VTNewData           []byte
	VTDataFlag          uint8
}

// EncodeVTData encodes VT-Data.
func EncodeVTData(req VTDataRequest) ([]byte, error) {
	dst, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(uint64(req.VTSessionIdentifier)))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{
		Kind: bacnet.ValueOctetString, OctetString: req.VTNewData,
	})
	if err != nil {
		return nil, err
	}
	return bacnet.AppendApplicationValue(dst, bacnet.UnsignedValue(uint64(req.VTDataFlag)))
}

// DecodeVTData decodes VT-Data.
func DecodeVTData(payload []byte, limits bacnet.DecodeLimits) (VTDataRequest, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return VTDataRequest{}, err
	}
	if n != len(payload) {
		return VTDataRequest{}, fmt.Errorf("%w: VTData trailing", bacnet.ErrTrailingData)
	}
	if len(els) != 3 {
		return VTDataRequest{}, fmt.Errorf("%w: VTData length", bacnet.ErrMalformed)
	}
	sid, err := bacnet.AsUnsigned(els[0].Value)
	if err != nil {
		return VTDataRequest{}, err
	}
	if els[1].Value.Kind != bacnet.ValueOctetString {
		return VTDataRequest{}, fmt.Errorf("%w: VTNewData", bacnet.ErrMalformed)
	}
	flag, err := bacnet.AsUnsigned(els[2].Value)
	if err != nil {
		return VTDataRequest{}, err
	}
	return VTDataRequest{
		VTSessionIdentifier: uint8(sid),
		VTNewData:           append([]byte(nil), els[1].Value.OctetString...),
		VTDataFlag:          uint8(flag),
	}, nil
}
