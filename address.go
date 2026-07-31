// SPDX-License-Identifier: MIT

package bacnet

import "fmt"

// AddressScope classifies a BACnet network address.
type AddressScope uint8

const (
	AddressLocalStation AddressScope = iota
	AddressLocalBroadcast
	AddressRemoteStation
	AddressRemoteBroadcast
	AddressGlobalBroadcast
)

// Address is a data-link-independent BACnet network address.
type Address struct {
	network uint16
	mac     MAC
	scope   AddressScope
}

// LocalStation returns a local unicast BACnet address.
func LocalStation(mac MAC) Address {
	return Address{scope: AddressLocalStation, mac: mac}
}

// LocalBroadcast returns a local broadcast address.
func LocalBroadcast() Address {
	return Address{scope: AddressLocalBroadcast}
}

// RemoteStation returns a remote unicast address on network.
func RemoteStation(network uint16, mac MAC) Address {
	return Address{scope: AddressRemoteStation, network: network, mac: mac}
}

// RemoteBroadcast returns a remote broadcast on network.
func RemoteBroadcast(network uint16) Address {
	return Address{scope: AddressRemoteBroadcast, network: network}
}

// GlobalBroadcast returns the BACnet global broadcast address.
func GlobalBroadcast() Address {
	return Address{scope: AddressGlobalBroadcast}
}

// Scope returns the address scope.
func (a Address) Scope() AddressScope { return a.scope }

// Network returns the BACnet network number when applicable.
func (a Address) Network() uint16 { return a.network }

// MAC returns the station MAC when applicable.
func (a Address) MAC() MAC { return a.mac }

// IsBroadcast reports whether the address is any broadcast form.
func (a Address) IsBroadcast() bool {
	switch a.scope {
	case AddressLocalBroadcast, AddressRemoteBroadcast, AddressGlobalBroadcast:
		return true
	default:
		return false
	}
}

// Equal reports whether a and o denote the same BACnet address.
func (a Address) Equal(o Address) bool { return a == o }

// String returns a human-readable form.
func (a Address) String() string {
	switch a.scope {
	case AddressLocalStation:
		return fmt.Sprintf("local:%s", a.mac)
	case AddressLocalBroadcast:
		return "local-broadcast"
	case AddressRemoteStation:
		return fmt.Sprintf("net=%d:%s", a.network, a.mac)
	case AddressRemoteBroadcast:
		return fmt.Sprintf("net=%d:broadcast", a.network)
	case AddressGlobalBroadcast:
		return "global-broadcast"
	default:
		return fmt.Sprintf("address(scope=%d)", a.scope)
	}
}
