// SPDX-License-Identifier: MIT

package object

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
)

var (
	devDest  = bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeDevice, Instance: 77}
	allDay   = Destination{ValidDays: 0x7F, From: bacnet.Time{}, To: bacnet.Time{Hour: 23, Minute: 59, Second: 59, Hundredths: 99}, Device: &devDest, ProcessID: 1, Transitions: 7}
	addrDest = Destination{ValidDays: 0x01, From: bacnet.Time{Hour: 8}, To: bacnet.Time{Hour: 17}, Network: 5, MAC: []byte{10, 0, 0, 1, 0xBA, 0xC0}, ProcessID: 2, Confirmed: true, Transitions: 1 << TransitionToFault}
)

func elementsOf(ds ...Destination) []bacnet.Element {
	var els []bacnet.Element
	for _, d := range ds {
		els = append(els, d.Elements()...)
	}
	return els
}

func TestDestinationRoundTrip(t *testing.T) {
	// Through the wire: encode the list and parse it back.
	raw, err := bacnet.AppendContextTagged(nil, 3, elementsOf(allDay, addrDest))
	if err != nil {
		t.Fatal(err)
	}
	els, _, err := bacnet.ParseSequence(raw, bacnet.DefaultDecodeLimits(), -1)
	if err != nil {
		t.Fatal(err)
	}
	ds, _, err := ParseDestinations(els[0].Value.Elements)
	if err != nil || len(ds) != 2 || !ds[0].equal(allDay) || !ds[1].equal(addrDest) {
		t.Fatalf("round trip: %v %+v", err, ds)
	}
	if a, err := addrDest.Address(); err != nil || a.Network() != 5 || a.MAC().Len() != 6 {
		t.Fatalf("address: %v %v", err, a)
	}
	for _, d := range []Destination{{}, {Network: 9}, {MAC: []byte{1}}} {
		a, err := d.Address()
		if err != nil {
			t.Fatal(err)
		}
		if (len(d.MAC) == 0) != a.IsBroadcast() {
			t.Errorf("%+v -> %v", d, a)
		}
	}
	if _, err := (Destination{MAC: make([]byte, 20)}).Address(); err == nil {
		t.Error("oversized MAC accepted")
	}
}

func TestParseDestinationsErrors(t *testing.T) {
	good := allDay.Elements()
	if _, first, err := ParseDestinations(append(good, good[0])); err == nil || first != 2 {
		t.Fatalf("short list: %d %v", first, err)
	}
	mutate := func(i int, el bacnet.Element) []bacnet.Element {
		out := append([]bacnet.Element(nil), good...)
		out[i] = el
		return append(append([]bacnet.Element(nil), good...), out...)
	}
	bad := []bacnet.Element{
		{Value: bacnet.UnsignedValue(1)},
		{Context: true, TagNumber: 2, Value: bacnet.UnsignedValue(1)},
		{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}}}},
		{Context: true, TagNumber: 1, Value: bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Value: bacnet.UnsignedValue(1)}, {Value: bacnet.ApplicationValue{Kind: bacnet.ValueOctetString, OctetString: make([]byte, 20)}}}}},
		{Context: true, TagNumber: 0, Value: bacnet.ApplicationValue{Kind: bacnet.ValueContext, OctetString: []byte{1}}},
	}
	for i, el := range bad {
		if _, first, err := ParseDestinations(mutate(3, el)); err == nil || first != 2 {
			t.Errorf("bad recipient %d: %d %v", i, first, err)
		}
	}
	if _, _, err := ParseDestinations(mutate(4, bacnet.Element{Value: bacnet.UnsignedValue(1 << 33)})); err == nil {
		t.Error("process id > 32 bits accepted")
	}
}

func TestDestinationActive(t *testing.T) {
	monday9 := time.Date(2026, 11, 2, 9, 0, 0, 0, time.Local) // a Monday
	if !addrDest.Active(monday9, TransitionToFault) {
		t.Error("Monday 09:00 to-fault not active")
	}
	if addrDest.Active(monday9, TransitionToNormal) {
		t.Error("unselected transition active")
	}
	if addrDest.Active(monday9.Add(24*time.Hour), TransitionToFault) {
		t.Error("Tuesday active")
	}
	if addrDest.Active(monday9.Add(9*time.Hour), TransitionToFault) {
		t.Error("18:00 active")
	}
	wild := addrDest
	wild.To = bacnet.Time{Hour: 0xFF, Minute: 0xFF, Second: 0xFF, Hundredths: 0xFF}
	if !wild.Active(monday9.Add(14*time.Hour), TransitionToFault) {
		t.Error("wildcard end time")
	}
}

