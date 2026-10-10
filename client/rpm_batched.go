// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// RPMBatchOptions controls adaptive ReadPropertyMultiple planning.
type RPMBatchOptions struct {
	// SafetyMargin octets reserved under remote MaxAPDU (default 16).
	SafetyMargin int
	// MaxPropertiesPerBatch caps the property references in one request
	// (default 32). Lower it for peers with small reply buffers.
	MaxPropertiesPerBatch int
	// MaxBatches caps the number of RPM transactions, retries after a
	// too-large reply included (default 64).
	MaxBatches int
}

// ReadPropertyMultipleBatched reads specs with as many ReadPropertyMultiple
// transactions as the peer's MaxAPDU requires and returns one result per
// input spec, in input order, with per-property errors preserved.
//
// A batch holds at most MaxPropertiesPerBatch property references and never
// more than fit in one unsegmented request; a spec with many properties is
// split across batches and its results are joined again. When the peer aborts
// because the reply did not fit (buffer-overflow, segmentation-not-supported,
// apdu-too-long) the batch is halved and retried, which is safe because the
// service only reads. A single property whose value still does not fit is
// returned as that Abort.
//
// On error the results gathered so far are returned with it; specs not yet
// read have no Properties.
func (c *Client) ReadPropertyMultipleBatched(ctx context.Context, target Target, specs []service.ReadAccessSpecification, opts RPMBatchOptions) ([]service.ReadAccessResult, error) {
	if len(specs) == 0 {
		return nil, fmt.Errorf("%w: empty RPM specs", bacnet.ErrMalformed)
	}
	if opts.SafetyMargin <= 0 {
		opts.SafetyMargin = 16
	}
	if opts.MaxPropertiesPerBatch <= 0 {
		opts.MaxPropertiesPerBatch = 32
	}
	if opts.MaxBatches <= 0 {
		opts.MaxBatches = 64
	}
	budget := max(int(c.resolveRemoteMaxAPDU(target))-opts.SafetyMargin, 50)

	// One unit per property reference, remembering the spec it came from.
	type unit struct {
		spec int
		prop bacnet.PropertyReference
	}
	var units []unit
	out := make([]service.ReadAccessResult, len(specs))
	for i, spec := range specs {
		if len(spec.Properties) == 0 {
			return nil, fmt.Errorf("%w: RPM spec %d has no properties", bacnet.ErrMalformed, i)
		}
		out[i].Object = spec.Object
		for _, p := range spec.Properties {
			units = append(units, unit{spec: i, prop: p})
		}
	}
	// group rebuilds request specs from consecutive units of the same spec and
	// reports which input spec each request spec belongs to.
	group := func(us []unit) ([]service.ReadAccessSpecification, []int) {
		var req []service.ReadAccessSpecification
		var owner []int
		for _, u := range us {
			if n := len(owner); n == 0 || owner[n-1] != u.spec {
				req = append(req, service.ReadAccessSpecification{Object: specs[u.spec].Object})
				owner = append(owner, u.spec)
			}
			last := &req[len(req)-1]
			last.Properties = append(last.Properties, u.prop)
		}
		return req, owner
	}
	fits := func(us []unit) (bool, error) {
		req, _ := group(us)
		raw, err := service.EncodeReadPropertyMultiple(req)
		if err != nil {
			return false, err
		}
		// Unsegmented confirmed-request header: type, max-info, invoke, service.
		return 4+len(raw) <= budget, nil
	}

	batches := 0
	batchSize := opts.MaxPropertiesPerBatch
	for i := 0; i < len(units); {
		if batches >= opts.MaxBatches {
			return out, fmt.Errorf("%w: RPM batch limit %d", bacnet.ErrLimitExceeded, opts.MaxBatches)
		}
		// Always send at least one unit; grow while the request still fits.
		end := i + 1
		for end < len(units) && end-i < batchSize {
			ok, err := fits(units[i : end+1])
			if err != nil {
				return out, err
			}
			if !ok {
				break
			}
			end++
		}
		req, owner := group(units[i:end])
		results, err := c.ReadPropertyMultiple(ctx, target, req)
		batches++
		if err != nil {
			var abort *bacnet.AbortError
			if errors.As(err, &abort) && responseTooLarge(abort.Reason) && end-i > 1 {
				// The peer could not return this many results in one reply:
				// halve what the batch actually carried and try again.
				batchSize = max(1, (end-i)/2)
				continue
			}
			return out, err
		}
		if len(results) != len(req) {
			return out, fmt.Errorf("%w: RPM ACK has %d results for %d specifications",
				bacnet.ErrProtocolViolation, len(results), len(req))
		}
		for j, res := range results {
			if !req[j].Object.Matches(res.Object) {
				return out, fmt.Errorf("%w: RPM ACK result %d is for %v, requested %v",
					bacnet.ErrProtocolViolation, j, res.Object, req[j].Object)
			}
			out[owner[j]].Object = res.Object
			out[owner[j]].Properties = append(out[owner[j]].Properties, res.Properties...)
		}
		i = end
	}
	return out, nil
}

// responseTooLarge reports Abort reasons that mean the reply did not fit and a
// smaller request can succeed.
func responseTooLarge(reason uint8) bool {
	switch reason {
	case bacnet.AbortReasonBufferOverflow,
		bacnet.AbortReasonSegmentationNotSupported,
		bacnet.AbortReasonAPDUTooLong:
		return true
	default:
		return false
	}
}
