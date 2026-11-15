// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"sync"

	"github.com/otfabric/go-bacnet"
)

// Property describes one property of a Base object.
type Property struct {
	// Value is the stored value. An array is a constructed value with one
	// element per array item.
	Value bacnet.ApplicationValue
	// Get, when set, computes the value on every read instead of Value. It
	// runs with the object's lock held for reading.
	Get func() bacnet.ApplicationValue
	// Set, when set, handles writes instead of the default (which stores a
	// value of one of Kinds). It runs with the object's lock held and returns
	// the properties whose value changed.
	Set func(w Write) ([]bacnet.PropertyIdentifier, error)
	// Writable makes the property writable through WriteProperty.
	Writable bool
	// Kinds are the value kinds a default write accepts.
	Kinds []bacnet.ValueKind
	// Array marks a BACnetARRAY (array index access, clause 12).
	Array bool
	// Required marks the property as required for the object type.
	Required bool
}

// Base implements Object over a table of properties. Built-in objects embed
// it; applications may too. Object_Identifier, Object_Name, Object_Type and
// Property_List are always present.
//
// Lock order with the Device that holds the object: while mu is held the
// Device's table may be read (a Get that calls Device.Object, for example),
// and the Device never waits for mu while it holds its table lock.
type Base struct {
	mu        sync.RWMutex
	id        bacnet.ObjectIdentifier
	name      string
	props     map[bacnet.PropertyIdentifier]*Property
	order     []bacnet.PropertyIdentifier
	notify    Notifier
	deletable bool
}

// NewBase returns a Base with the four properties every object has.
func NewBase(id bacnet.ObjectIdentifier, name string) *Base {
	b := &Base{id: id, name: name, props: make(map[bacnet.PropertyIdentifier]*Property)}
	b.Define(bacnet.PropertyObjectIdentifier, &Property{Required: true, Get: func() bacnet.ApplicationValue { return bacnet.ObjectIDValue(b.id) }})
	b.Define(bacnet.PropertyObjectName, &Property{Required: true, Get: func() bacnet.ApplicationValue { return charString(b.name) }})
	b.Define(bacnet.PropertyObjectType, &Property{Required: true, Get: func() bacnet.ApplicationValue { return bacnet.EnumValue(uint32(b.id.Type)) }})
	b.Define(bacnet.PropertyPropertyList, &Property{Required: true, Array: true, Get: b.propertyList})
	return b
}

// Define adds or replaces a property. It is meant for construction; a
// property added after the object was given to a Device changes what
// ReadPropertyMultiple ALL returns from then on.
func (b *Base) Define(id bacnet.PropertyIdentifier, p *Property) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.props[id]; !ok {
		b.order = append(b.order, id)
	}
	b.props[id] = p
}

// SetDeletable allows DeleteObject to remove the object.
func (b *Base) SetDeletable(v bool) {
	b.mu.Lock()
	b.deletable = v
	b.mu.Unlock()
}

// Deletable reports whether DeleteObject may remove the object.
func (b *Base) Deletable() bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.deletable
}

// SetNotifier installs the change callback (done by Device.Add).
func (b *Base) SetNotifier(n Notifier) {
	b.mu.Lock()
	b.notify = n
	b.mu.Unlock()
}

// Identifier implements Object.
func (b *Base) Identifier() bacnet.ObjectIdentifier { return b.id }

// Name implements Object.
func (b *Base) Name() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.name
}

// Properties implements Object.
func (b *Base) Properties() ([]bacnet.PropertyIdentifier, map[bacnet.PropertyIdentifier]bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	all := append([]bacnet.PropertyIdentifier(nil), b.order...)
	req := make(map[bacnet.PropertyIdentifier]bool)
	for id, p := range b.props {
		if p.Required {
			req[id] = true
		}
	}
	return all, req
}

// propertyList is Property_List: every property except Object_Identifier,
// Object_Name, Object_Type and Property_List itself (clause 12, Property_List).
// Called with the lock held.
func (b *Base) propertyList() bacnet.ApplicationValue {
	var els []bacnet.Element
	for _, id := range b.order {
		switch id {
		case bacnet.PropertyObjectIdentifier, bacnet.PropertyObjectName, bacnet.PropertyObjectType, bacnet.PropertyPropertyList:
			continue
		default:
		}
		els = append(els, bacnet.Element{Value: bacnet.EnumValue(uint32(id))})
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
}

// ReadProperty implements Object.
func (b *Base) ReadProperty(_ context.Context, ref bacnet.PropertyReference) (bacnet.ApplicationValue, error) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.readLocked(ref)
}

func (b *Base) readLocked(ref bacnet.PropertyReference) (bacnet.ApplicationValue, error) {
	p, ok := b.props[ref.Identifier]
	if !ok {
		return bacnet.ApplicationValue{}, ErrUnknownProperty
	}
	v := p.Value
	if p.Get != nil {
		v = p.Get()
	}
	if ref.ArrayIndex == nil {
		return v.Clone(), nil
	}
	if !p.Array {
		return bacnet.ApplicationValue{}, ErrNotAnArray
	}
	return arrayItem(v, *ref.ArrayIndex)
}

// arrayItem returns item idx of an array value; 0 is the length.
func arrayItem(v bacnet.ApplicationValue, idx uint32) (bacnet.ApplicationValue, error) {
	n := uint32(len(v.Elements))
	if v.Kind != bacnet.ValueConstructed {
		n = 0
	}
	if idx == 0 {
		return bacnet.UnsignedValue(uint64(n)), nil
	}
	if idx > n {
		return bacnet.ApplicationValue{}, ErrInvalidArrayIndex
	}
	el := v.Elements[idx-1]
	if el.Context {
		return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{el}}.Clone(), nil
	}
	return el.Value.Clone(), nil
}

