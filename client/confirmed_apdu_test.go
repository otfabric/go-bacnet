// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestReadPropertyAPDUTooLarge(t *testing.T) {
	env := newVirtualPair(t)
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogInput, Instance: 1}
	prop := bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue}
	target := env.Target
	target.MaxAPDU = 10

	_, err := env.Client.ReadProperty(context.Background(), target, obj, prop)
	var tooLarge *bacnet.APDUTooLargeError
	if !errors.As(err, &tooLarge) {
		t.Fatalf("expected APDUTooLargeError, got %v", err)
	}
}

func TestAdvertisedMaxAPDUErrorPath(t *testing.T) {
	env := newVirtualPair(t)
	limits := env.Client.limits
	limits.MaxAPDUSize = 1 << 30
	env.Client.limits = limits
	if _, err := env.Client.advertisedMaxAPDU(); err == nil {
		t.Fatal("expected overflow error")
	}
}
