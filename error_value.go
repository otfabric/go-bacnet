// SPDX-License-Identifier: MIT

package bacnet

import "fmt"

// DecodeBACnetError parses a BACnet-Error structure.
//
// Two encodings appear on the wire:
//  1. Context [0] class / [1] code (unsigned or enumerated content) — used by
//     this library's encoder and some goldens;
//  2. Application ENUMERATED class then ENUMERATED code — used by bacnet-stack
//     and BACpypes3 Error PDUs and RPM propertyAccessError.
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

// EncodeBACnetError encodes Error Class/Code as context [0]/[1].
func EncodeBACnetError(dst []byte, class, code uint16) ([]byte, error) {
	var err error
	dst, err = AppendContextUnsigned(dst, 0, uint64(class))
	if err != nil {
		return dst, err
	}
	return AppendContextUnsigned(dst, 1, uint64(code))
}