func TestNotificationClass(t *testing.T) {
	ctx := context.Background()
	nc, err := NewNotificationClass(NotificationClassConfig{Instance: 3, Name: "NC-3", Description: "d", Priority: &[3]uint8{10, 0, 30}, AckRequired: 1, Recipients: []Destination{allDay}, MaxRecipients: 2})
	if err != nil {
		t.Fatal(err)
	}
	if nc.Class() != 3 || nc.Priorities() != [3]uint8{10, 0, 30} || nc.AckRequired() != 1 {
		t.Fatalf("config: %v %v", nc.Priorities(), nc.AckRequired())
	}
	p, ack, to := nc.Routing(time.Now(), TransitionToOffNormal)
	if p != 10 || !ack || len(to) != 1 {
		t.Fatalf("routing: %d %v %d", p, ack, len(to))
	}
	rl := bacnet.PropertyReference{Identifier: bacnet.PropertyRecipientList}
	// A destination already there is not added again.
	if err := nc.AddListElements(ctx, rl, elementsOf(allDay, addrDest)); err != nil {
		t.Fatal(err)
	}
	if v, _ := nc.ReadProperty(ctx, rl); len(v.Elements) != 14 {
		t.Fatalf("after add: %d elements", len(v.Elements))
	}
	extra := addrDest
	extra.ProcessID = 9
	var le *ListError
	if err := nc.AddListElements(ctx, rl, elementsOf(extra)); !errors.As(err, &le) || le.First != 1 || !errors.Is(err, ErrNoSpaceForListElement) || le.Error() == "" {
		t.Fatalf("full: %v", err)
	}
	if err := nc.AddListElements(ctx, rl, good3()); !errors.As(err, &le) || le.First != 1 {
		t.Fatalf("malformed add: %v", err)
	}
	// Removing a missing destination removes nothing.
	if err := nc.RemoveListElements(ctx, rl, elementsOf(addrDest, extra)); !errors.As(err, &le) || le.First != 2 || !errors.Is(err, ErrListElementNotFound) {
		t.Fatalf("remove missing: %v", err)
	}
	if v, _ := nc.ReadProperty(ctx, rl); len(v.Elements) != 14 {
		t.Fatal("partial remove applied")
	}
	if err := nc.RemoveListElements(ctx, rl, elementsOf(addrDest)); err != nil {
		t.Fatal(err)
	}
	if err := nc.RemoveListElements(ctx, rl, good3()); err == nil {
		t.Fatal("malformed remove accepted")
	}
	for _, prop := range []bacnet.PropertyReference{{Identifier: bacnet.PropertyPriority}, {Identifier: bacnet.PropertyPolarity}} {
		if err := nc.AddListElements(ctx, prop, nil); err == nil {
			t.Errorf("list op on %d accepted", prop.Identifier)
		}
		if err := nc.RemoveListElements(ctx, prop, nil); err == nil {
			t.Errorf("remove on %d accepted", prop.Identifier)
		}
	}
	idx := uint32(1)
	if err := nc.AddListElements(ctx, bacnet.PropertyReference{Identifier: bacnet.PropertyRecipientList, ArrayIndex: &idx}, nil); !errors.Is(err, ErrNotAnArray) {
		t.Fatal(err)
	}
}

func good3() []bacnet.Element { return allDay.Elements()[:3] }

