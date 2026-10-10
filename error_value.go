// SPDX-License-Identifier: MIT

package bacnet

import "fmt"

// DecodeBACnetError parses a BACnet-Error structure.
//
// Two encodings appear on the wire:
//  1. Application ENUMERATED class then ENUMERATED code — the standard form,
//     sent by every peer stack and by this library's encoder;
//  2. Context [0] class / [1] code (unsigned or enumerated content) — a
//     non-standard form this library emitted up to v0.3.0, still accepted.
func DecodeBACnetError(elements []Element) (class, code uint16, err error) {
	if len(elements) == 0 {
		return 0, 0, fmt.Errorf("%w: empty BACnet-Error", ErrMalformed)
	}
	for _, el := range elements {
		if el.Opening || el.Closing || IsContextConstructed(el) {
			return 0, 0, fmt.Errorf("%w: unexpected constructed in BACnet-Error", ErrMalformed)
		}
	}

	allContext := true
	allApp := true
	for _, el := range elements {
		if el.Context {
			allApp = false
		} else {
			allContext = false
		}
	}
	switch {
	case allContext:
		return decodeBACnetErrorContext(elements)
	case allApp:
		return decodeBACnetErrorApplication(elements)
	default:
		return 0, 0, fmt.Errorf("%w: mixed context/application BACnet-Error", ErrMalformed)
	}
}

func decodeBACnetErrorContext(elements []Element) (class, code uint16, err error) {
	var haveClass, haveCode bool
	for _, el := range elements {
		u, e := errorFieldUint(el)
		if e != nil {
			return 0, 0, e
		}
		switch el.TagNumber {
		case 0:
			if haveClass {
				return 0, 0, fmt.Errorf("%w: duplicate Error Class", ErrMalformed)
			}
			class = u
			haveClass = true
		case 1:
			if haveCode {
				return 0, 0, fmt.Errorf("%w: duplicate Error Code", ErrMalformed)
			}
			code = u
			haveCode = true
		default:
			return 0, 0, fmt.Errorf("%w: unexpected BACnet-Error tag %d", ErrMalformed, el.TagNumber)
		}
	}
	if !haveClass || !haveCode {
		return 0, 0, fmt.Errorf("%w: BACnet-Error missing class/code", ErrMalformed)
	}
	return class, code, nil
}

func decodeBACnetErrorApplication(elements []Element) (class, code uint16, err error) {
	if len(elements) != 2 {
		return 0, 0, fmt.Errorf("%w: application BACnet-Error expects class then code", ErrMalformed)
	}
	class, err = errorFieldUint(elements[0])
	if err != nil {
		return 0, 0, err
	}
	code, err = errorFieldUint(elements[1])
	if err != nil {
		return 0, 0, err
	}
	return class, code, nil
}

func errorFieldUint(el Element) (uint16, error) {
	switch el.Value.Kind {
	case ValueEnumerated:
		if uint64(el.Value.Enumerated) > 0xFFFF {
			return 0, fmt.Errorf("%w: BACnet-Error field overflow", ErrMalformed)
		}
		return uint16(el.Value.Enumerated), nil
	case ValueUnsigned:
		if el.Value.Unsigned > 0xFFFF {
			return 0, fmt.Errorf("%w: BACnet-Error field overflow", ErrMalformed)
		}
		return uint16(el.Value.Unsigned), nil
	default:
		// Context primitives often surface as unsigned via ContextUnsigned.
		if el.Context {
			u, err := ContextUnsigned(el)
			if err != nil {
				return 0, err
			}
			if u > 0xFFFF {
				return 0, fmt.Errorf("%w: BACnet-Error field overflow", ErrMalformed)
			}
			return uint16(u), nil
		}
		return 0, fmt.Errorf("%w: BACnet-Error field kind %v", ErrMalformed, el.Value.Kind)
	}
}

// EncodeBACnetError encodes the BACnet Error production,
//
//	Error ::= SEQUENCE { error-class ENUMERATED, error-code ENUMERATED }
//
// as two application-tagged ENUMERATEDs (class 2, code 32 → 91 02 91 20).
// Releases up to v0.3.0 emitted context tags [0] and [1] instead, which no
// other stack produces; DecodeBACnetError still reads that form.
func EncodeBACnetError(dst []byte, class, code uint16) ([]byte, error) {
	dst, err := AppendApplicationValue(dst, EnumValue(uint32(class)))
	if err != nil {
		return dst, err
	}
	return AppendApplicationValue(dst, EnumValue(uint32(code)))
}
