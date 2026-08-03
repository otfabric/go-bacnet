// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"fmt"
	"sync"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
	"github.com/otfabric/go-bacnet/internal/diag"
)

// BVLCOperationError is a non-success BVLC-Result for a management operation.
type BVLCOperationError struct {
	Operation string
	Code      uint16
}

func (e *BVLCOperationError) Error() string {
	return fmt.Sprintf("bacnet: BVLC %s result code=%d", e.Operation, e.Code)
}

func (e *BVLCOperationError) Unwrap() error { return bacnet.ErrProtocolViolation }

type bvlcPending struct {
	peer bip.Endpoint
	want bvlc.Function // expected ACK function; FunctionResult always accepted
	op   string
	ch   chan bvlcPendingResult
}

type bvlcPendingResult struct {
	msg bvlc.Message
	err error
}

type bvlcOps struct {
	mu      sync.Mutex
	pending *bvlcPending
}

func (o *bvlcOps) begin(p *bvlcPending) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.pending != nil {
		return fmt.Errorf("%w: BVLC management operation already in flight", bacnet.ErrBusy)
	}
	o.pending = p
	return nil
}

func (o *bvlcOps) clear(p *bvlcPending) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.pending == p {
		o.pending = nil
	}
}

func (o *bvlcOps) deliver(msg bvlc.Message, from bip.Endpoint) bool {
	o.mu.Lock()
	p := o.pending
	o.mu.Unlock()
	if p == nil {
		return false
	}
	if !from.Equal(p.peer) {
		return false
	}
	if msg.Function == bvlc.FunctionResult {
		select {
		case p.ch <- bvlcPendingResult{msg: msg}:
		default:
		}
		return true
	}
	if msg.Function != p.want {
		return false
	}
	select {
	case p.ch <- bvlcPendingResult{msg: msg}:
	default:
	}
	return true
}

func (c *Client) bvlcExchange(ctx context.Context, peer bip.Endpoint, req bvlc.Message, wantAck bvlc.Function, op string) (bvlc.Message, error) {
	if c.isClosed() {
		return bvlc.Message{}, bacnet.ErrClosed
	}
	if !peer.IsValid() {
		return bvlc.Message{}, fmt.Errorf("%w: BBMD endpoint invalid", bacnet.ErrMalformed)
	}
	p := &bvlcPending{
		peer: peer,
		want: wantAck,
		op:   op,
		ch:   make(chan bvlcPendingResult, 1),
	}
	if err := c.bvlcOps.begin(p); err != nil {
		return bvlc.Message{}, err
	}
	defer c.bvlcOps.clear(p)

	frame, err := bvlc.Append(nil, req)
	if err != nil {
		return bvlc.Message{}, err
	}
	if err := c.tr.Send(ctx, OutboundPacket{Data: frame, Destination: peer}); err != nil {
		return bvlc.Message{}, err
	}
	timer := c.clock.NewTimer(c.cfg.apduTimeout)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return bvlc.Message{}, ctx.Err()
	case <-c.closeCh:
		return bvlc.Message{}, bacnet.ErrClosed
	case <-timer.C():
		return bvlc.Message{}, bacnet.ErrTimeout
	case res := <-p.ch:
		if res.err != nil {
			return bvlc.Message{}, res.err
		}
		if res.msg.Function == bvlc.FunctionResult {
			if res.msg.ResultCode != 0 {
				return bvlc.Message{}, &BVLCOperationError{Operation: op, Code: res.msg.ResultCode}
			}
			// Success Result with empty body for write/delete.
			return res.msg, nil
		}
		return res.msg, nil
	}
}

// ReadBroadcastDistributionTable reads the BDT from bbmd.
func (c *Client) ReadBroadcastDistributionTable(ctx context.Context, bbmd bip.Endpoint) ([]bvlc.BDTEntry, error) {
	msg, err := c.bvlcExchange(ctx, bbmd, bvlc.Message{Function: bvlc.FunctionReadBroadcastDistributionTable}, bvlc.FunctionReadBroadcastDistributionTableAck, "Read-BDT")
	if err != nil {
		return nil, err
	}
	if msg.Function == bvlc.FunctionResult {
		return nil, nil
	}
	return bvlc.DecodeBDTEntries(msg.Payload, c.limits)
}

// WriteBroadcastDistributionTable writes the BDT on bbmd.
// Requires WithNetworkManagementEnabled.
func (c *Client) WriteBroadcastDistributionTable(ctx context.Context, bbmd bip.Endpoint, entries []bvlc.BDTEntry) error {
	if !c.cfg.networkManagementEnabled {
		return ErrNetworkManagementDisabled
	}
	payload, err := bvlc.EncodeBDTEntries(nil, entries)
	if err != nil {
		return err
	}
	_, err = c.bvlcExchange(ctx, bbmd, bvlc.Message{
		Function: bvlc.FunctionWriteBroadcastDistributionTable,
		Payload:  payload,
	}, bvlc.FunctionResult, "Write-BDT")
	return err
}

// ReadForeignDeviceTable reads the FDT from bbmd.
func (c *Client) ReadForeignDeviceTable(ctx context.Context, bbmd bip.Endpoint) ([]bvlc.FDTEntry, error) {
	msg, err := c.bvlcExchange(ctx, bbmd, bvlc.Message{Function: bvlc.FunctionReadForeignDeviceTable}, bvlc.FunctionReadForeignDeviceTableAck, "Read-FDT")
	if err != nil {
		return nil, err
	}
	if msg.Function == bvlc.FunctionResult {
		return nil, nil
	}
	return bvlc.DecodeFDTEntries(msg.Payload, c.limits)
}

// DeleteForeignDeviceTableEntry deletes one FDT entry on bbmd.
func (c *Client) DeleteForeignDeviceTableEntry(ctx context.Context, bbmd bip.Endpoint, entry bip.Endpoint) error {
	payload, err := bvlc.EncodeDeleteFDTEntry(nil, entry)
	if err != nil {
		return err
	}
	_, err = c.bvlcExchange(ctx, bbmd, bvlc.Message{
		Function: bvlc.FunctionDeleteForeignDeviceTableEntry,
		Payload:  payload,
	}, bvlc.FunctionResult, "Delete-FDT")
	return err
}

func (c *Client) handleBVLCManagement(msg bvlc.Message, from bip.Endpoint) bool {
	switch msg.Function {
	case bvlc.FunctionResult,
		bvlc.FunctionReadBroadcastDistributionTableAck,
		bvlc.FunctionReadForeignDeviceTableAck:
		if c.bvlcOps.deliver(msg, from) {
			return true
		}
		if msg.Function == bvlc.FunctionResult && c.fd != nil {
			c.fd.handleResult(msg.ResultCode, from)
			return true
		}
		c.diag.Report(diag.Event{
			Kind:    diag.KindBVLC,
			Message: "unsolicited BVLC management response",
			Fields:  map[string]any{"function": uint8(msg.Function), "from": from.String()},
		})
		return true
	default:
		return false
	}
}
