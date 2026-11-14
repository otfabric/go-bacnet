// SPDX-License-Identifier: MIT

// Package bbmd is a BACnet/IP Broadcast Management Device (ASHRAE 135
// Annex J.4, J.5) for a server that shares its UDP endpoint with it. It
// keeps the Broadcast Distribution Table and the Foreign Device Table,
// answers the BVLL table and registration messages, and forwards
// broadcasts between the local subnet, its peer BBMDs and the registered
// foreign devices.
//
// Where the annex leaves room the behaviour follows bacnet-stack
// (src/bacnet/basic/bbmd/h_bbmd.c at the interop pin): the BDT is
// configured locally and Write-Broadcast-Distribution-Table is refused
// (J.4.4.2 for Protocol_Revision 17 and later), a registration lives for
// its Time-to-Live plus a 30 second grace period, and a Forwarded-NPDU
// from a peer whose BDT mask is all ones (one-hop distribution) is
// rebroadcast on the local subnet. A Forwarded-NPDU from a node that is
// not a BDT peer is delivered to the local device only, a BDT peer cannot
// register as a foreign device, and packets from a node that is not an
// IPv4 endpoint are ignored.
//
// A BBMD is used through server.WithBBMD; it holds no goroutines and
// needs no Close.
package bbmd

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/bvlc"
)

// GracePeriod is added to a foreign device's Time-to-Live (Annex J.5.2.3).
const GracePeriod = 30 * time.Second

// ErrInvalidConfig reports an unusable configuration.
var ErrInvalidConfig = bacnet.ErrInvalidConfig

// Config configures a BBMD.
type Config struct {
	// Self is the B/IP address of this BBMD (and its server). It must be a
	// unicast IPv4 address, not the wildcard address the socket may be
	// bound to.
	Self bip.Endpoint
	// BDT is the Broadcast Distribution Table, normally including the
	// entry of this BBMD. Peers receive forwarded broadcasts at their
	// address ORed with the inverted mask.
	BDT []bvlc.BDTEntry
	// MaxForeignDevices bounds the FDT; zero means 128.
	MaxForeignDevices int
	// RejectRegistrations refuses Register-Foreign-Device.
	RejectRegistrations bool
	// Now returns the current time; nil means time.Now.
	Now func() time.Time
}

// BBMD implements server.BBMD.
type BBMD struct {
	self   bip.Endpoint
	now    func() time.Time
	max    int
	reject bool

	mu  sync.Mutex
	bdt []bvlc.BDTEntry
	fdt map[bip.Endpoint]foreign
}

type foreign struct {
	ttl     uint16
	expires time.Time
}

// New returns a BBMD.
func New(cfg Config) (*BBMD, error) {
	if !cfg.Self.IsValid() || !cfg.Self.Addr.Addr().Is4() || cfg.Self.Addr.Addr().IsUnspecified() {
		return nil, fmt.Errorf("%w: bbmd: Self must be a unicast IPv4 endpoint", ErrInvalidConfig)
	}
	for i, e := range cfg.BDT {
		if _, err := bvlc.NewBDTEntry(e.Endpoint, e.Mask); err != nil {
			return nil, fmt.Errorf("%w: bbmd: BDT entry %d: %v", ErrInvalidConfig, i, err)
		}
	}
	if cfg.MaxForeignDevices < 0 {
		return nil, fmt.Errorf("%w: bbmd: MaxForeignDevices < 0", ErrInvalidConfig)
	}
	if cfg.MaxForeignDevices == 0 {
		cfg.MaxForeignDevices = 128
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &BBMD{
		self: cfg.Self, now: cfg.Now, max: cfg.MaxForeignDevices, reject: cfg.RejectRegistrations,
		bdt: append([]bvlc.BDTEntry(nil), cfg.BDT...), fdt: make(map[bip.Endpoint]foreign),
	}, nil
}

// BDT returns the Broadcast Distribution Table.
func (b *BBMD) BDT() []bvlc.BDTEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]bvlc.BDTEntry(nil), b.bdt...)
}

