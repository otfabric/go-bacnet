//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

// BACnet4J behind the native bacnet-stack router: the same scenario the
// bacnet-stack, BACpypes3 and System.IO.BACnet devices run.
func TestBACnet4JNativeRouter(t *testing.T) {
	requireAdapterVersion(t, bacnet4jImage(), "0.10.0")
	requireAdapterVersion(t, stackRouterImage(), "0.10.0")
	runNativeRouter(t, bacnet4jPeer(), nil)
}

// DeviceCommunicationControl "disable-initiation" on bacnet-stack: the device
// must keep answering requests (only its own initiation stops).
func TestBacnetStackDeviceCommunicationControlDisableInitiation(t *testing.T) {
	forPeers(t, []interopPeer{stackPeer()}, func(t *testing.T, dev deviceFixture, peer *peerHandle) {
		c := newClientOpts(t, client.WithDeviceManagementEnabled(client.DeviceManagementConfirm))
		ctx, cancel := withTimeout(t, 15*time.Second)
		defer cancel()
		minutes := uint16(1)
		err := c.DeviceCommunicationControl(ctx, peer.target, service.DeviceCommunicationControlRequest{
			EnableDisable: service.EnableDisableDisableInitiation, TimeDuration: &minutes,
		})
		if err != nil {
			t.Fatalf("DCC disable-initiation: %v", err)
		}
		val, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: bacnet.PropertyObjectName})
		if err != nil {
			t.Fatalf("ReadProperty while initiation is disabled: %v", err)
		}
		if got := characterString(t, val); got != dev.DeviceName {
			t.Fatalf("object-name %q", got)
		}
		if err := c.DeviceCommunicationControl(ctx, peer.target, service.DeviceCommunicationControlRequest{
			EnableDisable: service.EnableDisableEnable,
		}); err != nil {
			t.Fatalf("DCC enable: %v", err)
		}
	})
}

// The Worldiety adapter's Analog Value is not commandable: it has no
// Priority_Array, a write at a priority simply sets Present_Value, and a
// relinquish (NULL) is refused as a value of the wrong type. Asserted so the
// difference from the commandable peers is on record.
func TestWorldietyAnalogValueNotCommandable(t *testing.T) {
	forPeers(t, []interopPeer{worldietyPeer()}, func(t *testing.T, dev deviceFixture, peer *peerHandle) {
		c := newClient(t)
		ctx, cancel := withTimeout(t, 15*time.Second)
		defer cancel()
		av, baseline := analogValueObject(dev)
		pv := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
		t.Cleanup(func() {
			rctx, rcancel := withTimeout(t, 5*time.Second)
			defer rcancel()
			_ = c.WriteProperty(rctx, peer.target, av, pv, bacnet.RealValue(baseline), nil)
		})
		if err := c.WritePriority(ctx, peer.target, av, 5, bacnet.RealValue(77.5)); err != nil {
			t.Fatalf("WritePriority: %v", err)
		}
		v, err := c.ReadProperty(ctx, peer.target, av, pv)
		if err != nil || math.Abs(float64(v.Real-77.5)) > 0.01 {
			t.Fatalf("present-value after WritePriority: %+v, %v", v, err)
		}
		idx := uint32(5)
		_, err = c.ReadProperty(ctx, peer.target, av, bacnet.PropertyReference{Identifier: bacnet.PropertyPriorityArray, ArrayIndex: &idx})
		var remote *bacnet.ErrorResponse
		if !errors.As(err, &remote) || remote.Class != bacnet.ErrorClassProperty || remote.Code != bacnet.ErrorCodeUnknownProperty {
			t.Fatalf("priority-array[5]: %v, want property/unknown-property", err)
		}
		err = c.RelinquishPriority(ctx, peer.target, av, 5)
		if !errors.As(err, &remote) || remote.Class != bacnet.ErrorClassProperty || remote.Code != bacnet.ErrorCodeValueOutOfRange {
			t.Fatalf("RelinquishPriority: %v, want property/value-out-of-range", err)
		}
		v, err = c.ReadProperty(ctx, peer.target, av, pv)
		if err != nil || math.Abs(float64(v.Real-77.5)) > 0.01 {
			t.Fatalf("present-value after the refused relinquish: %+v, %v (it must be unchanged)", v, err)
		}
	})
}
