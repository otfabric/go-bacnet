// SPDX-License-Identifier: MIT

package client

import (
	"fmt"
	"math"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// validateAdvertisedMaxAPDU checks advertised vs parser MaxAPDU at construction.
// advertised 0 means "use parserMax". parserMax must be positive after Normalize.
func validateAdvertisedMaxAPDU(advertised uint16, parserMax int) error {
	if parserMax <= 0 {
		return fmt.Errorf("%w: DecodeLimits.MaxAPDUSize must be positive", bacnet.ErrMalformed)
	}
	if parserMax > math.MaxUint16 {
		return fmt.Errorf("%w: DecodeLimits.MaxAPDUSize %d exceeds uint16", bacnet.ErrMalformed, parserMax)
	}
	effective := advertised
	if effective == 0 {
		effective = uint16(parserMax)
	}
	if effective < 50 {
		return fmt.Errorf("%w: advertised MaxAPDU %d below BACnet minimum 50", bacnet.ErrMalformed, effective)
	}
	if int(effective) > parserMax {
		return fmt.Errorf("%w: advertised MaxAPDU %d exceeds parser MaxAPDUSize %d", bacnet.ErrMalformed, effective, parserMax)
	}
	if _, err := apdu.EncodeMaxAPDUSize(effective); err != nil {
		return fmt.Errorf("%w: %v", bacnet.ErrMalformed, err)
	}
	return nil
}

func (c *Client) advertisedMaxAPDU() (uint16, error) {
	localMax := c.cfg.advertisedMaxAPDU
	if localMax == 0 {
		if c.limits.MaxAPDUSize > math.MaxUint16 {
			return 0, fmt.Errorf("%w: DecodeLimits.MaxAPDUSize exceeds uint16", bacnet.ErrMalformed)
		}
		localMax = uint16(c.limits.MaxAPDUSize)
	}
	if localMax == 0 {
		localMax = 1476
	}
	return localMax, nil
}
