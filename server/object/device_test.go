// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
)

func newTestDevice(t *testing.T, cfg DeviceConfig) *Device {
	t.Helper()
	if cfg.Name == "" {
		cfg.Name = "dev"
	}
	d, err := NewDevice(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func newAV(t *testing.T, inst uint32, name string) *Point {
	t.Helper()
	p, err := NewAnalogValue(PointConfig{Instance: inst, Name: name})
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// plainObject implements Object without embedding Base.
type plainObject struct {
	id   bacnet.ObjectIdentifier
	name string
}

func (o *plainObject) Identifier() bacnet.ObjectIdentifier { return o.id }
func (o *plainObject) Name() string                        { return o.name }
func (o *plainObject) ReadProperty(context.Context, bacnet.PropertyReference) (bacnet.ApplicationValue, error) {
	return bacnet.ApplicationValue{}, ErrUnknownProperty
}
func (o *plainObject) WriteProperty(context.Context, Write) error { return ErrWriteAccessDenied }
func (o *plainObject) Properties() ([]bacnet.PropertyIdentifier, map[bacnet.PropertyIdentifier]bool) {
	return nil, nil
}

func TestDeviceConfig(t *testing.T) {
	for _, cfg := range []DeviceConfig{{Instance: bacnet.MaxObjectInstance, Name: "d"}, {Instance: 1}} {
		if d, err := NewDevice(cfg); err == nil || d != nil {
			t.Fatalf("config %+v accepted", cfg)
		}
	}
	ctx := context.Background()
	d := newTestDevice(t, DeviceConfig{Instance: 7, Location: "roof", Description: "ahu", SerialNumber: "SN-1"})
	if d.Config().ProtocolRevision != DefaultProtocolRevision || d.Config().MaxObjects != 65535 {
		t.Fatalf("defaults %+v", d.Config())
	}
	for id, want := range map[bacnet.PropertyIdentifier]string{bacnet.PropertyLocation: "roof", bacnet.PropertyDescription: "ahu", bacnet.PropertySerialNumber: "SN-1"} {
		if v, err := d.ReadProperty(ctx, prop(id)); err != nil || v.Character.Value != want {
			t.Fatalf("%v: %+v %v", id, v, err)
		}
	}
	if err := d.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyLocation), Value: CharString("plant room")}); err != nil {
		t.Fatal(err)
	}
	if v, _ := d.ReadProperty(ctx, prop(bacnet.PropertyLocation)); v.Character.Value != "plant room" {
		t.Fatalf("Location %+v", v)
	}
	wantErr(t, "serial number", d.WriteProperty(ctx, Write{Property: prop(bacnet.PropertySerialNumber), Value: CharString("x")}), ErrWriteAccessDenied)
	// Object_Name of the Device is not writable over the network.
	wantErr(t, "object name", d.WriteProperty(ctx, Write{Property: prop(bacnet.PropertyObjectName), Value: CharString("x")}), ErrWriteAccessDenied)
	plain := newTestDevice(t, DeviceConfig{Instance: 8})
	for _, id := range []bacnet.PropertyIdentifier{bacnet.PropertyLocation, bacnet.PropertyDescription, bacnet.PropertySerialNumber} {
		wantErr(t, "optional "+id.String(), readErr(plain, id), ErrUnknownProperty)
	}
}

// TestDeviceWithoutRuntime checks the values a Device reports before a
// server binds it.
func TestDeviceWithoutRuntime(t *testing.T) {
	ctx := context.Background()
	d := newTestDevice(t, DeviceConfig{Instance: 1})
	want := map[bacnet.PropertyIdentifier]uint64{
		bacnet.PropertyMaxAPDULength:       1476,
		bacnet.PropertyMaxSegmentsAccepted: 0,
		bacnet.PropertyAPDUSegmentTimeout:  2000,
		bacnet.PropertyAPDUTimeout:         3000,
		bacnet.PropertyNumberOfAPDURetries: 3,
	}
	for id, w := range want {
		if v, err := d.ReadProperty(ctx, prop(id)); err != nil || v.Unsigned != w {
			t.Errorf("%v: %+v %v, want %d", id, v, err, w)
		}
	}
	if v, _ := d.ReadProperty(ctx, prop(bacnet.PropertySegmentation)); v.Enumerated != 3 {
		t.Errorf("Segmentation %+v", v)
	}
	if v, _ := d.ReadProperty(ctx, prop(bacnet.PropertyActiveCOVSubscriptions)); v.Kind != bacnet.ValueConstructed || len(v.Elements) != 0 {
		t.Errorf("Active_COV_Subscriptions %+v", v)
	}
	ss, _ := d.ReadProperty(ctx, prop(bacnet.PropertyProtocolServicesSupported))
	for i := 0; i < servicesSupportedBits; i++ {
		if bit(ss, i) {
			t.Errorf("service %d supported without a server", i)
		}
	}
	before := time.Now()
	ld, err := d.ReadProperty(ctx, prop(bacnet.PropertyLocalDate))
	if err != nil || ld.Kind != bacnet.ValueDate || int(ld.Date.Year)+1900 < before.Year() {
		t.Errorf("Local_Date %+v %v", ld, err)
	}
	if lt, err := d.ReadProperty(ctx, prop(bacnet.PropertyLocalTime)); err != nil || lt.Kind != bacnet.ValueTime {
		t.Errorf("Local_Time %+v %v", lt, err)
	}
	if v, _ := d.ReadProperty(ctx, prop(bacnet.PropertySystemStatus)); v.Enumerated != StatusOperational {
		t.Errorf("System_Status %+v", v)
	}
	d.SetSystemStatus(StatusNonOperational)
	if v, _ := d.ReadProperty(ctx, prop(bacnet.PropertySystemStatus)); v.Enumerated != StatusNonOperational {
		t.Errorf("System_Status after SetSystemStatus %+v", v)
	}
}

