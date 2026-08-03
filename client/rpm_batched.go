// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// RPMBatchOptions controls adaptive ReadPropertyMultiple planning.
type RPMBatchOptions struct {
	// SafetyMargin octets reserved under remote MaxAPDU (default 16).
	SafetyMargin int
	// MaxPropertiesPerBatch caps properties in one request (default 32).
	MaxPropertiesPerBatch int
	// MaxBatches caps the number of RPM transactions (default 64).
	MaxBatches int
}

// ReadPropertyMultipleBatched splits specs into MaxAPDU-aware batches, merges
// results in request order, and preserves per-property errors. Automatic batch
// shrink after size/segmentation Abort applies only to this read-only path.
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
	remoteMax := int(c.resolveRemoteMaxAPDU(target))
	budget := remoteMax - opts.SafetyMargin
	if budget < 50 {
		budget = 50
	}

	var out []service.ReadAccessResult
	batches := 0
	i := 0
	batchSize := opts.MaxPropertiesPerBatch
	for i < len(specs) {
		if batches >= opts.MaxBatches {
			return out, fmt.Errorf("%w: RPM batch limit %d", bacnet.ErrLimitExceeded, opts.MaxBatches)
		}
		end := i + 1
		// Grow batch while encoded size fits.
		for end < len(specs) && end-i < batchSize {
			trial := specs[i : end+1]
			raw, err := service.EncodeReadPropertyMultiple(trial)
			if err != nil {
				return out, err
			}
			probe := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
				SegmentedResponseAccepted: true,
				ServiceChoice:             apdu.ServiceReadPropertyMultiple,
				Payload:                   raw,
			})
			if len(probe) > budget {
				break
			}
			end++
		}
		batch := specs[i:end]
		raw, err := service.EncodeReadPropertyMultiple(batch)
		if err != nil {
			return out, err
		}
		probe := apdu.AppendConfirmedRequest(nil, apdu.ConfirmedRequest{
			SegmentedResponseAccepted: true,
			ServiceChoice:             apdu.ServiceReadPropertyMultiple,
			Payload:                   raw,
		})
		if len(probe) > budget && len(batch) > 1 {
			end = i + 1
			batch = specs[i:end]
		}
		results, err := c.ReadPropertyMultiple(ctx, target, batch)
		if err != nil {
			var abort *bacnet.AbortError
			if errors.As(err, &abort) && (abort.Reason == 4 || abort.Reason == 5) && batchSize > 1 {
				batchSize = max(1, batchSize/2)
				continue
			}
			return out, err
		}
		out = append(out, results...)
		i = end
		batches++
	}
	return out, nil
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
