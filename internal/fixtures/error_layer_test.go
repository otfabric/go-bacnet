// SPDX-License-Identifier: MIT

package fixtures_test

import (
	"fmt"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/internal/fixtures"
	"github.com/otfabric/go-bacnet/service"
)

func TestAssertExpectedErrorLayerPipeline(t *testing.T) {
	meta := fixtures.Meta{
		ExpectedError: &fixtures.ExpectedError{Category: "malformed", Layer: "service"},
	}
	// Earlier stage must succeed when the failure is expected later.
	assertExpectedError(t, meta, nil, "apdu")
	assertExpectedError(t, meta, fmt.Errorf("%w: empty ReadProperty", bacnet.ErrMalformed), "service")
}

func TestServiceLayerMalformedReadPropertyFixturePath(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	raw := []byte{0x02, 0x63, 0x01, 0x0c} // confirmed RP, empty service payload
	pdu, err := apdu.Parse(raw, limits)
	if err != nil {
		t.Fatalf("APDU stage must succeed: %v", err)
	}
	_, err = service.DecodeReadProperty(pdu.ConfirmedRequest.Payload, limits)
	meta := fixtures.Meta{
		ExpectedError: &fixtures.ExpectedError{Category: "malformed", Layer: "service"},
	}
	assertExpectedError(t, meta, nil, "apdu")
	assertExpectedError(t, meta, err, "service")
}
