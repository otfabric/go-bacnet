// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"
	"math"
	"slices"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// ReadRangePageOptions bounds ReadRange paging helpers.
type ReadRangePageOptions struct {
	PageCount int32 // items requested per page; default 50
	MaxItems  int   // total items across pages; default 4096
	MaxBytes  int   // approximate ItemData byte budget; 0 disables
	MaxPages  int   // default 256
	// Deprecated: StopOnError has no effect. ReadRangeAll always stops at the
	// first failed page and returns the pages read so far with the error.
	StopOnError bool
}

// ReadRangePageResult aggregates paged ReadRange ACKs.
type ReadRangePageResult struct {
	Pages      []service.ReadRangeACK
	Items      []bacnet.ApplicationValue
	LogRecords []service.LogRecord
	// LogMultipleRecords and EventLogRecords collect the pages' records of
	// a Trend Log Multiple and an Event Log.
	LogMultipleRecords []service.LogMultipleRecord
	EventLogRecords    []service.EventLogRecord
	Partial            bool
	StoppedReason      string
}

// ReadRangeAll pages ReadRange until the peer reports LAST_ITEM, returns an
// empty or short final page, or a configured limit is reached (Partial is then
// set and StoppedReason names the limit).
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
	// remaining is the item budget left. It is int64 so that adding
	// peer-supplied uint32 counts cannot wrap on 32-bit platforms.
	remaining := int64(opts.MaxItems)
	for page := 0; page < opts.MaxPages; page++ {
		ack, err := c.ReadRange(ctx, target, cur)
		if err != nil {
			out.Partial = true
			out.StoppedReason = "request error"
			return out, err
		}
		// A page counts as the larger of the peer's ItemCount and what was
		// actually decoded: typed log records for a Log_Buffer, otherwise
		// the ItemData elements.
		pageItems := readRangePageItems(ack)
		truncated := pageItems > remaining
		if truncated {
			ack = truncateReadRangeACK(ack, remaining)
			pageItems = remaining
		}
		out.Pages = append(out.Pages, ack)
		out.Items = append(out.Items, ack.ItemData...)
		out.LogRecords = append(out.LogRecords, ack.LogRecords...)
		out.LogMultipleRecords = append(out.LogMultipleRecords, ack.LogMultipleRecords...)
		out.EventLogRecords = append(out.EventLogRecords, ack.EventLogRecords...)
		for _, v := range ack.ItemData {
			approxBytes += estimateValueBytes(v)
		}
		remaining -= pageItems
		if truncated || remaining <= 0 {
			out.Partial = true
			out.StoppedReason = "MaxItems"
			return out, nil
		}
		if opts.MaxBytes > 0 && approxBytes >= opts.MaxBytes {
			out.Partial = true
			out.StoppedReason = "MaxBytes"
			return out, nil
		}
		// LAST_ITEM ends the list. MORE_ITEMS only says that items matching
		// this one request did not fit in the reply; a page that returned
		// everything it was asked for has it clear with records still to
		// come, so it cannot end the paging. A short page without MORE_ITEMS
		// can: the peer had nothing further.
		short := cur.Count > 0 && ack.ItemCount < uint32(cur.Count)
		if ack.LastItem() || ack.ItemCount == 0 || (short && !ack.MoreItems()) {
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

// readRangePageItems returns the number of items a page holds: the larger of
// the peer's ItemCount and the decoded record (or ItemData element) count.
func readRangePageItems(ack service.ReadRangeACK) int64 {
	decoded := len(ack.LogRecords) + len(ack.LogMultipleRecords) + len(ack.EventLogRecords)
	if decoded == 0 {
		decoded = len(ack.ItemData)
	}
	return max(int64(ack.ItemCount), int64(decoded))
}

// truncateReadRangeACK keeps the first keep items of ack, in fresh slices so
// the dropped tail is not retained. ItemCount stays as the peer reported it. For a page of typed log records the
// records are kept and the flat ItemData stream, which cannot be split per
// record, is dropped.
func truncateReadRangeACK(ack service.ReadRangeACK, keep64 int64) service.ReadRangeACK {
	keep := 0
	if keep64 > 0 && keep64 <= math.MaxInt {
		keep = int(keep64)
	}
	if len(ack.LogRecords)+len(ack.LogMultipleRecords)+len(ack.EventLogRecords) > 0 {
		ack.LogRecords = slices.Clone(ack.LogRecords[:min(keep, len(ack.LogRecords))])
		keep -= len(ack.LogRecords)
		ack.LogMultipleRecords = slices.Clone(ack.LogMultipleRecords[:min(keep, len(ack.LogMultipleRecords))])
		keep -= len(ack.LogMultipleRecords)
		ack.EventLogRecords = slices.Clone(ack.EventLogRecords[:min(keep, len(ack.EventLogRecords))])
		ack.ItemData = nil
	} else {
		ack.ItemData = slices.Clone(ack.ItemData[:min(keep, len(ack.ItemData))])
	}
	return ack
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