func TestDeviceAddRemove(t *testing.T) {
	d := newTestDevice(t, DeviceConfig{Instance: 1, MaxObjects: 3})
	var changes []Change
	cancel := d.Observe(func(c Change) { changes = append(changes, c) })
	other := newTestDevice(t, DeviceConfig{Instance: 2, Name: "other"})
	if err := d.Add(other); err == nil {
		t.Fatal("second Device object accepted")
	}
	if err := d.Add(NewBase(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}, "")); err == nil {
		t.Fatal("object without a name accepted")
	}
	av1 := newAV(t, 1, "av1")
	if err := d.Add(av1); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "same identifier", d.Add(newAV(t, 1, "av1b")), ErrObjectExists)
	wantErr(t, "same name", d.Add(newAV(t, 2, "av1")), ErrDuplicateName)
	if err := d.Add(newAV(t, 2, "av2")); err != nil {
		t.Fatal(err)
	}
	wantErr(t, "table full", d.Add(newAV(t, 3, "av3")), ErrNoSpaceForObject)
	if d.DatabaseRevision() != 2 || len(changes) != 2 {
		t.Fatalf("revision %d, %d changes", d.DatabaseRevision(), len(changes))
	}
	// The Device notifier reaches the observers once an object is added.
	if err := av1.SetPresentValue(bacnet.RealValue(5)); err != nil {
		t.Fatal(err)
	}
	if len(changes) != 3 || changes[2].Object != av1.Identifier() {
		t.Fatalf("change of a member %+v", changes)
	}
	wantErr(t, "remove the Device", d.Remove(d.Identifier()), ErrNotDeletable)
	wantErr(t, "remove unknown", d.Remove(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 9}), ErrUnknownObject)
	if err := d.Remove(av1.Identifier()); err != nil {
		t.Fatal(err)
	}
	if _, ok := d.Object(av1.Identifier()); ok {
		t.Fatal("removed object still served")
	}
	if _, ok := d.ObjectByName("av1"); ok {
		t.Fatal("removed name still served")
	}
	if d.DatabaseRevision() != 3 || len(changes) != 4 || changes[3].Object != d.Identifier() {
		t.Fatalf("after remove: revision %d, %+v", d.DatabaseRevision(), changes)
	}
	// A removed object no longer reports to the Device.
	if err := av1.SetPresentValue(bacnet.RealValue(6)); err != nil || len(changes) != 4 {
		t.Fatalf("removed object notified: %v, %d", err, len(changes))
	}
	cancel()
	if err := d.Add(newAV(t, 4, "av4")); err != nil || len(changes) != 4 {
		t.Fatalf("cancelled observer called: %v, %d", err, len(changes))
	}
	if o, ok := d.Object(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: bacnet.WildcardInstance}); !ok || o != Object(d) {
		t.Fatal("wildcard device instance")
	}
}