// WriteProperty implements Object.
func (b *Base) WriteProperty(_ context.Context, w Write) error {
	changed, notify, err := b.write(w)
	if err == nil && len(changed) > 0 && notify != nil {
		notify(Change{Object: b.id, Properties: changed})
	}
	return err
}

// write applies w with the lock held. The lock is released by a deferred
// call, so that a Set function of the application that does not return
// normally leaves the object usable.
func (b *Base) write(w Write) ([]bacnet.PropertyIdentifier, Notifier, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	changed, err := b.writeLocked(w)
	return changed, b.notify, err
}

func (b *Base) writeLocked(w Write) ([]bacnet.PropertyIdentifier, error) {
	p, ok := b.props[w.Property.Identifier]
	if !ok {
		return nil, ErrUnknownProperty
	}
	if !p.Writable {
		return nil, ErrWriteAccessDenied
	}
	if p.Set != nil {
		return p.Set(w)
	}
	return p.store(w)
}

// store is the default write of a property without a Set function: it
// stores a value of one of Kinds, or one item of an array. The caller holds
// the object's lock.
func (p *Property) store(w Write) ([]bacnet.PropertyIdentifier, error) {
	if w.Property.ArrayIndex != nil {
		if !p.Array {
			return nil, ErrNotAnArray
		}
		idx := *w.Property.ArrayIndex
		n := uint32(len(p.Value.Elements))
		if idx == 0 || idx > n {
			// Resizing an array through index 0 is not supported.
			return nil, ErrInvalidArrayIndex
		}
		if !kindAllowed(p.Kinds, w.Value.Kind) {
			return nil, ErrInvalidDataType
		}
		els := append([]bacnet.Element(nil), p.Value.Elements...)
		els[idx-1] = bacnet.Element{Value: w.Value.Clone()}
		p.Value = bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
		return []bacnet.PropertyIdentifier{w.Property.Identifier}, nil
	}
	if p.Array {
		if w.Value.Kind != bacnet.ValueConstructed {
			return nil, ErrInvalidDataType
		}
		for _, el := range w.Value.Elements {
			if el.Context || !kindAllowed(p.Kinds, el.Value.Kind) {
				return nil, ErrInvalidDataType
			}
		}
	} else if !kindAllowed(p.Kinds, w.Value.Kind) {
		return nil, ErrInvalidDataType
	}
	p.Value = w.Value.Clone()
	return []bacnet.PropertyIdentifier{w.Property.Identifier}, nil
}

func kindAllowed(kinds []bacnet.ValueKind, k bacnet.ValueKind) bool {
	for _, x := range kinds {
		if x == k {
			return true
		}
	}
	return false
}

// Update runs fn with the object's lock held and then reports the changed
// properties to the Device. Built-in objects use it for application-side
// updates (a new sensor reading, for example). The lock is released also
// when fn does not return normally.
func (b *Base) Update(fn func() []bacnet.PropertyIdentifier) {
	changed, notify := b.update(fn)
	if len(changed) > 0 && notify != nil {
		notify(Change{Object: b.id, Properties: changed})
	}
}

func (b *Base) update(fn func() []bacnet.PropertyIdentifier) ([]bacnet.PropertyIdentifier, Notifier) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return fn(), b.notify
}

// View runs fn with the object's lock held for reading.
func (b *Base) View(fn func()) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	fn()
}

// setName changes Object_Name; the Device checks uniqueness first.
func (b *Base) setName(name string) {
	b.mu.Lock()
	b.name = name
	b.mu.Unlock()
}

func charString(s string) bacnet.ApplicationValue {
	return bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString, Character: bacnet.CharacterString{Encoding: 0, Value: s}}
}

// CharString returns a CharacterString value (ANSI X3.4 / UTF-8, encoding 0).
func CharString(s string) bacnet.ApplicationValue { return charString(s) }

// statusFlags returns a BACnetStatusFlags value: in-alarm, fault,
// overridden, out-of-service (bits 0..3).
func statusFlags(inAlarm, fault, overridden, outOfService bool) bacnet.ApplicationValue {
	var b byte
	if inAlarm {
		b |= 0x80
	}
	if fault {
		b |= 0x40
	}
	if overridden {
		b |= 0x20
	}
	if outOfService {
		b |= 0x10
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: 4, Bytes: []byte{b}}}
}

// bitString builds a bit string of n bits from the set positions.
func bitString(n int, set func(i int) bool) bacnet.ApplicationValue {
	nbytes := (n + 7) / 8
	if nbytes == 0 {
		nbytes = 1
	}
	bs := make([]byte, nbytes)
	for i := 0; i < n; i++ {
		if set(i) {
			bs[i/8] |= 0x80 >> (i % 8)
		}
	}
	unused := uint8(nbytes*8 - n)
	if n == 0 {
		unused = 0
		bs = nil
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueBitString, BitString: bacnet.BitString{UnusedBits: unused, Bytes: bs}}
}

// bit reports bit i of a bit string value.
func bit(v bacnet.ApplicationValue, i int) bool {
	if v.Kind != bacnet.ValueBitString || i/8 >= len(v.BitString.Bytes) {
		return false
	}
	return v.BitString.Bytes[i/8]&(0x80>>(i%8)) != 0
}
