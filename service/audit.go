// SPDX-License-Identifier: MIT

package service

import (
	"fmt"

	"github.com/otfabric/go-bacnet"
)

// AuditNotification is Confirmed/UnconfirmedAuditNotification (opaque-friendly).
//
// Typed fields cover the common header; Remaining holds undecoded trailing elements.
type AuditNotification struct {
	SourceDevice bacnet.ObjectIdentifier
	TargetDevice *bacnet.ObjectIdentifier
	SourceObject *bacnet.ObjectIdentifier
	TargetObject *bacnet.ObjectIdentifier
	Operation    uint32
	TimeStamp    *DateTime
	Remaining    []bacnet.Element
}

// EncodeAuditNotification encodes a minimal AuditNotification.
func EncodeAuditNotification(n AuditNotification) ([]byte, error) {
	dst, err := bacnet.AppendContextObjectID(nil, 0, n.SourceDevice)
	if err != nil {
		return nil, err
	}
	if n.TargetDevice != nil {
		dst, err = bacnet.AppendContextObjectID(dst, 1, *n.TargetDevice)
		if err != nil {
			return nil, err
		}
	}
	dst, err = bacnet.AppendContextUnsigned(dst, 4, uint64(n.Operation))
	if err != nil {
		return nil, err
	}
	// Remaining is decode-only; encode ignores unknown trailing fields.
	return dst, nil
}

// DecodeAuditNotification decodes AuditNotification, retaining unknown tags in Remaining.
func DecodeAuditNotification(payload []byte, limits bacnet.DecodeLimits) (AuditNotification, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AuditNotification{}, err
	}
	if n != len(payload) {
		return AuditNotification{}, fmt.Errorf("%w: AuditNotification trailing", bacnet.ErrTrailingData)
	}
	var note AuditNotification
	var haveSrc, haveOp bool
	for _, el := range els {
		switch {
		case el.TagNumber == 0 && !bacnet.IsContextConstructed(el):
			note.SourceDevice, err = bacnet.ContextObjectID(el)
			haveSrc = true
		case el.TagNumber == 1 && !bacnet.IsContextConstructed(el):
			id, e := bacnet.ContextObjectID(el)
			err = e
			if err == nil {
				note.TargetDevice = &id
			}
		case el.TagNumber == 2 && !bacnet.IsContextConstructed(el):
			id, e := bacnet.ContextObjectID(el)
			err = e
			if err == nil {
				note.SourceObject = &id
			}
		case el.TagNumber == 3 && !bacnet.IsContextConstructed(el):
			id, e := bacnet.ContextObjectID(el)
			err = e
			if err == nil {
				note.TargetObject = &id
			}
		case el.TagNumber == 4 && !bacnet.IsContextConstructed(el):
			u, e := bacnet.ContextUnsigned(el)
			err = e
			if err == nil {
				note.Operation = uint32(u)
				haveOp = true
			}
		default:
			note.Remaining = append(note.Remaining, el)
		}
		if err != nil {
			return AuditNotification{}, err
		}
	}
	if !haveSrc || !haveOp {
		return AuditNotification{}, fmt.Errorf("%w: AuditNotification missing fields", bacnet.ErrMalformed)
	}
	return note, nil
}

// AuditLogQueryRequest is a minimal AuditLogQuery confirmed request.
type AuditLogQueryRequest struct {
	AuditLog bacnet.ObjectIdentifier
	Query    []bacnet.Element // opaque query parameters
}

// EncodeAuditLogQuery encodes AuditLogQuery.
func EncodeAuditLogQuery(req AuditLogQueryRequest) ([]byte, error) {
	dst, err := bacnet.AppendContextObjectID(nil, 0, req.AuditLog)
	if err != nil {
		return nil, err
	}
	if len(req.Query) == 0 {
		return dst, nil
	}
	return bacnet.AppendContextTagged(dst, 1, req.Query)
}

// AuditLogQueryACK is a ComplexACK with opaque result elements.
type AuditLogQueryACK struct {
	Records []bacnet.Element
}

// DecodeAuditLogQueryACK decodes AuditLogQuery ACK as opaque elements.
func DecodeAuditLogQueryACK(payload []byte, limits bacnet.DecodeLimits) (AuditLogQueryACK, error) {
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AuditLogQueryACK{}, err
	}
	if n != len(payload) {
		return AuditLogQueryACK{}, fmt.Errorf("%w: AuditLogQueryACK trailing", bacnet.ErrTrailingData)
	}
	return AuditLogQueryACK{Records: cloneElements(els)}, nil
}

// WhoAmI is an Unconfirmed Who-Am-I request.
//
// Wire form matches ASHRAE 135 Who-Am-I-Request SEQUENCE without context tags
// (application Unsigned16 + CharacterString + CharacterString), same as I-Am.
type WhoAmI struct {
	VendorID     uint16
	ModelName    string
	SerialNumber string
}

// EncodeWhoAmI encodes Who-Am-I.
func EncodeWhoAmI(w WhoAmI) ([]byte, error) {
	dst, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(uint64(w.VendorID)))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: w.ModelName},
	})
	if err != nil {
		return nil, err
	}
	return bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: w.SerialNumber},
	})
}

