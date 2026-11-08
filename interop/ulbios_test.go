//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// ulbios peer, bacnet-interop adapter ulbios (new in bacnet-interop 0.14.0):
// github.com/ulbios/bacnet, a small MIT Go library independent of go-bacnet,
// pinned by commit. bacnet.Parse decodes Who-Is, I-Am, ReadProperty,
// WriteProperty, Simple-ACK, Complex-ACK and Error; its encoders build REAL,
// NULL, one-octet Enumerated, two-octet Unsigned and object identifiers. It
// has no Reject or Abort encoder, so whatever it cannot decode or answer gets
// no reply at all; the adapter reports each such request in an
// {"operation":"unanswered"} line. See bacnet-interop
// adapters/ulbios/README.md for the object model and what is adapter-shim.
//
// Answers the peer gives are asserted. A request it does not answer is an
// observation, not a pass: the test asserts what go-bacnet does with the
// silence (ErrTimeout for a read, OutcomeUnknownError for a write) and the
// adapter's reason, then skips with the observation.

const (
	defaultUlbiosImage      = "bacnet-interop-ulbios:local"
	ulbiosMinAdapterVersion = "0.14.0"

	// What the ulbios device announces and serves (bacnet-interop
	// adapters/ulbios: ulbios' DEFAULT_ACCEPTED_SIZE and
	// DEFAULT_SEGMENTATION_SUPPORT, the adapter's vendor identifier).
	ulbiosMaxAPDU      = 1024
	ulbiosSegmentation = 3 // no-segmentation, clause 21 BACnetSegmentation
	ulbiosVendorID     = 999

	// The adapter answers nothing it cannot decode: short timers keep the
	// silent cases quick.
	ulbiosSilentTimeout = 1500 * time.Millisecond
)

func ulbiosImage() string { return getEnv("ULBIOS_IMAGE", defaultUlbiosImage) }

func ulbiosPeer() interopPeer { return interopPeer{"ulbios", ulbiosImage()} }

func startUlbios(t *testing.T, env ...string) *peerHandle {
	t.Helper()
	requireAdapterVersion(t, ulbiosImage(), ulbiosMinAdapterVersion)
	return startPeer(t, ulbiosImage(), "ulbios", env...)
}

// ulbiosSilentClient waits ulbiosSilentTimeout once, without retries.
func ulbiosSilentClient(t *testing.T) *client.Client {
	t.Helper()
	return newClientOpts(t, client.WithTransactionOptions(ulbiosSilentTimeout, 0, ulbiosSilentTimeout))
}

// requireUnanswered asserts that the adapter reported the request of
// service choice svc as unanswered after mark, and returns its reason.
func requireUnanswered(t *testing.T, peer *peerHandle, svc int, mark int) string {
	t.Helper()
	ev := awaitOperationLine(t, peer, "unanswered", mark, 5*time.Second)
	if got, _ := ev["service"].(float64); int(got) != svc {
		t.Fatalf("unanswered line for service %v, want %d: %v", ev["service"], svc, ev)
	}
	if ev["result"] != "no-reply" {
		t.Fatalf("unanswered line result %v, want no-reply: %v", ev["result"], ev)
	}
	reason, _ := ev["reason"].(string)
	return reason
}

// requireReadTimeout: a read that gets no answer ends in bacnet.ErrTimeout.
func requireReadTimeout(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, bacnet.ErrTimeout) {
		t.Fatalf("%s: got %T %v, want bacnet.ErrTimeout", what, err, err)
	}
	var unknown *bacnet.OutcomeUnknownError
	if errors.As(err, &unknown) {
		t.Fatalf("%s: a read reported as outcome-unknown: %v", what, err)
	}
}

// requireWriteOutcomeUnknown: a write that gets no answer is not
// retransmitted and ends as outcome-unknown, caused by the timeout.
func requireWriteOutcomeUnknown(t *testing.T, what string, err error) {
	t.Helper()
	var unknown *bacnet.OutcomeUnknownError
	if !errors.As(err, &unknown) || !errors.Is(err, bacnet.ErrTimeout) {
		t.Fatalf("%s: got %T %v, want *OutcomeUnknownError caused by ErrTimeout", what, err, err)
	}
}

