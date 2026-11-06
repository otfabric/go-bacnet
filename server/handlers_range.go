// SPDX-License-Identifier: MIT

package server

import (
	"context"
	"sort"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/server/object"
	"github.com/otfabric/go-bacnet/service"
)

// responseBudget is the largest ComplexACK service payload the requester
// accepts: one APDU, or as many segments as both sides allow.
func (s *Server) responseBudget(req *apdu.ConfirmedRequest) int {
	remoteMax, _ := apdu.DecodeMaxAPDUSize(apdu.MaxAPDUCode(req.MaxAPDU))
	limit := int(min(remoteMax, s.cfg.maxAPDU))
	if !req.SegmentedResponseAccepted || !s.cfg.segmentation {
		return limit - 3 // unsegmented ComplexACK header
	}
	segs := s.cfg.maxSegments
	if m := decodeMaxSegments(req.MaxSegments); m > 0 && m < segs {
		segs = m
	}
	return (limit - 5) * min(segs, 256)
}

// readRange serves ReadRange (clause 15.8). The object selects the items;
// the reply carries as many as fit the requester's buffer and sets
// MORE_ITEMS when some were left out, as bacnet-stack does.
func (s *Server) readRange(ctx context.Context, req *apdu.ConfirmedRequest, _ peer) result {
	r, err := service.DecodeReadRange(req.Payload, s.limits)
	if err != nil {
		return rejectFor(err)
	}
	o, ok := s.dev.Object(r.Object)
	if !ok {
		return fail(object.ErrUnknownObject)
	}
	rr, ok := o.(object.RangeReader)
	if !ok {
		// An unknown property keeps its own error; any other is not a
		// list ReadRange can read.
		if _, err := o.ReadProperty(ctx, r.Property); err != nil && object.AsError(err).Code == bacnet.ErrorCodeUnknownProperty {
			return fail(err)
		}
		return fail(errPropertyIsNotAList)
	}
	sel, err := rr.ReadRange(ctx, r.Property, object.RangeQuery{By: r.By, Index: r.ReferenceIndex, Time: r.ReferenceTime, Count: r.Count})
	if err != nil {
		return fail(err)
	}
	n := len(sel.Records) + len(sel.Items)
	build := func(k int) ([]byte, error) {
		ack := service.ReadRangeACK{Object: o.Identifier(), Property: r.Property, ItemCount: uint32(k)}
		if sel.Records != nil {
			ack.LogRecords = sel.Records[:k]
		} else {
			ack.ItemData = sel.Items[:k]
		}
		first := k > 0 && sel.Position == 1
		last := k > 0 && sel.Position+uint32(k)-1 == sel.Total
		ack.ResultFlags = service.EncodeResultFlags(first, last, k < n)
		if sel.Sequenced && k > 0 && (r.By == service.ReadRangeBySequenceNumber || r.By == service.ReadRangeByTime) {
			seq := sel.FirstSequence
			ack.FirstSequence = &seq
		}
		return service.EncodeReadRangeACK(ack)
	}
	budget := s.responseBudget(req)
	payload, err := build(n)
	if err != nil {
		return fail(err)
	}
	if len(payload) > budget {
		// The largest prefix that fits.
		k := sort.Search(n+1, func(k int) bool {
			p, err := build(k)
			return err != nil || len(p) > budget
		}) - 1
		if k < 0 {
			k = 0
		}
		if payload, err = build(k); err != nil {
			return fail(err)
		}
	}
	return complexACK(payload)
}

var errPropertyIsNotAList = object.Err(bacnet.ErrorClassServices, bacnet.ErrorCodePropertyIsNotAList)
