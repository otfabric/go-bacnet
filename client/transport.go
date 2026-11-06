// SPDX-License-Identifier: MIT

package client

import (
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/virtual"
)

// Transport is the data-link send/receive abstraction; see bip.Transport.
// One Client owns one Transport. A Transport passed to WithTransport is
// closed by Client.Close.
type Transport = bip.Transport

// InboundPacket is a received datagram; see bip.InboundPacket.
type InboundPacket = bip.InboundPacket

// OutboundPacket is a datagram to send; see bip.OutboundPacket.
type OutboundPacket = bip.OutboundPacket

func newUDPTransport(cfg config) (Transport, error) {
	return bip.ListenUDP(bip.UDPConfig{Port: cfg.port, LocalAddr: cfg.localAddr, Interface: cfg.iface})
}

// AdaptVirtual returns t as a client.Transport.
// virtual.Transport is internal; this helper is for in-module tests only.
func AdaptVirtual(t *virtual.Transport) Transport { return t }