func binaryValueObject(dev deviceFixture) bacnet.ObjectIdentifier {
	for _, o := range dev.Objects {
		if o.Type == "binary-value" {
			return bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: o.Instance}
		}
	}
	return bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeBinaryValue, Instance: 1}
}

func awaitDevice(t *testing.T, c *client.Client, instance uint32, within time.Duration) (client.DeviceObservation, bool) {
	t.Helper()
	deadline := time.Now().Add(within)
	for {
		for _, obs := range c.Devices() {
			if obs.Instance == instance {
				return obs, true
			}
		}
		if time.Now().After(deadline) {
			return client.DeviceObservation{}, false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func requireUlbiosCapabilities(t *testing.T, obs client.DeviceObservation) {
	t.Helper()
	caps := obs.Capabilities
	if !caps.MaxAPDULengthAccepted.Known || caps.MaxAPDULengthAccepted.Value != ulbiosMaxAPDU ||
		!caps.Segmentation.Known || caps.Segmentation.Value != ulbiosSegmentation ||
		!caps.VendorID.Known || caps.VendorID.Value != ulbiosVendorID {
		t.Fatalf("I-Am capabilities %+v, want max APDU %d, segmentation %d, vendor %d",
			caps, ulbiosMaxAPDU, ulbiosSegmentation, ulbiosVendorID)
	}
}

// Who-Is (clause 16.10): unicast with and without limits (the adapter reads
// the limits from ulbios' tokens; ulbios' own Who-Is encoder has none), a
// range that excludes the device (no I-Am), and a broadcast Who-Is answered
// with ulbios' broadcast I-Am (NPDU DNET 0xFFFF).
func TestUlbiosDiscovery(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startUlbios(t)
	if peer.assertedByReexec {
		return
	}
	inst := dev.DeviceInstance

	t.Run("unicast", func(t *testing.T) {
		cases := []struct {
			name      string
			low, high *uint32
			answer    bool
		}{
			{"no-limits", nil, nil, true},
			{"exact", &inst, &inst, true},
			{"range", ptrU32(inst - 1), ptrU32(inst + 1), true},
			{"above", ptrU32(inst + 1), ptrU32(inst + 100), false},
			{"below", ptrU32(0), ptrU32(inst - 1), false},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				c := newClient(t)
				ctx, cancel := withTimeout(t, 10*time.Second)
				defer cancel()
				if err := c.SendWhoIs(ctx, peer.endpoint, false, client.DiscoveryOptions{LowLimit: tc.low, HighLimit: tc.high}); err != nil {
					t.Fatalf("SendWhoIs: %v", err)
				}
				within := 5 * time.Second
				if !tc.answer {
					within = 2 * time.Second
				}
				obs, ok := awaitDevice(t, c, inst, within)
				if ok != tc.answer {
					t.Fatalf("I-Am received %v, want %v; devices=%v", ok, tc.answer, c.Devices())
				}
				if !ok {
					return
				}
				requireUlbiosCapabilities(t, obs)
				if !obs.Address.Equal(peer.target.Address) {
					t.Fatalf("I-Am from %v, the peer is %v", obs.Address, peer.target.Address)
				}
			})
		}
	})

	t.Run("broadcast", func(t *testing.T) {
		c := newDiscoveryClient(t)
		ctx, cancel := withTimeout(t, 15*time.Second)
		defer cancel()
		found, err := c.Discover(ctx, client.DiscoveryOptions{Window: 3 * time.Second})
		if err != nil {
			t.Fatalf("Discover: %v", err)
		}
		var obs *client.DeviceObservation
		for i := range found {
			if found[i].Instance == inst {
				obs = &found[i]
			}
		}
		if obs == nil {
			t.Fatalf("device %d did not answer the broadcast Who-Is; found %+v", inst, found)
		}
		requireUlbiosCapabilities(t, *obs)
		if obs.Target().Endpoint != peer.target.Endpoint {
			t.Fatalf("Target() endpoint %v, the peer is %v", obs.Target().Endpoint, peer.target.Endpoint)
		}
		// The discovered target reaches the device.
		v, err := c.ReadProperty(ctx, obs.Target(), obs.DeviceID(), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier})
		if err != nil {
			t.Fatalf("ReadProperty via the discovered target: %v", err)
		}
		if id, err := bacnet.AsObjectID(v); err != nil || id != deviceObject(dev) {
			t.Fatalf("object-identifier %v (%v), want %v", id, err, deviceObject(dev))
		}
	})
}

