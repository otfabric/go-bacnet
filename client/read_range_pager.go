// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// ReadRangePageOptions bounds ReadRange paging helpers.
type ReadRangePageOptions struct {
	PageCount   int32 // items requested per page; default 50
	MaxItems    int   // total items across pages; default 4096
	MaxBytes    int   // approximate ItemData byte budget; 0 disables
	MaxPages    int   // default 256
	StopOnError bool  // stop and return partial + error (default true)
}

// ReadRangePageResult aggregates paged ReadRange ACKs.
type ReadRangePageResult struct {
	Pages         []service.ReadRangeACK
	Items         []bacnet.ApplicationValue
	LogRecords    []service.LogRecord
	Partial       bool
	StoppedReason string
}

// ReadRangeAll pages ReadRange until LastItem, !MoreItems, or configured limits.
//
// Only byPosition and bySequenceNumber are advanced automatically. byTime
// paging requires the caller to supply successive ReferenceTime values via
// repeated ReadRange calls.
func (c *Client) ReadRangeAll(ctx context.Context, target Target, req service.ReadRangeRequest, opts ReadRangePageOptions) (ReadRangePageResult, error) {
	if opts.PageCount <= 0 {
		opts.PageCount = 50
	}
	if opts.MaxItems <= 0 {
		opts.MaxItems = 4096
	}
	if opts.MaxPages <= 0 {
		opts.MaxPages = 256
	}
	if req.By != service.ReadRangeByPosition && req.By != service.ReadRangeBySequenceNumber {
		return ReadRangePageResult{}, fmt.Errorf("%w: ReadRangeAll supports byPosition/bySequence only", bacnet.ErrUnsupported)
	}
	out := ReadRangePageResult{}
	cur := req
	if cur.Count == 0 {
		cur.Count = opts.PageCount
	}
	approxBytes := 0
	for page := 0; page < opts.MaxPages; page++ {
		ack, err := c.ReadRange(ctx, target, cur)
		if err != nil {
			out.Partial = true
			out.StoppedReason = "request error"
			if opts.StopOnError || len(out.Pages) == 0 {
				return out, err
			}
			return out, err
		}
		out.Pages = append(out.Pages, ack)
		out.Items = append(out.Items, ack.ItemData...)
		out.LogRecords = append(out.LogRecords, ack.LogRecords...)
		for _, v := range ack.ItemData {
			approxBytes += estimateValueBytes(v)
		}
		if len(out.Items) >= opts.MaxItems {
			out.Partial = true
			out.StoppedReason = "MaxItems"
			return out, nil
		}
		if opts.MaxBytes > 0 && approxBytes >= opts.MaxBytes {
			out.Partial = true
			out.StoppedReason = "MaxBytes"
			return out, nil
		}
		if ack.LastItem() || !ack.MoreItems() || ack.ItemCount == 0 {
			return out, nil
		}
		// Advance reference for the next page.
		switch cur.By {
		case service.ReadRangeByPosition:
			cur.ReferenceIndex += ack.ItemCount
		case service.ReadRangeBySequenceNumber:
			if ack.FirstSequence != nil {
				cur.ReferenceIndex = *ack.FirstSequence + ack.ItemCount
			} else {
				cur.ReferenceIndex += ack.ItemCount
			}
		default:
			out.Partial = true
			out.StoppedReason = "unsupported paging mode"
			return out, nil
		}
		cur.Count = opts.PageCount
	}
	out.Partial = true
	out.StoppedReason = "MaxPages"
	return out, nil
}

func estimateValueBytes(v bacnet.ApplicationValue) int {
	n := 8
	n += len(v.OctetString)
	n += len(v.Character.Value)
	for _, el := range v.Elements {
		n += estimateValueBytes(el.Value)
	}
	return n
}
