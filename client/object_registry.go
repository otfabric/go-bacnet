// SPDX-License-Identifier: MIT

package client

import (
	"container/list"
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

type objectEntry struct {
	obs  ObjectObservation
	elem *list.Element // Value is the objectKey
}

// objectRegistry stores I-Have observations with the same retention policy
// shape as the device Registry (TTL, global cap). Entries sit on a recency
// list ordered by LastSeen, so expiry and eviction only look at its front.
type objectRegistry struct {
	mu      sync.RWMutex
	byKey   map[objectKey]*objectEntry
	recency *list.List
	diag    diag.Sink
	clock   clock.Clock
	opts    RegistryOptions

	// maintSteps counts entries examined by expiry and eviction (tests).
	maintSteps uint64
}

func newObjectRegistry(d diag.Sink, clk clock.Clock, opts RegistryOptions) *objectRegistry {
	if clk == nil {
		clk = clock.Real{}
	}
	return &objectRegistry{
		byKey:   make(map[objectKey]*objectEntry),
		recency: list.New(),
		diag:    d,
		clock:   clk,
		opts:    opts.withDefaults(),
	}
}

func (r *objectRegistry) Upsert(o ObjectObservation) {
	var events []diag.Event
	r.mu.Lock()
	now := r.clock.Now()
	o.LastSeen = now
	r.expireLocked(now, &events)
	k := objectKeyOf(o)
	if e, ok := r.byKey[k]; ok {
		e.obs = o
		r.recency.MoveToBack(e.elem)
	} else {
		r.byKey[k] = &objectEntry{obs: o, elem: r.recency.PushBack(k)}
	}
	r.enforceCapacityLocked(&events)
	r.mu.Unlock()
	for _, e := range events {
		r.diag.Report(e)
	}
}

func (r *objectRegistry) removeLocked(k objectKey) {
	if e, ok := r.byKey[k]; ok {
		r.recency.Remove(e.elem)
		delete(r.byKey, k)
	}
}

func (r *objectRegistry) expireLocked(now time.Time, events *[]diag.Event) {
	if r.opts.ObservationTTL <= 0 {
		return
	}
	cutoff := now.Add(-r.opts.ObservationTTL)
	for {
		front := r.recency.Front()
		if front == nil {
			return
		}
		r.maintSteps++
		k := front.Value.(objectKey)
		o := r.byKey[k].obs
		if !o.LastSeen.Before(cutoff) {
			return
		}
		r.removeLocked(k)
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

func (r *objectRegistry) enforceCapacityLocked(events *[]diag.Event) {
	if r.opts.MaxObservations < 0 {
		return
	}
	for len(r.byKey) > r.opts.MaxObservations {
		front := r.recency.Front()
		if front == nil {
			return
		}
		r.maintSteps++
		k := front.Value.(objectKey)
		o := r.byKey[k].obs
		r.removeLocked(k)
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
	for _, e := range r.byKey {
		out = append(out, e.obs)
	}
	return out
}

func (r *objectRegistry) ObservationsSince(since time.Time) []ObjectObservation {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ObjectObservation, 0, len(r.byKey))
	for _, e := range r.byKey {
		if !e.obs.LastSeen.Before(since) {
			out = append(out, e.obs)
		}
	}
	return out
}
