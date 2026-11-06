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

// changeList serves AddListElement and RemoveListElement (clauses 15.1,
// 15.2). A failure names the first element that failed in a
// ChangeList-Error.
func (s *Server) changeList(ctx context.Context, req *apdu.ConfirmedRequest, add bool) result {
	r, err := service.DecodeListElementRequest(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	o, ok := s.dev.Object(r.Object)
	if !ok {
		return fail(listFailure(object.ErrUnknownObject, 0))
	}
	lm, ok := o.(object.ListModifier)
	if !ok {
		// bacnet-stack answers property-is-not-a-list for an object
		// without list support (h_list_element.c); an unknown property
		// keeps its own error.
		if _, err := o.ReadProperty(ctx, bacnet.PropertyReference{Identifier: r.Property.Identifier}); err != nil {
			return fail(listFailure(err, 0))
		}
		return fail(listFailure(object.ErrPropertyIsNotAList, 0))
	}
	if h := s.cfg.hooks.BeforeWrite; h != nil {
		if err := h(ctx, o.Identifier(), object.Write{Property: r.Property, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: r.Elements}}); err != nil {
			return fail(listFailure(err, 0))
		}
	}
	if add {
		err = lm.AddListElements(ctx, r.Property, r.Elements)
	} else {
		err = lm.RemoveListElements(ctx, r.Property, r.Elements)
	}
	if err != nil {
		var le *object.ListError
		if errors.As(err, &le) {
			return fail(listFailure(le.Err, le.First))
		}
		return fail(listFailure(err, 0))
	}
	return ack()
}

func listFailure(err error, first uint32) error {
	e := object.AsError(err)
	return &createObjectError{class: e.Class, code: e.Code, first: first}
}

func (s *Server) addListElement(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	return s.changeList(ctx, req, true)
}

func (s *Server) removeListElement(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	return s.changeList(ctx, req, false)
}

// createObject serves CreateObject (clause 15.3) with the application's
// factories. Initial values are written in order; when one fails the
// object is removed again and the error names that value, as bacnet-stack
// does (create_object.c).
func (s *Server) createObject(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeCreateObject(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	var t bacnet.ObjectType
	var inst *uint32
	switch {
	case r.ObjectIdentifier != nil:
		t = r.ObjectIdentifier.Type
		if r.ObjectIdentifier.Instance != bacnet.WildcardInstance {
			i := r.ObjectIdentifier.Instance
			inst = &i
		}
	case r.ObjectType != nil:
		t = *r.ObjectType
	default:
		return fail(&rejectError{bacnet.RejectReasonMissingRequiredParameter})
	}
	name := ""
	for i, pv := range r.InitialValues {
		if pv.Property.Identifier != bacnet.PropertyObjectName {
			continue
		}
		if pv.Value.Kind != bacnet.ValueCharacterString || pv.Value.Character.Value == "" || pv.Property.ArrayIndex != nil {
			return fail(createFailure(object.ErrInvalidDataType, uint32(i+1)))
		}
		name = pv.Value.Character.Value
		if _, taken := s.dev.ObjectByName(name); taken {
			return fail(createFailure(object.ErrDuplicateName, uint32(i+1)))
		}
	}
	o, err := s.dev.Create(t, inst, name)
	if err != nil {
		return fail(createFailure(err, 0))
	}
	for i, pv := range r.InitialValues {
		switch pv.Property.Identifier {
		case bacnet.PropertyObjectName:
			continue
		case bacnet.PropertyObjectIdentifier, bacnet.PropertyObjectType:
			// Fixed at creation; a matching value is accepted.
			cur, _ := o.ReadProperty(ctx, pv.Property)
			if sameApp(cur, pv.Value) {
				continue
			}
			_ = s.dev.Remove(o.Identifier())
			return fail(createFailure(object.ErrWriteAccessDenied, uint32(i+1)))
		default:
			// Written below.
		}
		w := object.Write{Property: pv.Property, Value: pv.Value}
		if pv.Priority != nil {
			w.Priority = *pv.Priority
		}
		if err := o.WriteProperty(ctx, w); err != nil {
			_ = s.dev.Remove(o.Identifier())
			return fail(createFailure(err, uint32(i+1)))
		}
	}
	payload, err := service.EncodeCreateObjectACK(service.CreateObjectACK{Object: o.Identifier()})
	if err != nil {
		return fail(err)
	}
	return complexACK(payload)
}

func sameApp(a, b bacnet.ApplicationValue) bool {
	x, err1 := bacnet.AppendApplicationValue(nil, a)
	y, err2 := bacnet.AppendApplicationValue(nil, b)
	return err1 == nil && err2 == nil && string(x) == string(y)
}

func createFailure(err error, first uint32) error {
	e := object.AsError(err)
	return &createObjectError{class: e.Class, code: e.Code, first: first}
}

// deleteObject serves DeleteObject (clause 15.4): only objects created
// with CreateObject, or marked deletable by the application, can be
// deleted, and never the Device object (bacnet-stack answers
// object-deletion-not-permitted, device.c).
func (s *Server) deleteObject(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeDeleteObject(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	o, ok := s.dev.Object(r.Object)
	if !ok {
		return fail(object.ErrUnknownObject)
	}
	if d, ok := o.(object.Deletable); !ok || !d.Deletable() || o.Identifier() == s.dev.Identifier() {
		return fail(object.ErrNotDeletable)
	}
	if h := s.cfg.hooks.BeforeDelete; h != nil {
		if err := h(ctx, o.Identifier()); err != nil {
			return fail(err)
		}
	}
	if err := s.dev.Remove(o.Identifier()); err != nil {
		return fail(err)
	}
	s.cov.objectRemoved(o.Identifier())
	return ack()
}
