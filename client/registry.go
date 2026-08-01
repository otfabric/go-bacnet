// SPDX-License-Identifier: MIT

package client

import (
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/bip"
	"github.com/otfabric/go-bacnet/internal/clock"
	"github.com/otfabric/go-bacnet/internal/diag"
)

// Default registry retention policy (hostile-network safe defaults).
const (
	DefaultMaxObservations     = 4096
	DefaultMaxPathsPerInstance = 8
	DefaultObservationTTL      = 30 * time.Minute
)

// RegistryOptions bounds device observation retention.
//
// Zero fields select the package defaults. Negative Max* values disable that
// bound (not recommended on untrusted networks). A non-positive ObservationTTL
// disables TTL expiry.
type RegistryOptions struct {
	MaxObservations     int
	MaxPathsPerInstance int
	ObservationTTL      time.Duration
}

func (o RegistryOptions) withDefaults() RegistryOptions {
	if o.MaxObservations == 0 {
		o.MaxObservations = DefaultMaxObservations
	}
	if o.MaxPathsPerInstance == 0 {
		o.MaxPathsPerInstance = DefaultMaxPathsPerInstance
	}
	if o.ObservationTTL == 0 {
		o.ObservationTTL = DefaultObservationTTL
	}
	return o
}

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

// Registry stores device observations with duplicate-instance diagnostics
// and bounded retention.
type Registry struct {
	mu         sync.RWMutex
	byKey      map[observationKey]DeviceObservation
	byInstance map[uint32][]observationKey
	diag       diag.Sink
	clock      clock.Clock
	opts       RegistryOptions
}

func newRegistry(d diag.Sink, clk clock.Clock, opts RegistryOptions) *Registry {
	if clk == nil {
		clk = clock.Real{}
	}
	return &Registry{
		byKey:      make(map[observationKey]DeviceObservation),
		byInstance: make(map[uint32][]observationKey),
		diag:       d,
		clock:      clk,
		opts:       opts.withDefaults(),
	}
}

// Upsert records or refreshes an observation. Reports duplicate instances and
// enforces retention policy (TTL, per-instance path cap, global cap).
func (r *Registry) Upsert(o DeviceObservation) {
	var events []diag.Event
	r.mu.Lock()
	now := r.clock.Now()
	o.LastSeen = now
	r.expireLocked(now, &events)

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
		events = append(events, diag.Event{
			Kind:    diag.KindDuplicateInstance,
			Message: "device instance announced from multiple addresses",
			Fields: map[string]any{
				"instance": o.Instance,
				"address":  o.Address.String(),
				"origin":   o.Origin.String(),
			},
		})
	}
	if old, ok := r.byKey[k]; ok {
		caps := old.Capabilities
		mergeCapabilities(&caps, o.Capabilities)
		o.Capabilities = caps
	} else {
		r.enforceCapacityLocked(o.Instance, &events)
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

	for _, ev := range events {
		r.diag.Report(ev)
	}
}

func (r *Registry) expireLocked(now time.Time, events *[]diag.Event) {
	ttl := r.opts.ObservationTTL
	if ttl <= 0 {
		return
	}
	for k, o := range r.byKey {
		if now.Sub(o.LastSeen) >= ttl {
			r.removeKeyLocked(k)
			*events = append(*events, diag.Event{
				Kind:    diag.KindRegistryEviction,
				Message: "observation expired",
				Fields: map[string]any{
					"instance": o.Instance,
					"reason":   "ttl",
				},
			})
		}
	}
}

func (r *Registry) enforceCapacityLocked(instance uint32, events *[]diag.Event) {
	maxPaths := r.opts.MaxPathsPerInstance
	if maxPaths > 0 {
		keys := r.byInstance[instance]
		for len(keys) >= maxPaths {
			victim, ok := r.leastRecentlySeenLocked(keys)
			if !ok {
				break
			}
			r.removeKeyLocked(victim)
			*events = append(*events, diag.Event{
				Kind:    diag.KindRegistryEviction,
				Message: "per-instance path limit",
				Fields: map[string]any{
					"instance": instance,
					"reason":   "max_paths_per_instance",
					"limit":    maxPaths,
				},
			})
			keys = r.byInstance[instance]
		}
	}

	maxObs := r.opts.MaxObservations
	if maxObs > 0 {
		for len(r.byKey) >= maxObs {
			victim, ok := r.leastRecentlySeenLocked(nil)
			if !ok {
				break
			}
			inst := victim.instance
			r.removeKeyLocked(victim)
			*events = append(*events, diag.Event{
				Kind:    diag.KindRegistryEviction,
				Message: "global observation limit",
				Fields: map[string]any{
					"instance": inst,
					"reason":   "max_observations",
					"limit":    maxObs,
				},
			})
		}
	}
}

func (r *Registry) leastRecentlySeenLocked(keys []observationKey) (observationKey, bool) {
	var (
		found bool
		best  observationKey
		bestT time.Time
	)
	consider := func(k observationKey) {
		o, ok := r.byKey[k]
		if !ok {
			return
		}
		if !found || o.LastSeen.Before(bestT) {
			found = true
			best = k
			bestT = o.LastSeen
		}
	}
	if keys == nil {
		for k := range r.byKey {
			consider(k)
		}
	} else {
		for _, k := range keys {
			consider(k)
		}
	}
	return best, found
}

func (r *Registry) removeKeyLocked(k observationKey) {
	delete(r.byKey, k)
	keys := r.byInstance[k.instance]
	if len(keys) == 0 {
		return
	}
	out := keys[:0]
	for _, pk := range keys {
		if pk != k {
			out = append(out, pk)
		}
	}
	if len(out) == 0 {
		delete(r.byInstance, k.instance)
	} else {
		r.byInstance[k.instance] = out
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

// ObservationsSince returns observations with LastSeen at or after since.
func (r *Registry) ObservationsSince(since time.Time) []DeviceObservation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]DeviceObservation, 0, len(r.byKey))
	for _, o := range r.byKey {
		if !o.LastSeen.Before(since) {
			out = append(out, o)
		}
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

// Len returns the number of retained observations (test/helper).
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byKey)
}