func TestNotificationClassWrites(t *testing.T) {
	ctx := context.Background()
	nc, _ := NewNotificationClass(NotificationClassConfig{Instance: 1, Name: "NC", MaxRecipients: 1})
	w := func(p bacnet.PropertyIdentifier, idx *uint32, v bacnet.ApplicationValue) error {
		return nc.WriteProperty(ctx, Write{Property: bacnet.PropertyReference{Identifier: p, ArrayIndex: idx}, Value: v})
	}
	arr := func(vals ...bacnet.ApplicationValue) bacnet.ApplicationValue {
		var els []bacnet.Element
		for _, v := range vals {
			els = append(els, bacnet.Element{Value: v})
		}
		return bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: els}
	}
	u := bacnet.UnsignedValue
	if err := w(bacnet.PropertyPriority, nil, arr(u(1), u(2), u(3))); err != nil || nc.Priorities() != [3]uint8{1, 2, 3} {
		t.Fatalf("priority: %v %v", err, nc.Priorities())
	}
	two := uint32(2)
	if err := w(bacnet.PropertyPriority, &two, u(9)); err != nil || nc.Priorities()[1] != 9 {
		t.Fatalf("priority[2]: %v", err)
	}
	four, zero := uint32(4), uint32(0)
	for i, c := range []struct {
		idx  *uint32
		v    bacnet.ApplicationValue
		want error
	}{
		{&four, u(1), ErrInvalidArrayIndex},
		{&zero, u(1), ErrInvalidArrayIndex},
		{nil, arr(u(1), u(2)), ErrInvalidDataType},
		{nil, arr(u(1), u(2), bacnet.RealValue(1)), ErrInvalidDataType},
		{nil, arr(u(1), u(2), u(256)), ErrValueOutOfRange},
		{nil, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: []bacnet.Element{{Context: true}, {}, {}}}, ErrInvalidDataType},
	} {
		if err := w(bacnet.PropertyPriority, c.idx, c.v); !errors.Is(err, c.want) {
			t.Errorf("priority case %d: %v", i, err)
		}
	}
	if err := w(bacnet.PropertyAckRequired, nil, maskBits(3, 5)); err != nil || nc.AckRequired() != 5 {
		t.Fatalf("ack required: %v", err)
	}
	if err := w(bacnet.PropertyAckRequired, nil, u(1)); !errors.Is(err, ErrInvalidDataType) {
		t.Fatal(err)
	}
	if err := w(bacnet.PropertyAckRequired, &two, maskBits(3, 1)); !errors.Is(err, ErrNotAnArray) {
		t.Fatal(err)
	}
	rl := bacnet.PropertyRecipientList
	if err := w(rl, nil, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: elementsOf(allDay)}); err != nil {
		t.Fatal(err)
	}
	if err := w(rl, nil, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: elementsOf(allDay, addrDest)}); !errors.Is(err, ErrNoSpaceToWriteProperty) {
		t.Fatal(err)
	}
	if err := w(rl, nil, bacnet.ApplicationValue{Kind: bacnet.ValueConstructed, Elements: good3()}); !errors.Is(err, ErrInvalidDataType) {
		t.Fatal(err)
	}
	if err := w(rl, nil, u(1)); !errors.Is(err, ErrInvalidDataType) {
		t.Fatal(err)
	}
	if err := w(rl, &two, u(1)); !errors.Is(err, ErrNotAnArray) {
		t.Fatal(err)
	}
	for _, cfg := range []NotificationClassConfig{{}, {Name: "x", Instance: bacnet.MaxObjectInstance}, {Name: "x", MaxRecipients: 1, Recipients: []Destination{allDay, addrDest}}} {
		if _, err := NewNotificationClass(cfg); err == nil {
			t.Errorf("accepted %+v", cfg)
		}
	}
}

func TestNotificationClassPrioritiesAndRecipients(t *testing.T) {
	nc, err := NewNotificationClass(NotificationClassConfig{Instance: 1, Name: "NC", Recipients: []Destination{allDay, addrDest}})
	if err != nil {
		t.Fatal(err)
	}
	// Nil Priority starts all three at 255 (bacnet-stack nc.c).
	if p := nc.Priorities(); p != [3]uint8{255, 255, 255} {
		t.Fatalf("priorities %v", p)
	}
	zero, _ := NewNotificationClass(NotificationClassConfig{Instance: 2, Name: "NC2", Priority: &[3]uint8{}})
	if p := zero.Priorities(); p != [3]uint8{} {
		t.Fatalf("priority 0 not kept: %v", p)
	}
	// The enrollment filter matches recipient and process, nothing else.
	match := allDay
	match.ValidDays, match.Confirmed, match.Transitions = 0, !allDay.Confirmed, 0
	other := allDay
	other.ProcessID++
	addr := addrDest
	addr.Confirmed = !addr.Confirmed
	otherMAC := addrDest
	otherMAC.MAC = []byte{9}
	otherNet := addrDest
	otherNet.Network++
	asAddr := addrDest
	asAddr.ProcessID = allDay.ProcessID
	for i, k := range []struct {
		d    Destination
		want bool
	}{{match, true}, {other, false}, {addr, true}, {otherMAC, false}, {otherNet, false}, {asAddr, false}} {
		if got := nc.HasRecipient(k.d); got != k.want {
			t.Errorf("case %d: %v", i, got)
		}
	}
	otherDev := devDest
	otherDev.Instance++
	d := allDay
	d.Device = &otherDev
	if nc.HasRecipient(d) {
		t.Error("other device matched")
	}
}
