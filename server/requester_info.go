// SPDX-License-Identifier: MIT

package server

import (
	"context"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
)

// Requester is where a request the server is executing came from.
type Requester struct {
	// Endpoint is the B/IP node that originated the request: the
	// Forwarded-NPDU's originating address for a broadcast a BBMD
	// forwarded, else the node that sent the datagram. For a station on
	// another network that is its router; the station is Source.
	Endpoint bip.Endpoint
	// Via is the B/IP node the datagram came from: a router or a BBMD for
	// remote and forwarded traffic, else Endpoint.
	Via bip.Endpoint
	// Source is the NPDU source (SNET/SADR) of a station on another
	// network, reached through the router at Via; the zero Address for a
	// local station.
	Source bacnet.Address
}

type requesterKey struct{}

func withRequester(ctx context.Context, p peer) context.Context {
	return context.WithValue(ctx, requesterKey{}, Requester{Endpoint: p.origin, Via: p.immediate, Source: p.source})
}

// RequesterOf returns the requester of the request whose context ctx is: the
// context Hooks and objects are called with while the server executes a
// confirmed or unconfirmed request, a segmented one once reassembled. ok is
// false for any other context (a Schedule's write from the housekeeping
// goroutine, for example).
func RequesterOf(ctx context.Context) (r Requester, ok bool) {
	r, ok = ctx.Value(requesterKey{}).(Requester)
	return r, ok
}
