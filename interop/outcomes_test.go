//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"errors"
	"testing"
	"time"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/client"
	"github.com/otfabric/go-bacnet/service"
)

func TestBacnetStackReadPropertyUnknownPropertyError(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	_, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: unknownPropertyID})
	if err == nil {
		t.Fatal("expected Error PDU for unknown property")
	}
	assertErrorUnknownProperty(t, err)
}

func TestBACpypes3ReadPropertyUnknownPropertyError(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	_, err := c.ReadProperty(ctx, peer.target, deviceObject(dev), bacnet.PropertyReference{Identifier: unknownPropertyID})
	if err == nil {
		t.Fatal("expected Error PDU for unknown property")
	}
	assertErrorUnknownProperty(t, err)
}

func TestBacnetStackRejectUnrecognizedService(t *testing.T) {
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	c := newClient(t)
	ctx, cancel := withTimeout(t, 8*time.Second)
	defer cancel()

	_, err := c.InvokeConfirmed(ctx, peer.target, unrecognizedConfirmedService, nil, client.ConfirmedInvokeOptions{
		SegmentedResponseAccepted: true,
	})
	if err == nil {
		t.Fatal("expected Reject PDU for unrecognized service")
	}
	assertRejectUnrecognized(t, err)
}

func TestBacnetStackAbortSegmentationNotSupported(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACNET_STACK_IMAGE", defaultStackImage), "bacnet-stack")
	if peer.assertedByReexec {
		return
	}
	// bacserv does not segment ComplexACK; a large RPM against a tiny client
	// MaxAPDU yields Abort reason segmentation-not-supported (4).
	c := newClientWithAdvertisedMaxAPDU(t, 50)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()

	_, err := c.ReadPropertyMultiple(ctx, peer.target, []service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyObjectIdentifier},
				{Identifier: bacnet.PropertyObjectType},
				{Identifier: bacnet.PropertyVendorIdentifier},
				{Identifier: bacnet.PropertyModelName},
				{Identifier: bacnet.PropertyDescription},
				{Identifier: bacnet.PropertySystemStatus},
				{Identifier: bacnet.PropertyProtocolVersion},
				{Identifier: bacnet.PropertyProtocolRevision},
				{Identifier: bacnet.PropertyMaxAPDULength},
				{Identifier: bacnet.PropertySegmentation},
			},
		},
	})
	if err == nil {
		t.Fatal("expected Abort when bacserv cannot segment the RPM ACK")
	}
	var ae *bacnet.AbortError
	if !errors.As(err, &ae) {
		t.Fatalf("want *AbortError, got %T: %v", err, err)
	}
	if !ae.Server || ae.Reason != 4 {
		t.Fatalf("Abort server=%v reason=%d, want server=true reason=4", ae.Server, ae.Reason)
	}
}

func TestBACpypes3AbortWhenSegmentedResponseNotAccepted(t *testing.T) {
	dev := loadDeviceFixture(t)
	peer := startPeer(t, getEnv("BACPYPES3_IMAGE", defaultBACpypes3Image), "bacpypes3")
	if peer.assertedByReexec {
		return
	}
	// Segmentation is driven by the client's advertised MaxAPDU in the request header.
	c := newClientWithAdvertisedMaxAPDU(t, 50)
	ctx, cancel := withTimeout(t, 10*time.Second)
	defer cancel()

	av, _ := analogValueObject(dev)
	payload, err := service.EncodeReadPropertyMultiple([]service.ReadAccessSpecification{
		{
			Object: deviceObject(dev),
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyObjectIdentifier},
				{Identifier: bacnet.PropertyObjectType},
				{Identifier: bacnet.PropertyVendorIdentifier},
				{Identifier: bacnet.PropertyModelName},
				{Identifier: bacnet.PropertyDescription},
				{Identifier: bacnet.PropertySystemStatus},
				{Identifier: bacnet.PropertyProtocolVersion},
				{Identifier: bacnet.PropertyProtocolRevision},
				{Identifier: bacnet.PropertyMaxAPDULength},
				{Identifier: bacnet.PropertySegmentation},
			},
		},
		{
			Object: av,
			Properties: []bacnet.PropertyReference{
				{Identifier: bacnet.PropertyObjectName},
				{Identifier: bacnet.PropertyPresentValue},
				{Identifier: bacnet.PropertyDescription},
				{Identifier: bacnet.PropertyStatusFlags},
			},
		},
	})
	if err != nil {
		t.Fatalf("EncodeReadPropertyMultiple: %v", err)
	}

	_, err = c.InvokeConfirmed(ctx, peer.target, apdu.ServiceReadPropertyMultiple, payload, client.ConfirmedInvokeOptions{
		SegmentedResponseAccepted: false,
	})
	if err == nil {
		t.Fatal("expected Abort when segmented response is required but not accepted")
	}
	var ae *bacnet.AbortError
	if !errors.As(err, &ae) {
		t.Fatalf("want *AbortError, got %T: %v", err, err)
	}
	if !ae.Server {
		t.Fatalf("Abort.Server=%v, want true", ae.Server)
	}
}
