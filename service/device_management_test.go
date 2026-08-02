// SPDX-License-Identifier: MIT

package service

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
)

func TestDCCAndReinitDuplicateOptionals(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	d := uint16(5)
	pw := bacnet.CharacterString{Value: "secret"}
	dcc, err := EncodeDeviceCommunicationControl(DeviceCommunicationControlRequest{
		TimeDuration:  &d,
		EnableDisable: EnableDisableEnable,
		Password:      &pw,
	})
	if err != nil {
		t.Fatal(err)
	}
	dur, err := bacnet.AppendContextUnsigned(nil, 0, 6)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeDeviceCommunicationControl(append(dcc, dur...), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate duration: %v", err)
	}
	pwRaw, err := bacnet.AppendContextCharacterString(nil, 2, bacnet.CharacterString{Value: "other"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeDeviceCommunicationControl(append(dcc, pwRaw...), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate password: %v", err)
	}

	reinit, err := EncodeReinitializeDevice(ReinitializeDeviceRequest{
		State:    ReinitializedWarmstart,
		Password: &pw,
	})
	if err != nil {
		t.Fatal(err)
	}
	pw1, err := bacnet.AppendContextCharacterString(nil, 1, bacnet.CharacterString{Value: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeReinitializeDevice(append(reinit, pw1...), limits); !errors.Is(err, bacnet.ErrMalformed) {
		t.Fatalf("duplicate reinit password: %v", err)
	}
}