func ptrU32(v uint32) *uint32 { return &v }

// Every property the ulbios device serves, each a value type ulbios
// encodes: object identifiers, Enumerated, Unsigned (always two octets,
// objects.EncUnsignedInteger16), REAL, NULL, and the constructed
// Object_List and Priority_Array.
func TestUlbiosReadProperty(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startUlbios(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 60*time.Second)
	defer cancel()
	device := deviceObject(dev)
	av, wantPV := analogValueObject(dev)
	bv := binaryValueObject(dev)
	read := func(o bacnet.ObjectIdentifier, p bacnet.PropertyIdentifier) bacnet.ApplicationValue {
		t.Helper()
		v, err := c.ReadProperty(ctx, peer.target, o, bacnet.PropertyReference{Identifier: p})
		if err != nil {
			t.Fatalf("ReadProperty %v %s: %v", o, p.String(), err)
		}
		return v
	}
	unsigned := func(o bacnet.ObjectIdentifier, p bacnet.PropertyIdentifier, want uint64) {
		t.Helper()
		got, err := bacnet.AsUnsigned(read(o, p))
		if err != nil || got != want {
			t.Fatalf("%v %s = %d (%v), want %d", o, p.String(), got, err, want)
		}
	}
	enumerated := func(o bacnet.ObjectIdentifier, p bacnet.PropertyIdentifier, want uint32) {
		t.Helper()
		got, err := bacnet.AsEnumerated(read(o, p))
		if err != nil || got != want {
			t.Fatalf("%v %s = %d (%v), want %d", o, p.String(), got, err, want)
		}
	}

	t.Run("object-identifier and object-type", func(t *testing.T) {
		for _, o := range fixtureObjects(t, dev) {
			id, err := bacnet.AsObjectID(read(o, bacnet.PropertyObjectIdentifier))
			if err != nil || id != o {
				t.Fatalf("%v object-identifier = %v (%v)", o, id, err)
			}
			enumerated(o, bacnet.PropertyObjectType, uint32(o.Type))
		}
	})
	t.Run("device", func(t *testing.T) {
		list, err := c.ReadObjectList(ctx, peer.target, device, 0)
		if err != nil {
			t.Fatalf("ReadObjectList: %v", err)
		}
		want := fixtureObjects(t, dev)
		if len(list) != len(want) {
			t.Fatalf("object-list %v, want the %d fixture objects", list, len(want))
		}
		for _, o := range want {
			if !containsObject(list, o) {
				t.Fatalf("object-list %v lacks %v", list, o)
			}
		}
		unsigned(device, bacnet.PropertyVendorIdentifier, ulbiosVendorID)
		unsigned(device, bacnet.PropertyMaxAPDULength, ulbiosMaxAPDU)
		enumerated(device, bacnet.PropertySegmentation, ulbiosSegmentation)
		// 22 00 01: two octets, not the fewest (clause 20.2.4); go-bacnet
		// decodes the value.
		unsigned(device, bacnet.PropertyProtocolVersion, 1)
		enumerated(device, bacnet.PropertySystemStatus, 0) // operational
	})
	t.Run("analog-value", func(t *testing.T) {
		pv, err := bacnet.AsReal(read(av, bacnet.PropertyPresentValue))
		if err != nil || math.Abs(float64(pv-wantPV)) > 0.001 {
			t.Fatalf("present-value %v (%v), want %v", pv, err, wantPV)
		}
		enumerated(av, bacnet.PropertyEventState, 0) // normal
		enumerated(av, bacnet.PropertyUnits, 95)     // no-units
		if rd, err := bacnet.AsReal(read(av, bacnet.PropertyRelinquishDefault)); err != nil || rd != wantPV {
			t.Fatalf("relinquish-default %v (%v), want %v", rd, err, wantPV)
		}
		requirePriorityArray(t, read(av, bacnet.PropertyPriorityArray), nil)
	})
	t.Run("binary-value", func(t *testing.T) {
		enumerated(bv, bacnet.PropertyPresentValue, 0) // inactive
		enumerated(bv, bacnet.PropertyEventState, 0)
		enumerated(bv, bacnet.PropertyRelinquishDefault, 0)
		requirePriorityArray(t, read(bv, bacnet.PropertyPriorityArray), nil)
	})
	t.Run("errors", func(t *testing.T) {
		// Clause 15.5.1.3.1: an object or property the device does not have.
		_, err := c.ReadProperty(ctx, peer.target, bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 999},
			bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue})
		requireBACnetError(t, "unknown object", err, bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject)
		// ulbios has no CharacterString, BitString or Boolean encoder, so
		// the model has no Object_Name, Status_Flags or Out_Of_Service.
		for _, p := range []bacnet.PropertyIdentifier{bacnet.PropertyObjectName, bacnet.PropertyStatusFlags, bacnet.PropertyOutOfService} {
			_, err = c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: p})
			requireBACnetError(t, p.String(), err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
		}
		// Units of a binary value: not a property of the object.
		_, err = c.ReadProperty(ctx, peer.target, bv, bacnet.PropertyReference{Identifier: bacnet.PropertyUnits})
		requireBACnetError(t, "binary-value units", err, bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty)
	})
}

