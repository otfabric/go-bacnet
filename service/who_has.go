// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// WhoHas is an unconfirmed Who-Has request.
//
// Exactly one of Object or Name must be set. Optional LowLimit/HighLimit bound
// the device-instance search window (both or neither).
type WhoHas struct {
	LowLimit  *uint32
	HighLimit *uint32
	Object    *bacnet.ObjectIdentifier
	Name      *bacnet.CharacterString
}

// IHave is an unconfirmed I-Have announcement.
type IHave struct {
	Device bacnet.ObjectIdentifier
	Object bacnet.ObjectIdentifier
	Name   bacnet.CharacterString
}

// EncodeWhoHas encodes a Who-Has service payload.
func EncodeWhoHas(req WhoHas) ([]byte, error) {
	hasObj := req.Object != nil
	hasName := req.Name != nil
	if hasObj == hasName {
		return nil, fmt.Errorf("%w: Who-Has requires exactly one of objectIdentifier or objectName", bacnet.ErrMalformed)
	}
	if (req.LowLimit == nil) != (req.HighLimit == nil) {
		return nil, fmt.Errorf("%w: Who-Has requires both limits or neither", bacnet.ErrMalformed)
	}
	if req.LowLimit != nil && *req.LowLimit > *req.HighLimit {
		return nil, fmt.Errorf("%w: Who-Has low limit exceeds high limit", bacnet.ErrMalformed)
	}
	var dst []byte
	var err error
	if req.LowLimit != nil {
		dst, err = bacnet.AppendContextUnsigned(dst, 0, uint64(*req.LowLimit))
		if err != nil {
			return nil, err
		}
		dst, err = bacnet.AppendContextUnsigned(dst, 1, uint64(*req.HighLimit))
		if err != nil {
			return nil, err
		}
	}
	if hasObj {
		dst, err = bacnet.AppendContextObjectID(dst, 2, *req.Object)
		return dst, err
	}
	return bacnet.AppendContextCharacterString(dst, 3, *req.Name)
}

// DecodeWhoHas decodes a Who-Has service payload.
func DecodeWhoHas(payload []byte, limits bacnet.DecodeLimits) (WhoHas, error) {
	elements, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return WhoHas{}, err
	}
	if n != len(payload) {
		return WhoHas{}, fmt.Errorf("%w: Who-Has trailing data", bacnet.ErrTrailingData)
	}
	var out WhoHas
	var haveLow, haveHigh, haveObj, haveName bool
	for _, el := range elements {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			if haveLow {
				return WhoHas{}, fmt.Errorf("%w: duplicate Who-Has low limit", bacnet.ErrMalformed)
			}
			v, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return WhoHas{}, err
			}
			if v > 0xFFFFFFFF {
				return WhoHas{}, fmt.Errorf("%w: Who-Has low limit overflow", bacnet.ErrMalformed)
			}
			u := uint32(v)
			out.LowLimit = &u
			haveLow = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			if haveHigh {
				return WhoHas{}, fmt.Errorf("%w: duplicate Who-Has high limit", bacnet.ErrMalformed)
			}
			v, err := bacnet.ContextUnsigned(el)
			if err != nil {
				return WhoHas{}, err
			}
			if v > 0xFFFFFFFF {
				return WhoHas{}, fmt.Errorf("%w: Who-Has high limit overflow", bacnet.ErrMalformed)
			}
			u := uint32(v)
			out.HighLimit = &u
			haveHigh = true
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			if haveObj || haveName {
				return WhoHas{}, fmt.Errorf("%w: Who-Has object choice already set", bacnet.ErrMalformed)
			}
			id, err := bacnet.ContextObjectID(el)
			if err != nil {
				return WhoHas{}, err
			}
			out.Object = &id
			haveObj = true
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			if haveObj || haveName {
				return WhoHas{}, fmt.Errorf("%w: Who-Has object choice already set", bacnet.ErrMalformed)
			}
			name, err := bacnet.ContextCharacterString(el)
			if err != nil {
				return WhoHas{}, err
			}
			out.Name = &name
			haveName = true
		default:
			return WhoHas{}, fmt.Errorf("%w: unexpected Who-Has tag %d", bacnet.ErrMalformed, el.TagNumber)
		}
	}
	if haveLow != haveHigh {
		return WhoHas{}, fmt.Errorf("%w: Who-Has requires both limits or neither", bacnet.ErrMalformed)
	}
	if haveLow && *out.LowLimit > *out.HighLimit {
		return WhoHas{}, fmt.Errorf("%w: Who-Has low limit exceeds high limit", bacnet.ErrMalformed)
	}
	if haveObj == haveName {
		return WhoHas{}, fmt.Errorf("%w: Who-Has requires exactly one of objectIdentifier or objectName", bacnet.ErrMalformed)
	}
	return out, nil
}

// EncodeIHave encodes an I-Have service payload (application tags).
func EncodeIHave(msg IHave) ([]byte, error) {
	var dst []byte
	var err error
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.ObjectIDValue(msg.Device))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.ObjectIDValue(msg.Object))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{
		Kind:      bacnet.ValueCharacterString,
		Character: msg.Name,
	})
	return dst, err
}

// DecodeIHave decodes an I-Have service payload.
func DecodeIHave(payload []byte, limits bacnet.DecodeLimits) (IHave, error) {
	off := 0
	device, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return IHave{}, err
	}
	off += n
	object, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return IHave{}, err
	}
	off += n
	name, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return IHave{}, err
	}
	off += n
	if off != len(payload) {
		return IHave{}, fmt.Errorf("%w: I-Have trailing data", bacnet.ErrTrailingData)
	}
	devID, err := bacnet.AsObjectID(device)
	if err != nil {
		return IHave{}, err
	}
	if devID.Type != bacnet.ObjectTypeDevice {
		return IHave{}, fmt.Errorf("%w: I-Have device is not Device", bacnet.ErrMalformed)
	}
	objID, err := bacnet.AsObjectID(object)
	if err != nil {
		return IHave{}, err
	}
	if name.Kind != bacnet.ValueCharacterString {
		return IHave{}, fmt.Errorf("%w: I-Have objectName must be CharacterString", bacnet.ErrMalformed)
	}
	return IHave{
		Device: devID,
		Object: objID,
		Name:   name.Character,
	}, nil
}

// EncodeWhoHasAPDU builds a complete unconfirmed Who-Has APDU.
func EncodeWhoHasAPDU(req WhoHas) ([]byte, error) {
	payload, err := EncodeWhoHas(req)
	if err != nil {
		return nil, err
	}
	return apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceWhoHas,
		Payload:       payload,
	}), nil
}

// EncodeIHaveAPDU builds a complete unconfirmed I-Have APDU.
func EncodeIHaveAPDU(msg IHave) ([]byte, error) {
	payload, err := EncodeIHave(msg)
	if err != nil {
		return nil, err
	}
	return apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{
		ServiceChoice: apdu.ServiceIHave,
		Payload:       payload,
	}), nil
}

const (
	ServiceWhoHas = apdu.ServiceWhoHas
	ServiceIHave  = apdu.ServiceIHave
)