// FDT returns the live Foreign Device Table, ordered by address, with the
// remaining time of each entry in seconds.
func (b *BBMD) FDT() []bvlc.FDTEntry {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.fdtLocked(b.now())
}

func (b *BBMD) fdtLocked(now time.Time) []bvlc.FDTEntry {
	out := make([]bvlc.FDTEntry, 0, len(b.fdt))
	for ep, f := range b.fdt {
		left := f.expires.Sub(now)
		if left <= 0 {
			delete(b.fdt, ep)
			continue
		}
		secs := (left + time.Second - 1) / time.Second
		if secs > 0xFFFF {
			secs = 0xFFFF
		}
		out = append(out, bvlc.FDTEntry{Address: ep, TTL: f.ttl, Remaining: uint16(secs)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Address.Addr.Compare(out[j].Address.Addr) < 0 })
	return out
}

// forwardAddress is a BDT entry's address ORed with its inverted mask
// (Annex J.4.3.2): the directed broadcast of the peer's subnet, or the
// peer itself when the mask is all ones.
func forwardAddress(e bvlc.BDTEntry) bip.Endpoint {
	ip := e.Endpoint.Addr.Addr().As4()
	for i := range ip {
		ip[i] |= ^e.Mask[i]
	}
	return bip.NewEndpoint(netip.AddrPortFrom(netip.AddrFrom4(ip), e.Endpoint.Addr.Port()))
}

func oneHop(e bvlc.BDTEntry) bool { return e.Mask == bvlc.IPv4Mask{0xFF, 0xFF, 0xFF, 0xFF} }

type sendFunc = func(context.Context, bip.OutboundPacket) error

func forwarded(origin bip.Endpoint, npdu []byte) ([]byte, error) {
	if !origin.IsValid() {
		return nil, fmt.Errorf("bbmd: origin %q is not an IPv4 endpoint", origin.String())
	}
	return bvlc.Append(nil, bvlc.Message{
		Function: bvlc.FunctionForwardedNPDU, OriginIP: origin.Addr.Addr().As4(), OriginPort: origin.Addr.Port(), Payload: npdu,
	})
}

// distribute sends a Forwarded-NPDU to the BDT peers (except this BBMD
// and the origin) and to the foreign devices (except the origin, the
// immediate sender and any BDT peer, which receive the broadcast through
// the BDT or already have it).
func (b *BBMD) distribute(ctx context.Context, origin, from bip.Endpoint, npdu []byte, peers bool, send sendFunc) error {
	frame, err := forwarded(origin, npdu)
	if err != nil {
		return err
	}
	b.mu.Lock()
	var dests []bip.Endpoint
	if peers {
		for _, e := range b.bdt {
			to := forwardAddress(e)
			if to.Equal(b.self) || e.Endpoint.Equal(b.self) || to.Equal(origin) {
				continue
			}
			dests = append(dests, to)
		}
	}
	for _, f := range b.fdtLocked(b.now()) {
		if f.Address.Equal(origin) || f.Address.Equal(b.self) || f.Address.Equal(from) || b.isPeerLocked(f.Address) {
			continue
		}
		dests = append(dests, f.Address)
	}
	b.mu.Unlock()
	var errs []error
	for _, to := range dests {
		if err := send(ctx, bip.OutboundPacket{Data: frame, Destination: to}); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func result(ctx context.Context, to bip.Endpoint, code uint16, send sendFunc) {
	frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionResult, ResultCode: code})
	if err == nil {
		_ = send(ctx, bip.OutboundPacket{Data: frame, Destination: to})
	}
}

// isPeerLocked reports whether ep is the address of a BDT entry other
// than this BBMD. The caller holds b.mu.
func (b *BBMD) isPeerLocked(ep bip.Endpoint) bool {
	for _, e := range b.bdt {
		if e.Endpoint.Equal(ep) && !e.Endpoint.Equal(b.self) {
			return true
		}
	}
	return false
}

// isPeer reports whether from is a BDT peer other than this BBMD.
func (b *BBMD) isPeer(from bip.Endpoint) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.isPeerLocked(from)
}