// requirePriorityArray checks the 16 slots of a Priority_Array: NULL except
// the slots in want.
func requirePriorityArray(t *testing.T, v bacnet.ApplicationValue, want map[int]bacnet.ApplicationValue) {
	t.Helper()
	if v.Kind != bacnet.ValueConstructed || len(v.Elements) != 16 {
		t.Fatalf("priority-array %+v, want 16 elements", v)
	}
	for i, el := range v.Elements {
		w, set := want[i+1]
		if !set {
			w = bacnet.NullValue()
		}
		if el.Context || el.Value.Kind != w.Kind || (w.Kind == bacnet.ValueReal && el.Value.Real != w.Real) ||
			(w.Kind == bacnet.ValueEnumerated && el.Value.Enumerated != w.Enumerated) {
			t.Fatalf("priority-array[%d] = %+v, want %+v", i+1, el.Value, w)
		}
	}
}

// WriteProperty (clause 15.9) with and without priority, through ulbios'
// decoders (the adapter assigns the tokens of a standard request, since
// ConfirmedWriteProperty.Decode accepts only ulbios' own five-token form),
// and every Error the device answers.
func TestUlbiosWriteProperty(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startUlbios(t)
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 60*time.Second)
	defer cancel()
	av, wantPV := analogValueObject(dev)
	bv := binaryValueObject(dev)
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	prioArray := func(o bacnet.ObjectIdentifier) bacnet.ApplicationValue {
		t.Helper()
		v, err := c.ReadProperty(ctx, peer.target, o, bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray})
		if err != nil {
			t.Fatalf("priority-array: %v", err)
		}
		return v
	}
	applied := func(mark int, wantPrio float64) {
		t.Helper()
		ev := awaitOperationLine(t, peer, "write-property", mark, 5*time.Second)
		if ev["result"] != "applied" || ev["priority"] != wantPrio || ev["decoder"] != "adapter-shim" {
			t.Fatalf("write-property line %v, want applied at priority %v by the adapter-shim decoder", ev, wantPrio)
		}
	}

	t.Run("priority-8", func(t *testing.T) {
		mark := evidenceMark(peer)
		p := uint8(8)
		if err := c.WriteProperty(ctx, peer.target, av, pv, bacnet.RealValue(42.25), &p); err != nil {
			t.Fatalf("WriteProperty priority 8: %v", err)
		}
		applied(mark, 8)
		if got := readReal(t, ctx, c, peer, av); got != 42.25 {
			t.Fatalf("present-value %v after the write, want 42.25", got)
		}
		requirePriorityArray(t, prioArray(av), map[int]bacnet.ApplicationValue{8: bacnet.RealValue(42.25)})
	})
	t.Run("no-priority-is-16", func(t *testing.T) {
		// Clause 19.2.1: a write without priority is at priority 16.
		mark := evidenceMark(peer)
		if err := c.WriteProperty(ctx, peer.target, av, pv, bacnet.RealValue(7.5), nil); err != nil {
			t.Fatalf("WriteProperty without priority: %v", err)
		}
		applied(mark, 16)
		if got := readReal(t, ctx, c, peer, av); got != 42.25 {
			t.Fatalf("present-value %v, priority 8 (42.25) must still win over 16", got)
		}
		requirePriorityArray(t, prioArray(av), map[int]bacnet.ApplicationValue{8: bacnet.RealValue(42.25), 16: bacnet.RealValue(7.5)})
	})
	t.Run("priority-1-via-WritePriority", func(t *testing.T) {
		mark := evidenceMark(peer)
		if err := c.WritePriority(ctx, peer.target, av, 1, bacnet.RealValue(-3)); err != nil {
			t.Fatalf("WritePriority 1: %v", err)
		}
		applied(mark, 1)
		if got := readReal(t, ctx, c, peer, av); got != -3 {
			t.Fatalf("present-value %v, want -3 at priority 1", got)
		}
	})
	t.Run("binary-value", func(t *testing.T) {
		mark := evidenceMark(peer)
		p := uint8(9)
		if err := c.WriteProperty(ctx, peer.target, bv, pv, bacnet.EnumValue(1), &p); err != nil {
			t.Fatalf("WriteProperty binary-value active: %v", err)
		}
		applied(mark, 9)
		v, err := c.ReadProperty(ctx, peer.target, bv, pv)
		if got, aerr := bacnet.AsEnumerated(v); err != nil || aerr != nil || got != 1 {
			t.Fatalf("binary-value present-value %v (%v %v), want active", got, err, aerr)
		}
		requirePriorityArray(t, prioArray(bv), map[int]bacnet.ApplicationValue{9: bacnet.EnumValue(1)})
	})
	t.Run("errors", func(t *testing.T) {
		p := uint8(8)
		cases := []struct {
			name        string
			object      bacnet.ObjectIdentifier
			prop        bacnet.PropertyIdentifier
			value       bacnet.ApplicationValue
			class, code uint16
		}{
			{"unknown-object", bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 999}, bacnet.PropertyPresentValue,
				bacnet.RealValue(1), bacnet.ErrorClassObject, bacnet.ErrorCodeUnknownObject},
			{"unknown-property", av, bacnet.PropertyObjectName, bacnet.RealValue(1), bacnet.ErrorClassProperty, bacnet.ErrorCodeUnknownProperty},
			{"read-only object-type", av, bacnet.PropertyObjectType, bacnet.EnumValue(uint32(bacnet.ObjectTypeBinaryValue)),
				bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied},
			{"read-only units", av, bacnet.PropertyUnits, bacnet.EnumValue(62), bacnet.ErrorClassProperty, bacnet.ErrorCodeWriteAccessDenied},
			{"enumerated to analog", av, bacnet.PropertyPresentValue, bacnet.EnumValue(1), bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType},
			{"unsigned to analog", av, bacnet.PropertyPresentValue, bacnet.UnsignedValue(1), bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType},
			{"real to binary", bv, bacnet.PropertyPresentValue, bacnet.RealValue(1), bacnet.ErrorClassProperty, bacnet.ErrorCodeInvalidDataType},
			{"binary out of range", bv, bacnet.PropertyPresentValue, bacnet.EnumValue(2), bacnet.ErrorClassProperty, bacnet.ErrorCodeValueOutOfRange},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				err := c.WriteProperty(ctx, peer.target, tc.object, bacnet.PropertyReference{Identifier: tc.prop}, tc.value, &p)
				requireBACnetError(t, tc.name, err, tc.class, tc.code)
				var unknown *bacnet.OutcomeUnknownError
				if errors.As(err, &unknown) {
					t.Fatalf("definite Error reported as outcome-unknown: %v", err)
				}
			})
		}
		// The refused writes changed nothing.
		if got := readReal(t, ctx, c, peer, av); got != -3 {
			t.Fatalf("present-value %v after refused writes, want -3", got)
		}
		_ = wantPV
	})
}

