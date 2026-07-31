// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// WhoIs is an unconfirmed Who-Is request. Nil limits mean unrestricted.
type WhoIs struct {
	LowLimit  *uint32
	HighLimit *uint32
}

// EncodeWhoIs encodes a Who-Is service payload.
func EncodeWhoIs(req WhoIs) ([]byte, error) {
	if req.LowLimit == nil && req.HighLimit == nil {
		return nil, nil
	}
	if req.LowLimit == nil || req.HighLimit == nil {
		return nil, fmt.Errorf("%w: Who-Is requires both limits or neither", bacnet.ErrMalformed)
	}
	var dst []byte
	var err error
	dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(*req.LowLimit))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*req.HighLimit))
	return dst, err
}

// DecodeWhoIs decodes a Who-Is service payload.
func DecodeWhoIs(payload []byte, limits bacnet.DecodeLimits) (WhoIs, error) {
	if len(payload) == 0 {
		return WhoIs{}, nil
	}
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return WhoIs{}, err
	}
	if n != len(payload) {
		return WhoIs{}, fmt.Errorf("%w: Who-Is trailing data", bacnet.ErrTrailingData)
	}
	var out WhoIs
	var haveLow, haveHigh bool
	for _, el := range elements {
		switch el.TagNumber {
		case 0:
			if haveLow {
				return WhoIs{}, fmt.Errorf("%w: duplicate Who-Is low limit", bacnet.ErrMalformed)
			}
			v, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return WhoIs{}, err
			}
			if v > 0xFFFFFFFF {
				return WhoIs{}, fmt.Errorf("%w: Who-Is low limit overflow", bacnet.ErrMalformed)
			}
			u := uint32(v)
			out.LowLimit = &u
			haveLow = true
		case 1:
			if haveHigh {
				return WhoIs{}, fmt.Errorf("%w: duplicate Who-Is high limit", bacnet.ErrMalformed)
			}
			v, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return WhoIs{}, err
			}
			if v > 0xFFFFFFFF {
				return WhoIs{}, fmt.Errorf("%w: Who-Is high limit overflow", bacnet.ErrMalformed)
			}
			u := uint32(v)
			out.HighLimit = &u
			haveHigh = true
		default:
			return WhoIs{}, fmt.Errorf("%w: unexpected Who-Is tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if haveLow != haveHigh {
		return WhoIs{}, fmt.Errorf("%w: Who-Is requires both limits or neither", bacnet.ErrMalformed)
	}
	if haveLow && *out.LowLimit > *out.HighLimit {
		return WhoIs{}, fmt.Errorf("%w: Who-Is low limit exceeds high limit", bacnet.ErrMalformed)
	}
	return out, nil
}

// IAm is an unconfirmed I-Am request.
type IAm struct {
	Device        bacnet.ObjectIdentifier
	MaxAPDULength uint16
	Segmentation  uint8
	VendorID      uint16
}

// EncodeIAm encodes an I-Am service payload.
func EncodeIAm(msg IAm) ([]byte, error) {
	var dst []byte
	var err error
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.ObjectIDValue(msg.Device))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.UnsignedValue(uint64(msg.MaxAPDULength)))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.EnumValue(uint32(msg.Segmentation)))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.UnsignedValue(uint64(msg.VendorID)))
	return dst, err
}

// DecodeIAm decodes an I-Am service payload.
func DecodeIAm(payload []byte, limits bacnet.DecodeLimits) (IAm, error) {
	off := 0
	device, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return IAm{}, err
	}
	off += n
	maxAPDU, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return IAm{}, err
	}
	off += n
	seg, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return IAm{}, err
	}
	off += n
	vendor, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return IAm{}, err
	}
	off += n
	if off != len(payload) {
		return IAm{}, fmt.Errorf("%w: I-Am trailing data", bacnet.ErrTrailingData)
	}
	id, err := bacnet.AsObjectID(device)
	if err != nil {
		return IAm{}, err
	}
	if id.Type != bacnet.ObjectTypeDevice {
		return IAm{}, fmt.Errorf("%w: I-Am object is not Device", bacnet.ErrMalformed)
	}
	if maxAPDU.Kind != bacnet.ValueUnsigned {
		return IAm{}, fmt.Errorf("%w: I-Am MaxAPDU must be Unsigned", bacnet.ErrMalformed)
	}
	maxU := maxAPDU.Unsigned
	if maxU > 0xFFFF {
		return IAm{}, fmt.Errorf("%w: I-Am MaxAPDU overflow", bacnet.ErrMalformed)
	}
	segU, err := bacnet.AsEnumerated(seg)
	if err != nil {
		return IAm{}, fmt.Errorf("%w: I-Am segmentation must be Enumerated", bacnet.ErrMalformed)
	}
	if segU > 3 {
		return IAm{}, fmt.Errorf("%w: I-Am invalid segmentation %d", bacnet.ErrMalformed, segU)
	}
	if vendor.Kind != bacnet.ValueUnsigned {
		return IAm{}, fmt.Errorf("%w: I-Am vendor ID must be Unsigned", bacnet.ErrMalformed)
	}
	vendorU := vendor.Unsigned
	if vendorU > 0xFFFF {
		return IAm{}, fmt.Errorf("%w: I-Am vendor ID overflow", bacnet.ErrMalformed)
	}
	return IAm{
		Device:        id,
		MaxAPDULength: uint16(maxU),
		Segmentation:  uint8(segU),
		VendorID:      uint16(vendorU),
	}, nil
}

// EncodeWhoIsAPDU builds a complete unconfirmed Who-Is APDU.
func EncodeWhoIsAPDU(req WhoIs) ([]byte, error) {
	payload, err := EncodeWhoIs(req)
	if err != nil {
		return nil, err
	}
	return apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceWhoIs,
		Payload:       payload,
	}), nil
}

// EncodeIAmAPDU builds a complete unconfirmed I-Am APDU.
func EncodeIAmAPDU(msg IAm) ([]byte, error) {
	payload, err := EncodeIAm(msg)
	if err != nil {
		return nil, err
	}
	return apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceIAm,
		Payload:       payload,
	}), nil
}
