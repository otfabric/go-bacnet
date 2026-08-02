// SPDX-License-Identifier: MIT

package service_test

import (
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

func FuzzDecodeDeviceCommunicationControl(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeDeviceCommunicationControl(service.DeviceCommunicationControlRequest{
		EnableDisable: service.EnableDisableEnable,
	}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeDeviceCommunicationControl(data, limits)
	})
}

func FuzzDecodeReinitializeDevice(f *testing.F) {
	limits := bacnet.DefaultDecodeLimits()
	f.Add([]byte(nil))
	if p, err := service.EncodeReinitializeDevice(service.ReinitializeDeviceRequest{
		State: service.ReinitializedWarmstart,
	}); err == nil {
		f.Add(p)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = service.DecodeReinitializeDevice(data, limits)
	})
}
