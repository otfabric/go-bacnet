// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"errors"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

func isSpecialProperty(id bacnet.PropertyIdentifier) bool {
	return id == bacnet.PropertyAll || id == bacnet.PropertyRequired || id == bacnet.PropertyOptional
}

// readProperty serves ReadProperty (clause 15.5).
func (s *Server) readProperty(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeReadProperty(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	o, ok := s.dev.Object(r.Object)
	if !ok {
		return fail(object.ErrUnknownObject)
	}
	if isSpecialProperty(r.Property.Identifier) {
		// ALL, REQUIRED and OPTIONAL are ReadPropertyMultiple selectors.
		return fail(object.ErrUnknownProperty)
	}
	v, err := o.ReadProperty(ctx, r.Property)
	if err != nil {
		return fail(err)
	}
	payload, err := service.EncodeReadPropertyACK(service.ReadPropertyACK{Object: o.Identifier(), Property: r.Property, Value: v})
	if err != nil {
		return fail(err)
	}
	return complexACK(payload)
}

// readPropertyMultiple serves ReadPropertyMultiple (clause 15.7). Every
// property gets a value or an access error; ALL, REQUIRED and OPTIONAL
// expand to the object's properties.
//
// The reply is encoded while the results are produced, so that a request
// whose answer cannot fit the requester's buffer stops as soon as the
// encoded size passes the reply budget: it is answered with Abort
// segmentation-not-supported when the reply cannot be segmented, else
// buffer-overflow, as finish answers a reply that is too large.
func (s *Server) readPropertyMultiple(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	specs, err := service.DecodeReadPropertyMultiple(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	enc := rpmEncoder{budget: s.responseBudget(req)}
	for _, spec := range specs {
		o, ok := s.dev.Object(spec.Object)
		if !ok {
			if err := enc.begin(spec.Object); err != nil {
				return fail(err)
			}
			for _, ref := range spec.Properties {
				if err := enc.add(spec.Object, service.PropertyResult{Property: ref, Err: object.AsError(object.ErrUnknownObject)}); err != nil {
					return s.rpmFailure(req, err)
				}
			}
			enc.end()
			continue
		}
		id := o.Identifier()
		if err := enc.begin(id); err != nil {
			return fail(err)
		}
		for _, ref := range spec.Properties {
			if isSpecialProperty(ref.Identifier) {
				all, required := o.Properties()
				for _, pid := range all {
					switch ref.Identifier {
					case bacnet.PropertyRequired:
						if !required[pid] {
							continue
						}
					case bacnet.PropertyOptional:
						if required[pid] {
							continue
						}
					default: // ALL
					}
					if err := enc.add(id, readOne(ctx, o, bacnet.PropertyReference{Identifier: pid})); err != nil {
						return s.rpmFailure(req, err)
					}
				}
				continue
			}
			if err := enc.add(id, readOne(ctx, o, ref)); err != nil {
				return s.rpmFailure(req, err)
			}
		}
		enc.end()
	}
	if len(enc.payload) > enc.budget {
		return s.rpmFailure(req, errReplyTooLarge)
	}
	return complexACK(enc.payload)
}

var errReplyTooLarge = errors.New("server: reply exceeds the requester's buffer")

// rpmFailure answers a ReadPropertyMultiple that could not be encoded.
func (s *Server) rpmFailure(req *apdu.ConfirmedRequest, err error) result {
	if !errors.Is(err, errReplyTooLarge) {
		return fail(err)
	}
	if !req.SegmentedResponseAccepted || !s.cfg.segmentation {
		return fail(&abortError{bacnet.AbortReasonSegmentationNotSupported})
	}
	return fail(&abortError{bacnet.AbortReasonBufferOverflow})
}

// rpmEncoder builds a ReadPropertyMultiple-ACK one property result at a
// time and stops at the reply budget.
type rpmEncoder struct {
	budget  int
	payload []byte
	head    int // length of the open ReadAccessResult header
}

// begin opens the ReadAccessResult of obj: its identifier and the opening
// tag of listOfResults.
func (e *rpmEncoder) begin(obj bacnet.ObjectIdentifier) error {
	b, err := service.EncodeReadPropertyMultipleACK([]service.ReadAccessResult{{Object: obj}})
	if err != nil {
		return err
	}
	e.head = len(b) - 1 // without the closing tag
	e.payload = append(e.payload, b[:e.head]...)
	return e.check(1)
}

// add appends one property result to the open ReadAccessResult.
func (e *rpmEncoder) add(obj bacnet.ObjectIdentifier, pr service.PropertyResult) error {
	b, err := service.EncodeReadPropertyMultipleACK([]service.ReadAccessResult{{Object: obj, Properties: []service.PropertyResult{pr}}})
	if err != nil {
		return err
	}
	e.payload = append(e.payload, b[e.head:len(b)-1]...)
	return e.check(1)
}

// end closes the open ReadAccessResult.
func (e *rpmEncoder) end() { e.payload = append(e.payload, 0x1F) }

// check reports errReplyTooLarge once the payload, with pending more
// octets still to come, passes the budget.
func (e *rpmEncoder) check(pending int) error {
	if len(e.payload)+pending > e.budget {
		return errReplyTooLarge
	}
	return nil
}

func readOne(ctx context.Context, o object.Object, ref bacnet.PropertyReference) service.PropertyResult {
	v, err := o.ReadProperty(ctx, ref)
	if err != nil {
		return service.PropertyResult{Property: ref, Err: object.AsError(err)}
	}
	return service.PropertyResult{Property: ref, Value: v}
}

// write applies one write, through the application's BeforeWrite hook.
func (s *Server) write(ctx context.Context, id bacnet.ObjectIdentifier, w object.Write) error {
	o, ok := s.dev.Object(id)
	if !ok {
		return object.ErrUnknownObject
	}
	if h := s.cfg.hooks.BeforeWrite; h != nil {
		if err := h(ctx, o.Identifier(), w); err != nil {
			return err
		}
	}
	return o.WriteProperty(ctx, w)
}

// writeProperty serves WriteProperty (clause 15.9).
func (s *Server) writeProperty(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeWriteProperty(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	w := object.Write{Property: r.Property, Value: r.Value}
	if r.Priority != nil {
		w.Priority = *r.Priority
	}
	if err := s.write(ctx, r.Object, w); err != nil {
		return fail(err)
	}
	return ack()
}

// writePropertyMultiple serves WritePropertyMultiple (clause 15.10): the
// writes are applied in order and the first failure ends the request; the
// writes before it stay applied, and the Error names the one that failed.
func (s *Server) writePropertyMultiple(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	specs, err := service.DecodeWritePropertyMultiple(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	for _, spec := range specs {
		for _, pv := range spec.Properties {
			w := object.Write{Property: pv.Property, Value: pv.Value}
			if pv.Priority != nil {
				w.Priority = *pv.Priority
			}
			if err := s.write(ctx, spec.Object, w); err != nil {
				e := object.AsError(err)
				return fail(&service.WritePropertyMultipleError{Class: e.Class, Code: e.Code, FirstFailed: spec.Object, FirstProperty: pv.Property})
			}
		}
	}
	return ack()
}

// whoIs serves Who-Is (clause 16.10): an I-Am when the device instance is
// in the range, or when the request has none. A broadcast Who-Is gets a
// broadcast I-Am; a unicast one a unicast I-Am to the requester (Addendum
// 135-2004q, as bacnet-stack's handler_who_is_unicast does).
func (s *Server) whoIs(_ context.Context, req *apdu.UnconfirmedRequest, p peer) {
	r, err := service.DecodeWhoIs(req.Payload, s.limits)
	if err != nil {
		return
	}
	inst := s.dev.Identifier().Instance
	if r.LowLimit != nil && r.HighLimit != nil && (inst < *r.LowLimit || inst > *r.HighLimit) {
		return
	}
	_ = s.sendIAm(&p)
}

// IAm broadcasts an I-Am for the device (at start-up, for example). It is
// not sent while DeviceCommunicationControl disables initiation.
func (s *Server) IAm() error {
	if !s.dcc.mayInitiate(s.clock.Now()) {
		return nil
	}
	return s.sendIAm(nil)
}

func (s *Server) sendIAm(p *peer) error {
	seg := uint8(3)
	if s.cfg.segmentation {
		seg = 0
	}
	payload, err := service.EncodeIAm(service.IAm{
		Device: s.dev.Identifier(), MaxAPDULength: s.cfg.maxAPDU, Segmentation: seg, VendorID: s.dev.Config().VendorID,
	})
	if err != nil {
		return err
	}
	frame := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceIAm, Payload: payload})
	if p != nil && !p.broadcast {
		return s.reply(*p, frame)
	}
	return s.broadcastAPDU(p, frame)
}

// whoHas serves Who-Has (clause 16.9). The I-Have follows the rule of
// Who-Is: broadcast for a broadcast request, unicast for a unicast one.
func (s *Server) whoHas(_ context.Context, req *apdu.UnconfirmedRequest, p peer) {
	r, err := service.DecodeWhoHas(req.Payload, s.limits)
	if err != nil {
		return
	}
	inst := s.dev.Identifier().Instance
	if r.LowLimit != nil && r.HighLimit != nil && (inst < *r.LowLimit || inst > *r.HighLimit) {
		return
	}
	var o object.Object
	var ok bool
	switch {
	case r.Object != nil:
		o, ok = s.dev.Object(*r.Object)
	case r.Name != nil:
		o, ok = s.dev.ObjectByName(r.Name.Value)
	}
	if !ok {
		return
	}
	payload, err := service.EncodeIHave(service.IHave{
		Device: s.dev.Identifier(), Object: o.Identifier(), Name: bacnet.CharacterString{Value: o.Name()},
	})
	if err != nil {
		return
	}
	frame := apdu.AppendUnconfirmedRequest(nil, apdu.UnconfirmedRequest{ServiceChoice: apdu.ServiceIHave, Payload: payload})
	if !p.broadcast {
		_ = s.reply(p, frame)
		return
	}
	_ = s.broadcastAPDU(&p, frame)
}
