// SPDX-License-Identifier: MIT

package client

import (
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/diag"
)

// DeviceObservation is one sighting of a device announcement/path.
type DeviceObservation struct {
	Instance      uint32
	Address       bacnet.Address
	Origin        bip.Endpoint
	ImmediatePeer bip.Endpoint
	LastSeen      time.Time
	Capabilities  DeviceCapabilities
}

type observationKey struct {
	instance uint32
	addr     string
	origin   string
	peer     string
}

func keyOf(o DeviceObservation) observationKey {
	return observationKey{
		instance: o.Instance,
		addr:     o.Address.String(),
		origin:   o.Origin.String(),
		peer:     o.ImmediatePeer.String(),
	}
}

// Registry stores device observations with duplicate-instance diagnostics.
type Registry struct {
	mu         sync.RWMutex
	byKey      map[observationKey]DeviceObservation
	byInstance map[uint32][]observationKey
	diag       diag.Sink
}

func newRegistry(d diag.Sink) *Registry {
	return &Registry{
		byKey:      make(map[observationKey]DeviceObservation),
		byInstance: make(map[uint32][]observationKey),
		diag:       d,
	}
}

// Upsert records or refreshes an observation. Reports duplicate instances.
func (r *Registry) Upsert(o DeviceObservation) {
	var dupEvent *diag.Event
	r.mu.Lock()
	k := keyOf(o)
	prevKeys := r.byInstance[o.Instance]
	differentPath := false
	for _, pk := range prevKeys {
		if pk != k {
			differentPath = true
			break
		}
	}
	if differentPath {
		dupEvent = &diag.Event{
			Kind:    diag.KindDuplicateInstance,
			Message: "device instance announced from multiple addresses",
			Fields: map[string]any{
				"instance": o.Instance,
				"address":  o.Address.String(),
				"origin":   o.Origin.String(),
			},
		}
	}
	if old, ok := r.byKey[k]; ok {
		// Merge by source precedence so a fresh I-Am cannot overwrite
		// UserOverride / DeviceObject values.
		caps := old.Capabilities
		mergeCapabilities(&caps, o.Capabilities)
		o.Capabilities = caps
	}
	r.byKey[k] = o
	found := false
	for _, pk := range r.byInstance[o.Instance] {
		if pk == k {
			found = true
			break
		}
	}
	if !found {
		r.byInstance[o.Instance] = append(r.byInstance[o.Instance], k)
	}
	r.mu.Unlock()

	if dupEvent != nil {
		r.diag.Report(*dupEvent)
	}
}

func capabilityRank(s CapabilitySource) int {
	switch s {
	case CapabilityUserOverride:
		return 4
	case CapabilityFromDeviceObject:
		return 3
	case CapabilityFromIAm:
		return 2
	case CapabilityConservativeFallback:
		return 1
	default:
		return 0
	}
}

func mergeCapabilities(dst *DeviceCapabilities, src DeviceCapabilities) {
	mergeCap(&dst.ProtocolVersion, src.ProtocolVersion)
	mergeCap(&dst.ProtocolRevision, src.ProtocolRevision)
	mergeCap(&dst.MaxSegmentsAccepted, src.MaxSegmentsAccepted)
	mergeCap(&dst.MaxAPDULengthAccepted, src.MaxAPDULengthAccepted)
	mergeCap(&dst.Segmentation, src.Segmentation)
	mergeCap(&dst.VendorID, src.VendorID)
}

func mergeCap[T any](dst *Capability[T], src Capability[T]) {
	if !src.Known {
		return
	}
	if !dst.Known || capabilityRank(src.Source) >= capabilityRank(dst.Source) {
		*dst = src
	}
}

// ResolveCapabilities finds capabilities for a target by BACnet address and path.
func (r *Registry) ResolveCapabilities(target Target) (DeviceCapabilities, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, o := range r.byKey {
		if target.Address.Scope() == bacnet.AddressRemoteStation || target.Address.Scope() == bacnet.AddressLocalStation {
			if !o.Address.Equal(target.Address) {
				continue
			}
		}
		if target.Endpoint.IsValid() && o.ImmediatePeer.IsValid() && !o.ImmediatePeer.Equal(target.Endpoint) {
			continue
		}
		if target.Origin.IsValid() {
			if !o.Origin.IsValid() || !o.Origin.Equal(target.Origin) {
				continue
			}
		}
		return o.Capabilities, true
	}
	return DeviceCapabilities{}, false
}

// Observations returns a snapshot of all observations.
func (r *Registry) Observations() []DeviceObservation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]DeviceObservation, 0, len(r.byKey))
	for _, o := range r.byKey {
		out = append(out, o)
	}
	return out
}

// ByInstance returns all observations for a device instance.
func (r *Registry) ByInstance(instance uint32) []DeviceObservation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	keys := r.byInstance[instance]
	out := make([]DeviceObservation, 0, len(keys))
	for _, k := range keys {
		if o, ok := r.byKey[k]; ok {
			out = append(out, o)
		}
	}
	return out
}
