// SPDX-License-Identifier: MIT

package client

import (
	"container/list"
	"slices"
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

// Target returns the Target that addresses the observed device over the path
// it was seen on, with the Max APDU it announced. For a device seen through a
// BBMD (Forwarded-NPDU) requests go straight to the device's own address.
func (o DeviceObservation) Target() Target {
	t := Target{Address: o.Address, Endpoint: o.ImmediatePeer}
	if o.Origin.IsValid() && !o.Origin.Equal(o.ImmediatePeer) {
		switch o.Address.Scope() {
		case bacnet.AddressRemoteStation, bacnet.AddressRemoteBroadcast:
			// Routed: the immediate peer is the router and stays the next hop.
		default:
			t.Endpoint = o.Origin
		}
	}
	if o.Capabilities.MaxAPDULengthAccepted.Known {
		t.MaxAPDU = o.Capabilities.MaxAPDULengthAccepted.Value
	}
	return t
}

// DeviceID returns the Device object identifier of the observed device.
func (o DeviceObservation) DeviceID() bacnet.ObjectIdentifier {
	return bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: o.Instance}
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

// registryEntry is one retained observation and its position in the
// recency list.
type registryEntry struct {
	obs  DeviceObservation
	elem *list.Element // Value is the observationKey
}

// Registry stores device observations with duplicate-instance diagnostics
// and bounded retention.
//
// Observations are kept on a recency list ordered by LastSeen (least recently
// seen first), so TTL expiry and global-cap eviction only look at the front
// of the list: per-Upsert maintenance is O(1) amortised, independent of the
// number of retained observations.
type Registry struct {
	mu         sync.RWMutex
	byKey      map[observationKey]*registryEntry
	byInstance map[uint32][]observationKey
	recency    *list.List
	diag       diag.Sink
	clock      clock.Clock
	opts       RegistryOptions

	// maintSteps counts entries examined by expiry and eviction; tests use
	// it to check that maintenance work does not grow with the table size.
	maintSteps uint64
}

func newRegistry(d diag.Sink, clk clock.Clock, opts RegistryOptions) *Registry {
	if clk == nil {
		clk = clock.Real{}
	}
	return &Registry{
		byKey:      make(map[observationKey]*registryEntry),
		byInstance: make(map[uint32][]observationKey),
		recency:    list.New(),
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
	if e, ok := r.byKey[k]; ok {
		caps := e.obs.Capabilities
		mergeCapabilities(&caps, o.Capabilities)
		o.Capabilities = caps
		e.obs = o
		r.recency.MoveToBack(e.elem)
	} else {
		r.enforceCapacityLocked(o.Instance, &events)
		r.byKey[k] = &registryEntry{obs: o, elem: r.recency.PushBack(k)}
	}
	if !slices.Contains(r.byInstance[o.Instance], k) {
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
	for {
		front := r.recency.Front()
		if front == nil {
			return
		}
		r.maintSteps++
		k := front.Value.(observationKey)
		e := r.byKey[k]
		if now.Sub(e.obs.LastSeen) < ttl {
			return
		}
		r.removeKeyLocked(k)
		*events = append(*events, diag.Event{
			Kind:    diag.KindRegistryEviction,
			Message: "observation expired",
			Fields: map[string]any{
				"instance": e.obs.Instance,
				"reason":   "ttl",
			},
		})
	}
}

func (r *Registry) enforceCapacityLocked(instance uint32, events *[]diag.Event) {
	maxPaths := r.opts.MaxPathsPerInstance
	if maxPaths > 0 {
		for len(r.byInstance[instance]) >= maxPaths {
			victim, ok := r.leastRecentlySeenOfLocked(r.byInstance[instance])
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
		}
	}

	maxObs := r.opts.MaxObservations
	if maxObs > 0 {
		for len(r.byKey) >= maxObs {
			front := r.recency.Front()
			if front == nil {
				break
			}
			r.maintSteps++
			victim := front.Value.(observationKey)
			r.removeKeyLocked(victim)
			*events = append(*events, diag.Event{
				Kind:    diag.KindRegistryEviction,
				Message: "global observation limit",
				Fields: map[string]any{
					"instance": victim.instance,
					"reason":   "max_observations",
					"limit":    maxObs,
				},
			})
		}
	}
}

// leastRecentlySeenOfLocked picks the least recently seen key among keys (the
// paths of one instance, bounded by MaxPathsPerInstance).
func (r *Registry) leastRecentlySeenOfLocked(keys []observationKey) (observationKey, bool) {
	var (
		found bool
		best  observationKey
		bestT time.Time
	)
	for _, k := range keys {
		r.maintSteps++
		e, ok := r.byKey[k]
		if !ok {
			continue
		}
		if !found || e.obs.LastSeen.Before(bestT) {
			found = true
			best = k
			bestT = e.obs.LastSeen
		}
	}
	return best, found
}

func (r *Registry) removeKeyLocked(k observationKey) {
	if e, ok := r.byKey[k]; ok {
		r.recency.Remove(e.elem)
		delete(r.byKey, k)
	}
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
	for _, e := range r.byKey {
		o := e.obs
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
	for _, e := range r.byKey {
		out = append(out, e.obs)
	}
	return out
}

// ObservationsSince returns observations with LastSeen at or after since.
func (r *Registry) ObservationsSince(since time.Time) []DeviceObservation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]DeviceObservation, 0, len(r.byKey))
	for _, e := range r.byKey {
		if !e.obs.LastSeen.Before(since) {
			out = append(out, e.obs)
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
		if e, ok := r.byKey[k]; ok {
			out = append(out, e.obs)
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
