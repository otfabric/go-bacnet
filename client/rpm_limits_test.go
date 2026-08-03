// SPDX-License-Identifier: MIT

package client

import (
	"context"
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

func TestRPMBatchedEmptyAndTinyBudget(t *testing.T) {
	env := newVirtualPair(t)
	if _, err := env.Client.ReadPropertyMultipleBatched(context.Background(), env.Target, nil, RPMBatchOptions{}); err == nil {
		t.Fatal("empty specs")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeReadPropertyMultipleACK([]service.ReadAccessResult{{
			Object: obj,
			Properties: []service.PropertyResult{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.RealValue(1),
			}},
		}})
	})
	// Force tiny remote max via Target.MaxAPDU so budget clamps to 50.
	target := env.Target
	target.MaxAPDU = 40
	specs := []service.ReadAccessSpecification{
		{Object: obj, Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}}},
	}
	if _, err := env.Client.ReadPropertyMultipleBatched(ctx, target, specs, RPMBatchOptions{
		MaxPropertiesPerBatch: 8, MaxBatches: 8, SafetyMargin: 0,
	}); err != nil {
		t.Fatal(err)
	}
	if max(2, 1) != 2 || max(1, 2) != 2 {
		t.Fatal("max")
	}
}

func TestRPMBatchedMaxBatches(t *testing.T) {
	env := newVirtualPair(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	obj := bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: 1}
	go serveComplexACK(ctx, env.PeerTr, env.Local, func(choice uint8) ([]byte, error) {
		return service.EncodeReadPropertyMultipleACK([]service.ReadAccessResult{{
			Object: obj,
			Properties: []service.PropertyResult{{
				Property: bacnet.PropertyReference{Identifier: bacnet.PropertyPresentValue},
				Value:    bacnet.RealValue(1),
			}},
		}})
	})
	specs := make([]service.ReadAccessSpecification, 3)
	for i := range specs {
		specs[i] = service.ReadAccessSpecification{
			Object:     bacnet.ObjectIdentifier{Type: bacnet.ObjectTypeAnalogValue, Instance: uint32(i + 1)},
			Properties: []bacnet.PropertyReference{{Identifier: bacnet.PropertyPresentValue}},
		}
	}
	_, err := env.Client.ReadPropertyMultipleBatched(ctx, env.Target, specs, RPMBatchOptions{
		MaxPropertiesPerBatch: 1, MaxBatches: 1,
	})
	if !errors.Is(err, bacnet.ErrLimitExceeded) {
		t.Fatalf("got %v", err)
	}
	_ = apdu.ServiceReadPropertyMultiple
}
