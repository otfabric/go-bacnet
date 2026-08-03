// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// BatchWriteState is the outcome of one transmitted WPM batch.
type BatchWriteState uint8

const (
	BatchWriteCompleted BatchWriteState = iota
	BatchWriteFailed
	BatchWriteUnknown
)

// WriteReference identifies one write inside a batch.
type WriteReference struct {
	Object   bacnet.ObjectIdentifier
	Property bacnet.PropertyReference
}

// BatchWriteOutcome records one WPM transaction.
type BatchWriteOutcome struct {
	Writes []WriteReference
	State  BatchWriteState
	Err    error
}

// BatchWriteResult aggregates explicit WPM batch outcomes.
type BatchWriteResult struct {
	Batches []BatchWriteOutcome
}

// WPMBatchOptions controls explicit WritePropertyMultiple batching.
type WPMBatchOptions struct {
	MaxSpecsPerBatch int // default 16
}

// WritePropertyMultipleBatched sends specs in explicit batches and never
// silently retries a side-effecting batch after ambiguity.
func (c *Client) WritePropertyMultipleBatched(ctx context.Context, target Target, specs []service.WriteAccessSpecification, opts WPMBatchOptions) (BatchWriteResult, error) {
	var result BatchWriteResult
	if len(specs) == 0 {
		return result, fmt.Errorf("%w: empty WPM specs", bacnet.ErrMalformed)
	}
	if opts.MaxSpecsPerBatch <= 0 {
		opts.MaxSpecsPerBatch = 16
	}
	for i := 0; i < len(specs); {
		end := i + opts.MaxSpecsPerBatch
		if end > len(specs) {
			end = len(specs)
		}
		batch := specs[i:end]
		refs := writeRefs(batch)
		err := c.WritePropertyMultiple(ctx, target, batch)
		outcome := BatchWriteOutcome{Writes: refs}
		if err == nil {
			outcome.State = BatchWriteCompleted
		} else {
			var unknown *bacnet.OutcomeUnknownError
			if errors.As(err, &unknown) {
				outcome.State = BatchWriteUnknown
			} else {
				outcome.State = BatchWriteFailed
			}
			outcome.Err = err
			result.Batches = append(result.Batches, outcome)
			return result, err
		}
		result.Batches = append(result.Batches, outcome)
		i = end
	}
	return result, nil
}

func writeRefs(specs []service.WriteAccessSpecification) []WriteReference {
	var out []WriteReference
	for _, s := range specs {
		for _, w := range s.Properties {
			out = append(out, WriteReference{Object: s.Object, Property: w.Property})
		}
	}
	return out
}