// What the ulbios device does not answer. ulbios has no Reject or Abort
// encoder (plumbing.APDU.MarshalTo has no case for PDU types 6 and 7), so a
// request it cannot decode gets nothing back where clause 18.8 has a Reject
// (unrecognized-service, invalid-tag, missing-required-parameter) and clause
// 5.4.5 an Abort. go-bacnet sees silence: a read ends in ErrTimeout after the
// APDU timer, a write is not retransmitted and ends as outcome-unknown.
// Each case is an observation and skips after its assertions.
func TestUlbiosUnansweredRequests(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startUlbios(t)
	if peer.assertedByReexec {
		return
	}
	c := ulbiosSilentClient(t)
	device := deviceObject(dev)
	av, _ := analogValueObject(dev)
	pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	p8 := uint8(8)

	encode := func(t *testing.T) func([]byte, error) []byte {
		return func(b []byte, err error) []byte {
			t.Helper()
			if err != nil {
				t.Fatal(err)
			}
			return b
		}
	}
	raw := func(t *testing.T, svc uint8, payload []byte, sideEffecting bool) error {
		t.Helper()
		ctx, cancel := withTimeout(t, 10*time.Second)
		defer cancel()
		_, err := c.InvokeConfirmed(ctx, peer.target, svc, payload, client.ConfirmedInvokeOptions{
			SegmentedResponseAccepted: true, SideEffecting: sideEffecting,
		})
		return err
	}
	observe := func(t *testing.T, svc int, mark int, what string) {
		t.Helper()
		reason := requireUnanswered(t, peer, svc, mark)
		t.Skipf("observation (known difference): %s gets no reply from ulbios; adapter: %s", what, reason)
	}

	readCases := []struct {
		name string
		call func(t *testing.T) error
	}{
		{"rp-property-above-255", func(t *testing.T) error {
			ctx, cancel := withTimeout(t, 10*time.Second)
			defer cancel()
			_, err := c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: unknownPropertyID})
			return err
		}},
		{"rp-array-index", func(t *testing.T) error {
			ctx, cancel := withTimeout(t, 10*time.Second)
			defer cancel()
			one := uint32(1)
			_, err := c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &one})
			return err
		}},
	}
	for _, tc := range readCases {
		t.Run(tc.name, func(t *testing.T) {
			mark := evidenceMark(peer)
			requireReadTimeout(t, tc.name, tc.call(t))
			observe(t, apdu.ServiceReadProperty, mark, tc.name)
		})
	}

	writeCases := []struct {
		name string
		call func(t *testing.T) error
	}{
		// objects.DecNull checks the tag against TagReal and refuses every NULL.
		{"wp-null-relinquish", func(t *testing.T) error {
			ctx, cancel := withTimeout(t, 10*time.Second)
			defer cancel()
			return c.RelinquishPriority(ctx, peer.target, av, 8)
		}},
		{"wp-character-string", func(t *testing.T) error {
			ctx, cancel := withTimeout(t, 10*time.Second)
			defer cancel()
			return c.WriteProperty(ctx, peer.target, av, pv, bacnet.ApplicationValue{Kind: bacnet.ValueCharacterString,
				Character: bacnet.CharacterString{Value: "hello"}}, &p8)
		}},
		{"wp-boolean", func(t *testing.T) error {
			ctx, cancel := withTimeout(t, 10*time.Second)
			defer cancel()
			return c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyOutOfService}, bacnet.BoolValue(true), nil)
		}},
		{"wp-array-index", func(t *testing.T) error {
			ctx, cancel := withTimeout(t, 10*time.Second)
			defer cancel()
			idx := uint32(8)
			return c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &idx}, bacnet.RealValue(1), nil)
		}},
		// Five tokens, like ulbios' own form: ConfirmedWriteProperty.Decode
		// takes them and DecReal refuses the array index in the third.
		{"wp-array-index-with-priority", func(t *testing.T) error {
			ctx, cancel := withTimeout(t, 10*time.Second)
			defer cancel()
			idx := uint32(8)
			return c.WriteProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &idx}, bacnet.RealValue(1), &p8)
		}},
		{"wp-priority-17", func(t *testing.T) error {
			// Clause 15.9.1.1.5: priority 1..16. go-bacnet does not send 17,
			// so the context tag 4 is appended by hand.
			payload := encode(t)(service.EncodeWriteProperty(service.WritePropertyRequest{Object: av, Property: pv, Value: bacnet.RealValue(1)}))
			return raw(t, apdu.ServiceWriteProperty, append(payload, 0x49, 17), true)
		}},
	}
	for _, tc := range writeCases {
		t.Run(tc.name, func(t *testing.T) {
			mark := evidenceMark(peer)
			requireWriteOutcomeUnknown(t, tc.name, tc.call(t))
			observe(t, apdu.ServiceWriteProperty, mark, tc.name)
		})
	}

	// go-bacnet's service policy table makes a request of a state-changing
	// service outcome-unknown once it is sent (client/confirmed.go
	// maybeUnknown), also through InvokeConfirmed.
	services := []struct {
		name     string
		svc      uint8
		changing bool
		payload  func(t *testing.T) []byte
	}{
		{"read-property-multiple", apdu.ServiceReadPropertyMultiple, false, func(t *testing.T) []byte {
			return encode(t)(service.EncodeReadPropertyMultiple([]service.ReadAccessSpecification{{Object: av, Properties: []bacnet.PropertyReference{pv}}}))
		}},
		{"write-property-multiple", apdu.ServiceWritePropertyMultiple, true, func(t *testing.T) []byte {
			return encode(t)(service.EncodeWritePropertyMultiple([]service.WriteAccessSpecification{{Object: av,
				Properties: []service.WritePropertyValue{{Property: pv, Value: bacnet.RealValue(1), Priority: &p8}}}}))
		}},
		{"subscribe-cov", apdu.ServiceSubscribeCOV, true, func(t *testing.T) []byte {
			return encode(t)(service.EncodeSubscribeCOV(service.SubscribeCOVRequest{ProcessIdentifier: 1, MonitoredObject: av, Lifetime: 60}))
		}},
		{"read-range", apdu.ServiceReadRange, false, func(t *testing.T) []byte {
			tl, _ := trendLogObject(dev)
			return encode(t)(service.EncodeReadRange(service.ReadRangeRequest{Object: tl,
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyLogBuffer}, By: service.ReadRangeByPosition, ReferenceIndex: 1, Count: 1}))
		}},
		{"device-communication-control", apdu.ServiceDeviceCommunicationControl, true, func(t *testing.T) []byte {
			return encode(t)(service.EncodeDeviceCommunicationControl(service.DeviceCommunicationControlRequest{EnableDisable: service.EnableDisableEnable}))
		}},
		{"reinitialize-device", apdu.ServiceReinitializeDevice, true, func(t *testing.T) []byte {
			return encode(t)(service.EncodeReinitializeDevice(service.ReinitializeDeviceRequest{State: service.ReinitializedWarmstart}))
		}},
		{"unrecognized-service", unrecognizedConfirmedService, false, func(*testing.T) []byte { return nil }},
	}
	for _, tc := range services {
		t.Run(tc.name, func(t *testing.T) {
			mark := evidenceMark(peer)
			err := raw(t, tc.svc, tc.payload(t), false)
			if tc.changing {
				requireWriteOutcomeUnknown(t, tc.name, err)
			} else {
				requireReadTimeout(t, tc.name, err)
			}
			observe(t, int(tc.svc), mark, fmt.Sprintf("%s (service %d)", tc.name, tc.svc))
		})
	}

	// The silence is per request: the device still answers afterwards.
	t.Run("still-answers", func(t *testing.T) {
		ctx, cancel := withTimeout(t, 10*time.Second)
		defer cancel()
		v, err := c.ReadProperty(ctx, peer.target, device, bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier})
		if err != nil {
			t.Fatalf("ReadProperty after the unanswered requests: %v", err)
		}
		if id, err := bacnet.AsObjectID(v); err != nil || id != device {
			t.Fatalf("object-identifier %v (%v)", id, err)
		}
	})
}