// isPeerOneHop reports whether from is a BDT peer whose entry uses an
// all-ones mask, so that its Forwarded-NPDUs arrive by unicast and must be
// rebroadcast locally (h_bbmd.c bbmd_bdt_member_mask_is_unicast).
func (b *BBMD) isPeerOneHop(from bip.Endpoint) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, e := range b.bdt {
		if e.Endpoint.Equal(from) && !e.Endpoint.Equal(b.self) && oneHop(e) {
			return true
		}
	}
	return false
}

func (b *BBMD) registered(ep bip.Endpoint) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	f, ok := b.fdt[ep]
	return ok && b.now().Before(f.expires)
}

// confirmedRequest reports whether an NPDU carries a confirmed request,
// which is never distributed as a broadcast (as bacnet-stack, after the
// BTL test that a broadcast confirmed request is discarded).
func confirmedRequest(npdu []byte) bool {
	if len(npdu) < 2 {
		return false
	}
	ctl := npdu[1]
	if ctl&0x80 != 0 {
		return false // network layer message
	}
	off := 2
	if ctl&0x20 != 0 { // DNET, DLEN, DADR
		if len(npdu) < off+3 {
			return false
		}
		off += 3 + int(npdu[off+2])
	}
	if ctl&0x08 != 0 { // SNET, SLEN, SADR
		if len(npdu) < off+3 {
			return false
		}
		off += 3 + int(npdu[off+2])
	}
	if ctl&0x20 != 0 {
		off++ // hop count
	}
	return off < len(npdu) && npdu[off]&0xF0 == 0x00
}

