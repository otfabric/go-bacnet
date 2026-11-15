// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
)

// lockOrderBound is how long the two calls of a lock-order test get to
// return. They finish in microseconds; the bound only ends a run in which
// they wait for each other.
const lockOrderBound = 10 * time.Second

// untilTableChanged returns once the Device's object table is held for
// writing by another goroutine, or once changed reports (under the table's
// read lock) that the writer is done with the table. It does not sleep: the
// caller is the only goroutine that could let the writer finish.
func untilTableChanged(d *Device, changed func() bool) {
	for {
		if !d.tmu.TryRLock() {
			return
		}
		done := changed()
		d.tmu.RUnlock()
		if done {
			return
		}
		runtime.Gosched()
	}
}

func bothReturn(t *testing.T, what string, a, b <-chan struct{}) {
	t.Helper()
	timeout := time.After(lockOrderBound)
	for a != nil || b != nil {
		select {
		case <-a:
			a = nil
		case <-b:
			b = nil
		case <-timeout:
			t.Fatalf("%s: the two calls did not both return within %v", what, lockOrderBound)
		}
	}
}

// Event evaluation asks the Device for the Notification Class of a point
// while the point is being updated; Remove and Rename of that point change
// the object table at the same time. Both calls return (row L9 of the
// lifecycle table in TESTING.md: the object lock and the table lock are
// never waited for in opposite orders).
func TestDeviceTableChangeDuringEventEvaluation(t *testing.T) {
	for _, tc := range []struct {
		name    string
		change  func(d *Device, id bacnet.ObjectIdentifier) error
		changed func(d *Device, id bacnet.ObjectIdentifier) bool
	}{
		{
			name:    "Remove",
			change:  func(d *Device, id bacnet.ObjectIdentifier) error { return d.Remove(id) },
			changed: func(d *Device, id bacnet.ObjectIdentifier) bool { _, ok := d.objects[id]; return !ok },
		},
		{
			name:    "Rename",
			change:  func(d *Device, id bacnet.ObjectIdentifier) error { return d.Rename(id, "renamed") },
			changed: func(d *Device, id bacnet.ObjectIdentifier) bool { return d.names["renamed"] == id },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newTestDevice(t, DeviceConfig{Instance: 1})
			p := analogWithEvents(t, 0)
			if err := d.Add(p); err != nil {
				t.Fatal(err)
			}
			id := p.Identifier()
			class := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeNotificationClass, Instance: 3}
			_ = p.SetPresentValue(bacnet.RealValue(90)) // above the high limit: a transition to report

			inCallback := make(chan struct{})
			startChange := make(chan struct{})
			evaluated, changed := make(chan struct{}), make(chan struct{})
			var transitions []EventTransition
			go func() {
				defer close(evaluated)
				transitions = p.EvaluateEvents(t0.Add(time.Second), func(uint32, int) bool {
					// As the server's callback: look the class up in the table.
					close(inCallback)
					<-startChange
					untilTableChanged(d, func() bool { return tc.changed(d, id) })
					_, _ = d.Object(class)
					return true
				})
			}()
			<-inCallback
			var changeErr error
			go func() {
				defer close(changed)
				changeErr = tc.change(d, id)
			}()
			close(startChange)
			bothReturn(t, tc.name+" during EvaluateEvents", evaluated, changed)
			if changeErr != nil {
				t.Fatal(changeErr)
			}
			if len(transitions) != 1 || transitions[0].To != EventStateHighLimit {
				t.Fatalf("transitions %+v", transitions)
			}
			// The table answers afterwards.
			if _, ok := d.Object(d.Identifier()); !ok {
				t.Fatal("device object not found")
			}
			if tc.name == "Rename" {
				if o, ok := d.ObjectByName("renamed"); !ok || o.Name() != "renamed" {
					t.Fatalf("renamed object: %v %v", o, ok)
				}
				if _, ok := d.ObjectByName("AI"); ok {
					t.Fatal("old name still registered")
				}
			} else if _, ok := d.ObjectByName("AI"); ok {
				t.Fatal("removed object still registered by name")
			}
		})
	}
}

// A read of a Device property whose value comes from the object table, and a
// Rename of the Device object at the same time, both return.
func TestDeviceRenameDuringTableRead(t *testing.T) {
	d := newTestDevice(t, DeviceConfig{Instance: 1})
	id := d.Identifier()
	const prop = bacnet.PropertyIdentifier(9001)
	inGet := make(chan struct{})
	startRename := make(chan struct{})
	d.Define(prop, &Property{Get: func() bacnet.ApplicationValue {
		// As Database_Revision and Object_List: computed from the table,
		// with the Device object's lock held for reading.
		close(inGet)
		<-startRename
		untilTableChanged(d, func() bool { return d.names["renamed"] == id })
		return bacnet.UnsignedValue(uint64(d.DatabaseRevision()))
	}})

	read, renamed := make(chan struct{}), make(chan struct{})
	var readErr, renameErr error
	go func() {
		defer close(read)
		_, readErr = d.ReadProperty(context.Background(), bacnet.PropertyReference{Identifier: prop})
	}()
	<-inGet
	go func() {
		defer close(renamed)
		renameErr = d.Rename(id, "renamed")
	}()
	close(startRename)
	bothReturn(t, "Rename of the Device during a read", read, renamed)
	if readErr != nil || renameErr != nil {
		t.Fatalf("read %v, rename %v", readErr, renameErr)
	}
	if d.Name() != "renamed" {
		t.Fatalf("name %q", d.Name())
	}
	if o, ok := d.ObjectByName("renamed"); !ok || o.Identifier() != id {
		t.Fatal("device not registered under its new name")
	}
}

// Renames of one object from several goroutines leave the table and the
// object with the same name.
func TestDeviceRenameConcurrent(t *testing.T) {
	d := newTestDevice(t, DeviceConfig{Instance: 1})
	p := newAV(t, 1, "AV")
	if err := d.Add(p); err != nil {
		t.Fatal(err)
	}
	names := []string{"a", "b", "c", "d"}
	done := make(chan struct{})
	for _, n := range names {
		go func() {
			defer func() { done <- struct{}{} }()
			for range 200 {
				_ = d.Rename(p.Identifier(), n)
			}
		}()
	}
	for range names {
		<-done
	}
	o, ok := d.ObjectByName(p.Name())
	if !ok || o.Identifier() != p.Identifier() {
		t.Fatalf("object named %q is not registered under that name", p.Name())
	}
	registered := 0
	for _, n := range append(names, "AV") {
		if _, ok := d.ObjectByName(n); ok {
			registered++
		}
	}
	if registered != 1 {
		t.Fatalf("%d names registered for one object", registered)
	}
}