// An Object_List that does not fit the client's Max_APDU: ulbios cannot
// segment and cannot send the Abort of clause 5.4.5, so the whole-array
// read gets no answer and ReadObjectList, which falls back to element reads
// on an Abort only, ends in ErrTimeout. With the default Max_APDU the list
// comes back in one reply.
func TestUlbiosObjectListTooLong(t *testing.T) {
	requireAdapterVersion(t, ulbiosImage(), ulbiosMinAdapterVersion)
	forPeersWithFixture(t, []interopPeer{ulbiosPeer()}, largeDeviceFixturePath, largeDeviceFixture, func(t *testing.T, peer *peerHandle) {
		dev := loadDeviceFixture(t)
		if dev.Fixture != largeDeviceFixture {
			t.Fatalf("loaded fixture %q, want %q", dev.Fixture, largeDeviceFixture)
		}
		ctx, cancel := withTimeout(t, 30*time.Second)
		defer cancel()
		device := deviceObject(dev)

		list, err := newClient(t).ReadObjectList(ctx, peer.target, device, 0)
		if err != nil {
			t.Fatalf("ReadObjectList with the default Max_APDU: %v", err)
		}
		if len(list) != len(dev.Objects) {
			t.Fatalf("object-list has %d entries, the fixture declares %d", len(list), len(dev.Objects))
		}

		// 206 octets (max-APDU code 2): the list of 5-octet identifiers does not fit.
		if 5*len(list) <= 206 {
			t.Fatalf("object-list of %d entries fits 206 octets; the fixture is too small for this test", len(list))
		}
		narrow := newClientOpts(t, client.WithAdvertisedMaxAPDU(206),
			client.WithTransactionOptions(ulbiosSilentTimeout, 0, ulbiosSilentTimeout))
		mark := evidenceMark(peer)
		_, err = narrow.ReadObjectList(ctx, peer.target, device, 0)
		requireReadTimeout(t, "ReadObjectList with Max_APDU 206", err)
		reason := requireUnanswered(t, peer, apdu.ServiceReadProperty, mark)
		t.Skipf("observation (known difference): an answer longer than the client's Max_APDU gets neither a reply nor an Abort; adapter: %s", reason)
	})
}

