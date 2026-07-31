// SPDX-License-Identifier: MIT

package client

// CapabilitySource identifies how a capability value was obtained.
type CapabilitySource uint8

const (
	CapabilityUnknown CapabilitySource = iota
	CapabilityFromIAm
	CapabilityFromDeviceObject
	CapabilityUserOverride
	CapabilityConservativeFallback
)

// Capability is a value with explicit known/provenance state.
type Capability[T any] struct {
	Value  T
	Known  bool
	Source CapabilitySource
}

// DeviceCapabilities tracks remote device protocol capabilities.
//
// I-Am seeds only Device instance (via observation), MaxAPDULengthAccepted,
// Segmentation and VendorID. ProtocolVersion/Revision remain unknown until
// Device-object reads or explicit configuration.
type DeviceCapabilities struct {
	MaxAPDULengthAccepted Capability[uint16]
	Segmentation          Capability[uint8]
	MaxSegmentsAccepted   Capability[uint8]
	ProtocolVersion       Capability[uint8]
	ProtocolRevision      Capability[uint8]
	VendorID              Capability[uint16]
}

// SetIAmFields records capabilities carried by I-Am only.
func (c *DeviceCapabilities) SetIAmFields(maxAPDU uint16, segmentation uint8, vendorID uint16) {
	c.MaxAPDULengthAccepted = Capability[uint16]{Value: maxAPDU, Known: true, Source: CapabilityFromIAm}
	c.Segmentation = Capability[uint8]{Value: segmentation, Known: true, Source: CapabilityFromIAm}
	c.VendorID = Capability[uint16]{Value: vendorID, Known: true, Source: CapabilityFromIAm}
}

// EnsureFallbackMaxAPDU sets a conservative max APDU if unknown.
func (c *DeviceCapabilities) EnsureFallbackMaxAPDU(v uint16) {
	if !c.MaxAPDULengthAccepted.Known {
		c.MaxAPDULengthAccepted = Capability[uint16]{
			Value:  v,
			Known:  true,
			Source: CapabilityConservativeFallback,
		}
	}
}

// MaxAPDUOr returns the known max APDU or fallback.
func (c DeviceCapabilities) MaxAPDUOr(fallback uint16) uint16 {
	if c.MaxAPDULengthAccepted.Known {
		return c.MaxAPDULengthAccepted.Value
	}
	return fallback
}