func TestDeviceRename(t *testing.T) {
	ctx := context.Background()
	d := newTestDevice(t, DeviceConfig{Instance: 1})
	av := newAV(t, 1, "av1")
	plain := &plainObject{id: bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 2}, name: "plain"}
	for _, o := range []Object{av, newAV(t, 3, "av3"), plain} {
		if err := d.Add(o); err != nil {
			t.Fatal(err)
		}
	}
	rev := d.DatabaseRevision()
	var changes []Change
	d.Observe(func(c Change) { changes = append(changes, c) })
	wantErr(t, "empty", d.Rename(av.Identifier(), ""), ErrValueOutOfRange)
	wantErr(t, "unknown", d.Rename(bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 9}, "x"), ErrUnknownObject)
	wantErr(t, "taken", d.Rename(av.Identifier(), "av3"), ErrDuplicateName)
	wantErr(t, "not a Base", d.Rename(plain.id, "renamed"), ErrWriteAccessDenied)
	if err := d.Rename(av.Identifier(), "av1"); err != nil || d.DatabaseRevision() != rev || len(changes) != 0 {
		t.Fatalf("rename to the same name: %v, revision %d, %d changes", err, d.DatabaseRevision(), len(changes))
	}
	if err := d.Rename(av.Identifier(), "supply"); err != nil {
		t.Fatal(err)
	}
	if av.Name() != "supply" || d.DatabaseRevision() != rev+1 {
		t.Fatalf("name %q, revision %d", av.Name(), d.DatabaseRevision())
	}
	if n, _ := av.ReadProperty(ctx, prop(bacnet.PropertyObjectName)); n.Character.Value != "supply" {
		t.Fatalf("Object_Name %+v", n)
	}
	if o, ok := d.ObjectByName("supply"); !ok || o.Identifier() != av.Identifier() {
		t.Fatal("new name not found")
	}
	if _, ok := d.ObjectByName("av1"); ok {
		t.Fatal("old name still found")
	}
	if len(changes) != 1 || changes[0].Object != av.Identifier() || changes[0].Properties[0] != bacnet.PropertyObjectName {
		t.Fatalf("changes %+v", changes)
	}
	// The old name is free again.
	if err := d.Add(newAV(t, 4, "av1")); err != nil {
		t.Fatal(err)
	}
}

func TestDeviceCreate(t *testing.T) {
	d := newTestDevice(t, DeviceConfig{Instance: 1, MaxObjects: 5})
	avType := bacnet.ObjectTypeAnalogValue
	if err := d.Add(newAV(t, 0, avType.String()+"-1")); err != nil {
		t.Fatal(err)
	}
	if err := d.Add(newBV(t)); err != nil {
		t.Fatal(err)
	}
	_, err := d.Create(bacnet.ObjectTypeBinaryValue, nil, "")
	wantErr(t, "type without a factory", err, ErrNotCreatable)
	_, err = d.Create(bacnet.ObjectTypeAccumulator, nil, "")
	wantErr(t, "unknown type", err, ErrUnsupportedType)
	_, err = d.Create(bacnet.ObjectTypeDevice, nil, "")
	wantErr(t, "device", err, ErrUnsupportedType)
	failing := errors.New("factory failed")
	d.RegisterFactory(avType, func(inst uint32, name string) (Object, error) {
		if name == "fail" {
			return nil, failing
		}
		return NewAnalogValue(PointConfig{Instance: inst, Name: name})
	})
	big := uint32(bacnet.MaxObjectInstance)
	_, err = d.Create(avType, &big, "")
	wantErr(t, "instance out of range", err, ErrValueOutOfRange)
	zero := uint32(0)
	_, err = d.Create(avType, &zero, "")
	wantErr(t, "instance taken", err, ErrObjectExists)
	if _, err := d.Create(avType, nil, "fail"); !errors.Is(err, failing) {
		t.Fatalf("factory error %v", err)
	}
	// The lowest free instance; the generated name skips a taken one.
	o, err := d.Create(avType, nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if o.Identifier().Instance != 1 || o.Name() != avType.String()+"-1-1" {
		t.Fatalf("created %v %q", o.Identifier(), o.Name())
	}
	if del, ok := o.(Deletable); !ok || !del.Deletable() {
		t.Fatal("created object not deletable")
	}
	seven := uint32(7)
	o, err = d.Create(avType, &seven, "")
	if err != nil || o.Identifier().Instance != 7 || o.Name() != avType.String()+"-7" {
		t.Fatalf("created %v %q %v", o, o.Name(), err)
	}
	ts, _ := d.ReadProperty(context.Background(), prop(bacnet.PropertyProtocolObjectTypesSupported))
	if !bit(ts, int(avType)) || !bit(ts, int(bacnet.ObjectTypeBinaryValue)) {
		t.Fatalf("Protocol_Object_Types_Supported %+v", ts)
	}
	// The table is full: Add fails after the factory ran.
	_, err = d.Create(avType, nil, "extra")
	wantErr(t, "table full", err, ErrNoSpaceForObject)
	if _, ok := d.ObjectByName("extra"); ok {
		t.Fatal("object added beyond MaxObjects")
	}
	if itoa(0) != "0" || itoa(4194302) != "4194302" {
		t.Fatalf("itoa %q %q", itoa(0), itoa(4194302))
	}
}

func newBV(t *testing.T) *Point {
	t.Helper()
	p, err := NewBinaryValue(PointConfig{Instance: 1, Name: "bv"})
	if err != nil {
		t.Fatal(err)
	}
	return p
}