// ulbios' bacnet.Parse identifies a confirmed request by its whole first
// octet, so a request with segmented-response-accepted (SA, clause 20.1.2.4)
// set is not recognised. go-bacnet sets SA by default, like BACnet4J; the
// adapter clears the bit before parsing (adapter-shim) unless
// ULBIOS_KEEP_SA=1. With the bit kept, go-bacnet's ReadProperty gets no
// answer, and a request without SA does.
func TestUlbiosSegmentedResponseAcceptedBit(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startUlbios(t, "ULBIOS_KEEP_SA=1")
	if peer.assertedByReexec {
		return
	}
	c := ulbiosSilentClient(t)
	ctx, cancel := withTimeout(t, 15*time.Second)
	defer cancel()
	ref := bacnet.PropertyReference{Identifier: bacnet.PropertyObjectIdentifier}
	device := deviceObject(dev)

	payload, err := service.EncodeReadProperty(service.ReadPropertyRequest{Object: device, Property: ref})
	if err != nil {
		t.Fatal(err)
	}
	pdu, err := c.InvokeConfirmed(ctx, peer.target, apdu.ServiceReadProperty, payload, client.ConfirmedInvokeOptions{SegmentedResponseAccepted: false})
	if err != nil || pdu.ComplexACK == nil {
		t.Fatalf("ReadProperty without SA: %v %+v", err, pdu)
	}
	ack, err := service.DecodeReadPropertyACK(pdu.ComplexACK.Payload, bacnet.DefaultDecodeLimits())
	if err != nil {
		t.Fatal(err)
	}
	if id, err := bacnet.AsObjectID(ack.Value); err != nil || id != device || ack.Object != device {
		t.Fatalf("ACK %+v (%v), want %v", ack, err, device)
	}

	mark := evidenceMark(peer)
	_, err = c.ReadProperty(ctx, peer.target, device, ref)
	requireReadTimeout(t, "ReadProperty with SA", err)
	reason := requireUnanswered(t, peer, apdu.ServiceReadProperty, mark)
	t.Skipf("observation (known difference): with SA set ulbios does not recognise the request; adapter: %s", reason)
}
