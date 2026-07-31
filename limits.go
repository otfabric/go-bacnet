// SPDX-License-Identifier: MIT

package bacnet

// DecodeLimits bounds parser and reassembly allocations.
// Application-configured hard bounds win over peer-advertised maxima.
type DecodeLimits struct {
	MaxDatagramSize      int
	MaxAPDUSize          int
	MaxConstructedDepth  int
	MaxElements          int
	MaxOctetStringLength int
	MaxCharacterLength   int
	MaxBitStringBits     int
	MaxSegments          int
	MaxReassembledAPDU   int
}

// DefaultDecodeLimits returns conservative Horizon 1 defaults.
func DefaultDecodeLimits() DecodeLimits {
	return DecodeLimits{
		MaxDatagramSize:      65535,
		MaxAPDUSize:          1476,
		MaxConstructedDepth:  32,
		MaxElements:          4096,
		MaxOctetStringLength: 65535,
		MaxCharacterLength:   65535,
		MaxBitStringBits:     65535,
		MaxSegments:          64,
		MaxReassembledAPDU:   65535,
	}
}

func (l DecodeLimits) withDefaults() DecodeLimits {
	d := DefaultDecodeLimits()
	if l.MaxDatagramSize <= 0 {
		l.MaxDatagramSize = d.MaxDatagramSize
	}
	if l.MaxAPDUSize <= 0 {
		l.MaxAPDUSize = d.MaxAPDUSize
	}
	if l.MaxConstructedDepth <= 0 {
		l.MaxConstructedDepth = d.MaxConstructedDepth
	}
	if l.MaxElements <= 0 {
		l.MaxElements = d.MaxElements
	}
	if l.MaxOctetStringLength <= 0 {
		l.MaxOctetStringLength = d.MaxOctetStringLength
	}
	if l.MaxCharacterLength <= 0 {
		l.MaxCharacterLength = d.MaxCharacterLength
	}
	if l.MaxBitStringBits <= 0 {
		l.MaxBitStringBits = d.MaxBitStringBits
	}
	if l.MaxSegments <= 0 {
		l.MaxSegments = d.MaxSegments
	}
	if l.MaxReassembledAPDU <= 0 {
		l.MaxReassembledAPDU = d.MaxReassembledAPDU
	}
	return l
}

// Normalize returns limits with zeros replaced by defaults.
func (l DecodeLimits) Normalize() DecodeLimits { return l.withDefaults() }
