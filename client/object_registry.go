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

// ObjectObservation is one I-Have sighting of an object name/path.
type ObjectObservation struct {
	DeviceInstance uint32
	Object         bacnet.ObjectIdentifier
	Name           bacnet.CharacterString
	Address        bacnet.Address
	Origin         bip.Endpoint
	ImmediatePeer  bip.Endpoint
	LastSeen       time.Time
}

type objectKey struct {
	device   uint32
	objType  uint16
	instance uint32
	addr     string
	origin   string
	peer     string
}

func objectKeyOf(o ObjectObservation) objectKey {
	return objectKey{
		device:   o.DeviceInstance,
		objType:  uint16(o.Object.Type),
		instance: o.Object.Instance,
		addr:     o.Address.String(),
		origin:   o.Origin.String(),
		peer:     o.ImmediatePeer.String(),
	}
}

// objectRegistry stores I-Have observations with the same retention policy
// shape as the device Registry (TTL, global cap).
type objectRegistry struct {
	mu    sync.RWMutex
	byKey map[objectKey]ObjectObservation
	diag  diag.Sink
	clock clock.Clock
	opts  RegistryOptions
}

func newObjectRegistry(d diag.Sink, clk clock.Clock, opts RegistryOptions) *objectRegistry {
	if clk == nil {
		clk = clock.Real{}
	}
	return &objectRegistry{
		byKey: make(map[objectKey]ObjectObservation),
		diag:  d,
		clock: clk,
		opts:  opts.withDefaults(),
	}
}

func (r *objectRegistry) Upsert(o ObjectObservation) {
	var events []diag.Event
	r.mu.Lock()
	now := r.clock.Now()
	o.LastSeen = now
	r.expireLocked(now, &events)
	k := objectKeyOf(o)
	r.byKey[k] = o
	r.enforceCapacityLocked(&events)
	r.mu.Unlock()
	for _, e := range events {
		r.diag.Report(e)
	}
}

func (r *objectRegistry) expireLocked(now time.Time, events *[]diag.Event) {
	if r.opts.ObservationTTL <= 0 {
		return
	}
	cutoff := now.Add(-r.opts.ObservationTTL)
	for k, o := range r.byKey {
		if o.LastSeen.Before(cutoff) {
			delete(r.byKey, k)
			*events = append(*events, diag.Event{
				Kind:    diag.KindRegistryEviction,
				Message: "object observation expired",
				Fields: map[string]any{
					"device":   o.DeviceInstance,
					"object":   o.Object.String(),
					"name":     o.Name.Value,
					"lastSeen": o.LastSeen,
				},
			})
		}
	}
}

func (r *objectRegistry) enforceCapacityLocked(events *[]diag.Event) {
	if r.opts.MaxObservations < 0 {
		return
	}
	for len(r.byKey) > r.opts.MaxObservations {
		var oldestKey objectKey
		var oldestTime time.Time
		first := true
		for k, o := range r.byKey {
			if first || o.LastSeen.Before(oldestTime) {
				oldestKey = k
				oldestTime = o.LastSeen
				first = false
			}
		}
		o := r.byKey[oldestKey]
		delete(r.byKey, oldestKey)
		*events = append(*events, diag.Event{
			Kind:    diag.KindRegistryEviction,
			Message: "object observation capacity eviction",
			Fields: map[string]any{
				"device": o.DeviceInstance,
				"object": o.Object.String(),
				"name":   o.Name.Value,
			},
		})
	}
}

func (r *objectRegistry) Observations() []ObjectObservation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ObjectObservation, 0, len(r.byKey))
	for _, o := range r.byKey {
		out = append(out, o)
	}
	return out
}

func (r *objectRegistry) ObservationsSince(since time.Time) []ObjectObservation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ObjectObservation, 0, len(r.byKey))
	for _, o := range r.byKey {
		if !o.LastSeen.Before(since) {
			out = append(out, o)
		}
	}
	return out
}
