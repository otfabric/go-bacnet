// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/otfabric/go-bacnet"
)

// Device system status values (BACnetDeviceStatus).
const (
	StatusOperational         uint32 = 0
	StatusOperationalReadOnly uint32 = 1
	StatusNonOperational      uint32 = 4
)

// Protocol constants of the Device object.
const (
	// ProtocolVersion is the BACnet protocol version (clause 12.11).
	ProtocolVersion = 1
	// DefaultProtocolRevision is the revision of the baseline edition
	// (docs/STANDARD_BASELINE.md).
	DefaultProtocolRevision = 31
	// servicesSupportedBits and objectTypesSupportedBits are the lengths of
	// Protocol_Services_Supported and Protocol_Object_Types_Supported for
	// that revision (as bacnet-stack sizes them for revisions 29 and later).
	servicesSupportedBits    = 50
	objectTypesSupportedBits = 65
)

// Runtime is what the server provides to its Device: values of Device
// properties that belong to the protocol engine, not to the object model.
type Runtime interface {
	// ServicesSupported returns the bit positions of
	// Protocol_Services_Supported (BACnetServicesSupported) that are set.
	ServicesSupported() []int
	MaxAPDULengthAccepted() uint16
	// Segmentation returns BACnetSegmentation: 0 both, 3 none.
	Segmentation() uint32
	MaxSegmentsAccepted() uint32
	APDUTimeout() time.Duration
	APDUSegmentTimeout() time.Duration
	APDURetries() int
	// ActiveCOVSubscriptions returns the encoded list of
	// BACnetCOVSubscription (each one element per field group).
	ActiveCOVSubscriptions() []bacnet.Element
	Now() time.Time
}

// DeviceConfig configures a Device object.
type DeviceConfig struct {
	Instance                   uint32
	Name                       string
	VendorID                   uint16
	VendorName                 string
	ModelName                  string
	FirmwareRevision           string
	ApplicationSoftwareVersion string
	Description                string
	Location                   string
	SerialNumber               string
	// ProtocolRevision defaults to DefaultProtocolRevision.
	ProtocolRevision uint32
	// MaxObjects bounds the object table, the Device included (default
	// 65535); CreateObject beyond it fails with resources/no-space-for-object.
	MaxObjects int
}

// Factory creates an object for CreateObject. instance is the requested or
// chosen instance; the initial values are applied by the server afterwards
// with WriteProperty.
type Factory func(instance uint32, name string) (Object, error)

// Device is the Device object and the table of every object of the device.
type Device struct {
	*Base
	cfg DeviceConfig

	tmu       sync.RWMutex
	objects   map[bacnet.ObjectIdentifier]Object
	names     map[string]bacnet.ObjectIdentifier
	revision  uint32
	factories map[bacnet.ObjectType]Factory
	status    uint32

	omu       sync.Mutex
	observers map[int]Notifier
	nextObs   int

	rt Runtime
}

// NewDevice returns a Device object with an empty object table.
func NewDevice(cfg DeviceConfig) (*Device, error) {
	if cfg.Instance >= bacnet.MaxObjectInstance {
		return nil, errorf("device instance %d out of range", cfg.Instance)
	}
	if cfg.Name == "" {
		return nil, errorf("device name required")
	}
	if cfg.ProtocolRevision == 0 {
		cfg.ProtocolRevision = DefaultProtocolRevision
	}
	if cfg.MaxObjects <= 0 {
		cfg.MaxObjects = 65535
	}
	id := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: cfg.Instance}
	d := &Device{
		Base:      NewBase(id, cfg.Name),
		cfg:       cfg,
		objects:   make(map[bacnet.ObjectIdentifier]Object),
		names:     make(map[string]bacnet.ObjectIdentifier),
		factories: make(map[bacnet.ObjectType]Factory),
		observers: make(map[int]Notifier),
	}
	d.objects[id] = d
	d.names[cfg.Name] = id
	d.defineProperties()
	return d, nil
}

