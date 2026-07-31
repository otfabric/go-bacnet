// SPDX-License-Identifier: MIT

package client

import "github.com/otfabric/go-bacnet"

// matchTargetSource reports whether src is an acceptable path for traffic that
// should come from target. Used by confirmed-response matching and COV delivery
// so routed remote-station checks require both the BACnet address and the
// expected immediate next hop when known.
func matchTargetSource(target Target, src packetSource) bool {
	switch target.Address.Scope() {
	case bacnet.AddressRemoteStation, bacnet.AddressRemoteBroadcast:
		if !src.bacnetAddress.Equal(target.Address) {
			return false
		}
		if target.Endpoint.IsValid() && src.immediate.IsValid() && !target.Endpoint.Equal(src.immediate) {
			return false
		}
		return true
	case bacnet.AddressLocalStation, bacnet.AddressLocalBroadcast, bacnet.AddressGlobalBroadcast:
		// Forwarded NPDU: require claimed origin and expected immediate BBMD path.
		if target.Origin.IsValid() {
			if !src.origin.IsValid() || !target.Origin.Equal(src.origin) {
				return false
			}
			if target.Endpoint.IsValid() && src.immediate.IsValid() && !target.Endpoint.Equal(src.immediate) {
				return false
			}
			return true
		}
		if src.bacnetAddress.Scope() == bacnet.AddressLocalStation || src.bacnetAddress.Scope() == bacnet.AddressRemoteStation {
			if !src.bacnetAddress.Equal(target.Address) {
				return false
			}
		}
		if target.Endpoint.IsValid() && src.immediate.IsValid() && !target.Endpoint.Equal(src.immediate) {
			return false
		}
		return true
	default:
		return false
	}
}