// DecodeWhoAmI decodes Who-Am-I.
func DecodeWhoAmI(payload []byte, limits bacnet.DecodeLimits) (WhoAmI, error) {
	off := 0
	vendor, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return WhoAmI{}, err
	}
	off += n
	model, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return WhoAmI{}, err
	}
	off += n
	serial, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return WhoAmI{}, err
	}
	off += n
	if off != len(payload) {
		return WhoAmI{}, fmt.Errorf("%w: WhoAmI trailing", bacnet.ErrTrailingData)
	}
	if vendor.Kind != bacnet.ValueUnsigned || vendor.Unsigned > 0xffff {
		return WhoAmI{}, fmt.Errorf("%w: WhoAmI vendor-id", bacnet.ErrMalformed)
	}
	if model.Kind != bacnet.ValueCharacterString || serial.Kind != bacnet.ValueCharacterString {
		return WhoAmI{}, fmt.Errorf("%w: WhoAmI character strings", bacnet.ErrMalformed)
	}
	return WhoAmI{
		VendorID:     uint16(vendor.Unsigned),
		ModelName:    model.Character.Value,
		SerialNumber: serial.Character.Value,
	}, nil
}

// YouAre is an Unconfirmed You-Are request.
//
// Wire form matches ASHRAE 135 You-Are-Request SEQUENCE without context tags
// (application Unsigned16 + CharacterString + CharacterString + optional
// Device ObjectIdentifier).
type YouAre struct {
	VendorID     uint16
	ModelName    string
	SerialNumber string
	Device       bacnet.ObjectIdentifier
}

// EncodeYouAre encodes You-Are.
func EncodeYouAre(y YouAre) ([]byte, error) {
	dst, err := bacnet.AppendApplicationValue(nil, bacnet.UnsignedValue(uint64(y.VendorID)))
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: y.ModelName},
	})
	if err != nil {
		return nil, err
	}
	dst, err = bacnet.AppendApplicationValue(dst, bacnet.ApplicationValue{
		Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Value: y.SerialNumber},
	})
	if err != nil {
		return nil, err
	}
	return bacnet.AppendApplicationValue(dst, bacnet.ObjectIDValue(y.Device))
}

// DecodeYouAre decodes You-Are.
func DecodeYouAre(payload []byte, limits bacnet.DecodeLimits) (YouAre, error) {
	off := 0
	vendor, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return YouAre{}, err
	}
	off += n
	model, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return YouAre{}, err
	}
	off += n
	serial, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
	if err != nil {
		return YouAre{}, err
	}
	off += n
	if vendor.Kind != bacnet.ValueUnsigned || vendor.Unsigned > 0xffff {
		return YouAre{}, fmt.Errorf("%w: YouAre vendor-id", bacnet.ErrMalformed)
	}
	if model.Kind != bacnet.ValueCharacterString || serial.Kind != bacnet.ValueCharacterString {
		return YouAre{}, fmt.Errorf("%w: YouAre character strings", bacnet.ErrMalformed)
	}
	y := YouAre{
		VendorID:     uint16(vendor.Unsigned),
		ModelName:    model.Character.Value,
		SerialNumber: serial.Character.Value,
	}
	if off < len(payload) {
		device, n, err := bacnet.ParseApplicationValue(payload[off:], limits)
		if err != nil {
			return YouAre{}, err
		}
		off += n
		id, err := bacnet.AsObjectID(device)
		if err != nil {
			return YouAre{}, fmt.Errorf("%w: YouAre device", bacnet.ErrMalformed)
		}
		y.Device = id
	}
	if off != len(payload) {
		// Optional MAC OctetString and any further trailing data: reject extras
		// we do not model yet so corpus fixtures stay deterministic.
		return YouAre{}, fmt.Errorf("%w: YouAre trailing", bacnet.ErrTrailingData)
	}
	return y, nil
}

// AuthRequest is a confirmed AuthRequest (opaque parameters preserved).
type AuthRequest struct {
	Parameters []bacnet.Element
}

// EncodeAuthRequest encodes AuthRequest as a constructed context [0] list when non-empty.
func EncodeAuthRequest(r AuthRequest) ([]byte, error) {
	if len(r.Parameters) == 0 {
		return nil, nil
	}
	return bacnet.AppendContextTagged(nil, 0, r.Parameters)
}

// DecodeAuthRequest decodes AuthRequest.
func DecodeAuthRequest(payload []byte, limits bacnet.DecodeLimits) (AuthRequest, error) {
	if len(payload) == 0 {
		return AuthRequest{}, nil
	}
	els, n, err := bacnet.ParseSequence(payload, limits, -1)
	if err != nil {
		return AuthRequest{}, err
	}
	if n != len(payload) {
		return AuthRequest{}, fmt.Errorf("%w: AuthRequest trailing", bacnet.ErrTrailingData)
	}
	return AuthRequest{Parameters: cloneElements(els)}, nil
}