// HandleBVLC implements server.BBMD.
func (b *BBMD) HandleBVLC(ctx context.Context, msg bvlc.Message, from bip.Endpoint, send sendFunc) (bool, bip.Endpoint) {
	if !from.IsValid() {
		// Only an IPv4 B/IP node can be answered, registered or named as
		// the origin of a Forwarded-NPDU.
		return false, bip.Endpoint{}
	}
	if from.Equal(b.self) {
		return false, bip.Endpoint{} // our own broadcast looped back
	}
	switch msg.Function {
	case bvlc.FunctionOriginalBroadcastNPDU:
		if confirmedRequest(msg.Payload) {
			return false, bip.Endpoint{}
		}
		_ = b.distribute(ctx, from, from, msg.Payload, true, send)
		return true, from
	case bvlc.FunctionForwardedNPDU:
		origin, _ := msg.OriginAddrPort()
		oep := bip.NewEndpoint(origin)
		if !oep.IsValid() || oep.Equal(b.self) || confirmedRequest(msg.Payload) {
			return false, bip.Endpoint{}
		}
		// Annex J.4.5: only a Forwarded-NPDU from a peer BBMD is
		// redistributed (rebroadcast for a one-hop peer, and sent to the
		// foreign devices). From any other node it is delivered to the
		// local device only, so that forwarded traffic never circulates.
		if !b.isPeer(from) {
			return true, oep
		}
		if b.isPeerOneHop(from) {
			frame, err := forwarded(oep, msg.Payload)
			if err == nil {
				_ = send(ctx, bip.OutboundPacket{Data: frame, Broadcast: true})
			}
		}
		_ = b.distribute(ctx, oep, from, msg.Payload, false, send)
		return true, oep
	case bvlc.FunctionDistributeBroadcastToNetwork:
		// Annex J.4.5: from a foreign device; an unregistered sender gets a
		// NAK.
		if !b.registered(from) || confirmedRequest(msg.Payload) {
			result(ctx, from, bvlc.ResultDistributeBroadcastToNetworkNAK, send)
			return false, bip.Endpoint{}
		}
		frame, err := forwarded(from, msg.Payload)
		if err != nil {
			result(ctx, from, bvlc.ResultDistributeBroadcastToNetworkNAK, send)
			return false, bip.Endpoint{}
		}
		_ = send(ctx, bip.OutboundPacket{Data: frame, Broadcast: true})
		_ = b.distribute(ctx, from, from, msg.Payload, true, send)
		return true, from
	case bvlc.FunctionRegisterForeignDevice:
		result(ctx, from, b.register(from, msg.TTL), send)
	case bvlc.FunctionReadBroadcastDistributionTable:
		body, err := bvlc.EncodeBDTEntries(nil, b.BDT())
		if err != nil {
			result(ctx, from, bvlc.ResultReadBroadcastDistributionTableNAK, send)
			break
		}
		frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionReadBroadcastDistributionTableAck, Payload: body})
		if err != nil {
			result(ctx, from, bvlc.ResultReadBroadcastDistributionTableNAK, send)
			break
		}
		_ = send(ctx, bip.OutboundPacket{Data: frame, Destination: from})
	case bvlc.FunctionWriteBroadcastDistributionTable:
		result(ctx, from, bvlc.ResultWriteBroadcastDistributionTableNAK, send)
	case bvlc.FunctionReadForeignDeviceTable:
		body, err := bvlc.EncodeFDTEntries(nil, b.FDT())
		if err != nil {
			result(ctx, from, bvlc.ResultReadForeignDeviceTableNAK, send)
			break
		}
		frame, err := bvlc.Append(nil, bvlc.Message{Function: bvlc.FunctionReadForeignDeviceTableAck, Payload: body})
		if err != nil {
			result(ctx, from, bvlc.ResultReadForeignDeviceTableNAK, send)
			break
		}
		_ = send(ctx, bip.OutboundPacket{Data: frame, Destination: from})
	case bvlc.FunctionDeleteForeignDeviceTableEntry:
		ep, err := bvlc.DecodeDeleteFDTEntry(msg.Payload)
		code := bvlc.ResultDeleteForeignDeviceTableEntryNAK
		if err == nil && b.delete(ep) {
			code = bvlc.ResultSuccessfulCompletion
		}
		result(ctx, from, code, send)
	default:
		// Original-Unicast-NPDU, BVLC-Result and the table ACKs are not
		// BBMD functions; the caller handles them.
	}
	return false, bip.Endpoint{}
}

func (b *BBMD) register(ep bip.Endpoint, ttl uint16) uint16 {
	if b.reject {
		return bvlc.ResultRegisterForeignDeviceNAK
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	// A peer BBMD (or this BBMD) receives broadcasts through the BDT; as a
	// foreign device too it would receive them twice and could send them
	// back.
	if ep.Equal(b.self) || b.isPeerLocked(ep) {
		return bvlc.ResultRegisterForeignDeviceNAK
	}
	now := b.now()
	b.fdtLocked(now) // drop expired entries first
	if _, known := b.fdt[ep]; !known && len(b.fdt) >= b.max {
		return bvlc.ResultRegisterForeignDeviceNAK
	}
	b.fdt[ep] = foreign{ttl: ttl, expires: now.Add(time.Duration(ttl)*time.Second + GracePeriod)}
	return bvlc.ResultSuccessfulCompletion
}

func (b *BBMD) delete(ep bip.Endpoint) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.fdt[ep]; !ok {
		return false
	}
	delete(b.fdt, ep)
	return true
}

// LocalBroadcast implements server.BBMD: a broadcast of the local device
// also goes to the peers and the foreign devices (Annex J.4.5).
func (b *BBMD) LocalBroadcast(ctx context.Context, npdu []byte, send sendFunc) {
	_ = b.distribute(ctx, b.self, b.self, npdu, true, send)
}