func (d *Device) defineProperties() {
	str := func(s string) *Property { return &Property{Required: true, Value: charString(s)} }
	d.Define(bacnet.PropertySystemStatus, &Property{Required: true, Get: func() bacnet.ApplicationValue { return bacnet.EnumValue(d.systemStatus()) }})
	d.Define(bacnet.PropertyVendorName, str(d.cfg.VendorName))
	d.Define(bacnet.PropertyVendorIdentifier, &Property{Required: true, Value: bacnet.UnsignedValue(uint64(d.cfg.VendorID))})
	d.Define(bacnet.PropertyModelName, str(d.cfg.ModelName))
	d.Define(bacnet.PropertyFirmwareRevision, str(d.cfg.FirmwareRevision))
	d.Define(bacnet.PropertyApplicationSoftwareVersion, str(d.cfg.ApplicationSoftwareVersion))
	if d.cfg.Location != "" {
		d.Define(bacnet.PropertyLocation, &Property{Value: charString(d.cfg.Location), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
	}
	if d.cfg.Description != "" {
		d.Define(bacnet.PropertyDescription, &Property{Value: charString(d.cfg.Description), Writable: true, Kinds: []bacnet.ValueKind{bacnet.ValueCharacterString}})
	}
	if d.cfg.SerialNumber != "" {
		d.Define(bacnet.PropertySerialNumber, &Property{Value: charString(d.cfg.SerialNumber)})
	}
	d.Define(bacnet.PropertyProtocolVersion, &Property{Required: true, Value: bacnet.UnsignedValue(ProtocolVersion)})
	d.Define(bacnet.PropertyProtocolRevision, &Property{Required: true, Value: bacnet.UnsignedValue(uint64(d.cfg.ProtocolRevision))})
	d.Define(bacnet.PropertyProtocolServicesSupported, &Property{Required: true, Get: d.servicesSupported})
	d.Define(bacnet.PropertyProtocolObjectTypesSupported, &Property{Required: true, Get: d.objectTypesSupported})
	d.Define(bacnet.PropertyObjectList, &Property{Required: true, Array: true, Get: d.objectList})
	d.Define(bacnet.PropertyMaxAPDULength, &Property{Required: true, Get: func() bacnet.ApplicationValue {
		return bacnet.UnsignedValue(uint64(d.runtimeOr(func(r Runtime) any { return r.MaxAPDULengthAccepted() }, uint16(1476)).(uint16)))
	}})
	d.Define(bacnet.PropertySegmentation, &Property{Required: true, Get: func() bacnet.ApplicationValue {
		return bacnet.EnumValue(d.runtimeOr(func(r Runtime) any { return r.Segmentation() }, uint32(3)).(uint32))
	}})
	d.Define(bacnet.PropertyMaxSegmentsAccepted, &Property{Get: func() bacnet.ApplicationValue {
		return bacnet.UnsignedValue(uint64(d.runtimeOr(func(r Runtime) any { return r.MaxSegmentsAccepted() }, uint32(0)).(uint32)))
	}})
	d.Define(bacnet.PropertyAPDUSegmentTimeout, &Property{Get: func() bacnet.ApplicationValue {
		return bacnet.UnsignedValue(uint64(d.runtimeOr(func(r Runtime) any { return r.APDUSegmentTimeout() }, 2*time.Second).(time.Duration).Milliseconds()))
	}})
	d.Define(bacnet.PropertyAPDUTimeout, &Property{Required: true, Get: func() bacnet.ApplicationValue {
		return bacnet.UnsignedValue(uint64(d.runtimeOr(func(r Runtime) any { return r.APDUTimeout() }, 3*time.Second).(time.Duration).Milliseconds()))
	}})
	d.Define(bacnet.PropertyNumberOfAPDURetries, &Property{Required: true, Get: func() bacnet.ApplicationValue {
		return bacnet.UnsignedValue(uint64(d.runtimeOr(func(r Runtime) any { return r.APDURetries() }, 3).(int)))
	}})
	d.Define(bacnet.PropertyDeviceAddressBinding, &Property{Required: true, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed}})
	d.Define(bacnet.PropertyDatabaseRevision, &Property{Required: true, Get: func() bacnet.ApplicationValue {
		return bacnet.UnsignedValue(uint64(d.DatabaseRevision()))
	}})
	d.Define(bacnet.PropertyLocalDate, &Property{Get: func() bacnet.ApplicationValue { return bacnet.DateValue(dateOf(d.now())) }})
	d.Define(bacnet.PropertyLocalTime, &Property{Get: func() bacnet.ApplicationValue { return bacnet.TimeValue(timeOf(d.now())) }})
	d.Define(bacnet.PropertyActiveCOVSubscriptions, &Property{Get: func() bacnet.ApplicationValue {
		var els []bacnet.Element
		if r := d.runtime(); r != nil {
			els = r.ActiveCOVSubscriptions()
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
	}})
}

// Config returns the configuration the Device was built with.
func (d *Device) Config() DeviceConfig { return d.cfg }

// Bind connects the Device to the server that serves it. The server calls
// it; a Device can be served by one server at a time.
func (d *Device) Bind(rt Runtime) {
	d.tmu.Lock()
	d.rt = rt
	d.tmu.Unlock()
}

func (d *Device) runtime() Runtime {
	d.tmu.RLock()
	defer d.tmu.RUnlock()
	return d.rt
}

func (d *Device) runtimeOr(fn func(Runtime) any, def any) any {
	if r := d.runtime(); r != nil {
		return fn(r)
	}
	return def
}

func (d *Device) now() time.Time {
	if r := d.runtime(); r != nil {
		return r.Now()
	}
	return time.Now()
}

func (d *Device) systemStatus() uint32 {
	d.tmu.RLock()
	defer d.tmu.RUnlock()
	return d.status
}

// SetSystemStatus sets System_Status.
func (d *Device) SetSystemStatus(s uint32) {
	d.tmu.Lock()
	d.status = s
	d.tmu.Unlock()
}

func (d *Device) servicesSupported() bacnet.ApplicationValue {
	set := map[int]bool{}
	if r := d.runtime(); r != nil {
		for _, b := range r.ServicesSupported() {
			set[b] = true
		}
	}
	return bitString(servicesSupportedBits, func(i int) bool { return set[i] })
}

func (d *Device) objectTypesSupported() bacnet.ApplicationValue {
	set := map[int]bool{}
	d.tmu.RLock()
	for id := range d.objects {
		set[int(id.Type)] = true
	}
	for t := range d.factories {
		set[int(t)] = true
	}
	d.tmu.RUnlock()
	return bitString(objectTypesSupportedBits, func(i int) bool { return set[i] })
}

// objectList is Object_List: the Device first, then by type and instance.
func (d *Device) objectList() bacnet.ApplicationValue {
	ids := d.ObjectIdentifiers()
	els := make([]bacnet.Element, len(ids))
	for i, id := range ids {
		els[i] = bacnet.Element{Value: bacnet.ObjectIDValue(id)}
	}
	return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
}

// ObjectIdentifiers returns every object, the Device first.
func (d *Device) ObjectIdentifiers() []bacnet.ObjectIdentifier {
	d.tmu.RLock()
	ids := make([]bacnet.ObjectIdentifier, 0, len(d.objects))
	for id := range d.objects {
		if id != d.Identifier() {
			ids = append(ids, id)
		}
	}
	d.tmu.RUnlock()
	sort.Slice(ids, func(i, j int) bool {
		if ids[i].Type != ids[j].Type {
			return ids[i].Type < ids[j].Type
		}
		return ids[i].Instance < ids[j].Instance
	})
	return append([]bacnet.ObjectIdentifier{d.Identifier()}, ids...)
}

// DatabaseRevision returns Database_Revision. It increases when an object
// is added, removed or renamed (clause 12.11).
func (d *Device) DatabaseRevision() uint32 {
	d.tmu.RLock()
	defer d.tmu.RUnlock()
	return d.revision
}

// Add adds an object. Identifier and name must be unique in the device
// (clause 12.1); the Device installs its change notifier in the object.
func (d *Device) Add(o Object) error {
	id := o.Identifier()
	name := o.Name()
	if id.Type == bacnet.ObjectTypeDevice {
		return errorf("a device holds one Device object")
	}
	if name == "" {
		return errorf("object %v has no name", id)
	}
	d.tmu.Lock()
	if _, ok := d.objects[id]; ok {
		d.tmu.Unlock()
		return ErrObjectExists
	}
	if _, ok := d.names[name]; ok {
		d.tmu.Unlock()
		return ErrDuplicateName
	}
	if len(d.objects) >= d.cfg.MaxObjects {
		d.tmu.Unlock()
		return ErrNoSpaceForObject
	}
	d.objects[id] = o
	d.names[name] = id
	d.revision++
	d.tmu.Unlock()
	if n, ok := o.(Notifying); ok {
		n.SetNotifier(d.dispatch)
	}
	d.dispatch(Change{Object: d.Identifier(), Properties: []bacnet.PropertyIdentifier{bacnet.PropertyObjectList, bacnet.PropertyDatabaseRevision}})
	return nil
}

// Remove removes an object. The Device itself cannot be removed.
func (d *Device) Remove(id bacnet.ObjectIdentifier) error {
	if id == d.Identifier() {
		return ErrNotDeletable
	}
	d.tmu.Lock()
	o, ok := d.objects[id]
	if !ok {
		d.tmu.Unlock()
		return ErrUnknownObject
	}
	delete(d.objects, id)
	delete(d.names, o.Name())
	d.revision++
	d.tmu.Unlock()
	if n, ok := o.(Notifying); ok {
		n.SetNotifier(nil)
	}
	d.dispatch(Change{Object: d.Identifier(), Properties: []bacnet.PropertyIdentifier{bacnet.PropertyObjectList, bacnet.PropertyDatabaseRevision}})
	return nil
}

// Rename changes the Object_Name of an object, keeping names unique.
func (d *Device) Rename(id bacnet.ObjectIdentifier, name string) error {
	if name == "" {
		return ErrValueOutOfRange
	}
	d.tmu.Lock()
	o, ok := d.objects[id]
	if !ok {
		d.tmu.Unlock()
		return ErrUnknownObject
	}
	if other, taken := d.names[name]; taken {
		d.tmu.Unlock()
		if other == id {
			return nil
		}
		return ErrDuplicateName
	}
	b, ok := baseOf(o)
	if !ok {
		d.tmu.Unlock()
		return ErrWriteAccessDenied
	}
	delete(d.names, o.Name())
	b.setName(name)
	d.names[name] = id
	d.revision++
	d.tmu.Unlock()
	d.dispatch(Change{Object: id, Properties: []bacnet.PropertyIdentifier{bacnet.PropertyObjectName}})
	return nil
}

type baser interface{ base() *Base }

func (b *Base) base() *Base { return b }

func baseOf(o Object) (*Base, bool) {
	if x, ok := o.(baser); ok {
		return x.base(), true
	}
	return nil, false
}

// Object returns the object with the given identifier. The Device's own
// identifier and the wildcard instance (4194303) both return the Device.
func (d *Device) Object(id bacnet.ObjectIdentifier) (Object, bool) {
	if id.Type == bacnet.ObjectTypeDevice && id.Instance == bacnet.WildcardInstance {
		return d, true
	}
	d.tmu.RLock()
	defer d.tmu.RUnlock()
	o, ok := d.objects[id]
	return o, ok
}

// ObjectByName returns the object with the given Object_Name.
func (d *Device) ObjectByName(name string) (Object, bool) {
	d.tmu.RLock()
	defer d.tmu.RUnlock()
	id, ok := d.names[name]
	if !ok {
		return nil, false
	}
	return d.objects[id], true
}

// RegisterFactory enables CreateObject for an object type.
func (d *Device) RegisterFactory(t bacnet.ObjectType, f Factory) {
	d.tmu.Lock()
	d.factories[t] = f
	d.tmu.Unlock()
}

// Create creates an object with a registered factory. instance nil picks
// the lowest free instance. The object is added to the device.
func (d *Device) Create(t bacnet.ObjectType, instance *uint32, name string) (Object, error) {
	d.tmu.RLock()
	f, ok := d.factories[t]
	d.tmu.RUnlock()
	if !ok {
		// As bacnet-stack's create_object_process: a type the device has
		// objects of but cannot create is dynamic-creation-not-supported,
		// any other type unsupported-object-type.
		if t != bacnet.ObjectTypeDevice && d.hasType(t) {
			return nil, ErrNotCreatable
		}
		return nil, ErrUnsupportedType
	}
	var inst uint32
	if instance != nil {
		inst = *instance
		if inst >= bacnet.MaxObjectInstance {
			return nil, ErrValueOutOfRange
		}
		if _, exists := d.Object(bacnet.ObjectIdentifier{Type: t, Instance: inst}); exists {
			return nil, ErrObjectExists
		}
	} else {
		d.tmu.RLock()
		for {
			if _, used := d.objects[bacnet.ObjectIdentifier{Type: t, Instance: inst}]; !used {
				break
			}
			inst++
		}
		d.tmu.RUnlock()
	}
	if name == "" {
		name = t.String() + "-" + itoa(inst)
		for i := 1; ; i++ {
			if _, taken := d.ObjectByName(name); !taken {
				break
			}
			name = t.String() + "-" + itoa(inst) + "-" + itoa(uint32(i))
		}
	}
	o, err := f(inst, name)
	if err != nil {
		return nil, err
	}
	if b, ok := baseOf(o); ok {
		b.SetDeletable(true)
	}
	if err := d.Add(o); err != nil {
		return nil, err
	}
	return o, nil
}

func (d *Device) hasType(t bacnet.ObjectType) bool {
	d.tmu.RLock()
	defer d.tmu.RUnlock()
	for id := range d.objects {
		if id.Type == t {
			return true
		}
	}
	return false
}

func itoa(u uint32) string {
	if u == 0 {
		return "0"
	}
	var b [10]byte
	i := len(b)
	for u > 0 {
		i--
		b[i] = byte('0' + u%10)
		u /= 10
	}
	return string(b[i:])
}

// Observe registers fn for every change of every object, the Device
// included. fn runs on the goroutine that made the change and must not
// block; it must not call back into the object that changed.
func (d *Device) Observe(fn Notifier) (cancel func()) {
	d.omu.Lock()
	id := d.nextObs
	d.nextObs++
	d.observers[id] = fn
	d.omu.Unlock()
	return func() {
		d.omu.Lock()
		delete(d.observers, id)
		d.omu.Unlock()
	}
}

func (d *Device) dispatch(c Change) {
	d.omu.Lock()
	fns := make([]Notifier, 0, len(d.observers))
	for _, fn := range d.observers {
		fns = append(fns, fn)
	}
	d.omu.Unlock()
	for _, fn := range fns {
		fn(c)
	}
}

// WriteProperty implements Object for the Device object. Writes to
// Object_Name go through Rename so that names stay unique.
func (d *Device) WriteProperty(ctx context.Context, w Write) error {
	if w.Property.Identifier == bacnet.PropertyObjectName {
		return ErrWriteAccessDenied
	}
	return d.Base.WriteProperty(ctx, w)
}

func dateOf(t time.Time) bacnet.Date {
	wd := uint8(t.Weekday())
	if wd == 0 {
		wd = 7
	}
	return bacnet.Date{Year: uint8(t.Year() - 1900), Month: uint8(t.Month()), Day: uint8(t.Day()), Weekday: wd}
}

func timeOf(t time.Time) bacnet.Time {
	return bacnet.Time{Hour: uint8(t.Hour()), Minute: uint8(t.Minute()), Second: uint8(t.Second()), Hundredths: uint8(t.Nanosecond() / 1e7)}
}
