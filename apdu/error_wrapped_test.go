// SPDX-License-Identifier: MIT

package apdu_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
)

// Services whose Error PDU wraps the Error as error-type [0] and appends
// service-specific parameters must still yield the remote class and code.
func TestDecodeErrorWrappedForms(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	cases := []struct {
		name        string
		payload     []byte
		class, code uint16
		wrapped     bool
		params      int
		firstFailed int // -1 when absent
	}{
		{"bare application", []byte{0x91, 0x02, 0x91, 0x20}, 2, 32, false, 0, -1},
		{"bare context", []byte{0x09, 0x02, 0x19, 0x20}, 2, 32, false, 0, -1},
		{"CreateObject-Error", []byte{0x0E, 0x91, 0x02, 0x91, 0x25, 0x0F, 0x19, 0x01}, 2, 37, true, 1, 1},
		{"ChangeList-Error", []byte{0x0E, 0x91, 0x02, 0x91, 0x09, 0x0F, 0x19, 0x03}, 2, 9, true, 1, 3},
		{"ConfirmedPrivateTransfer-Error", []byte{0x0E, 0x91, 0x05, 0x91, 0x00, 0x0F, 0x19, 0x0A, 0x29, 0x01, 0x3E, 0x21, 0x07, 0x3F}, 5, 0, true, 3, 10},
		{"VTClose-Error", []byte{0x0E, 0x91, 0x06, 0x91, 0x22, 0x0F, 0x1E, 0x21, 0x05, 0x1F}, 6, 34, true, 1, -1},
		{"SubscribeCOVPropertyMultiple-Error", []byte{0x0E, 0x91, 0x02, 0x91, 0x2C, 0x0F, 0x1E, 0x0C, 0x00, 0x00, 0x00, 0x01, 0x1E, 0x09, 0x55, 0x1F, 0x1F}, 2, 44, true, 1, -1},
		{"WritePropertyMultiple-Error", []byte{0x0E, 0x91, 0x02, 0x91, 0x28, 0x0F, 0x1E, 0x0C, 0x00, 0x80, 0x00, 0x01, 0x19, 0x55, 0x1F}, 2, 40, true, 1, -1},
		{"wrapped, context-tagged inner error", []byte{0x0E, 0x09, 0x02, 0x19, 0x20, 0x0F, 0x19, 0x02}, 2, 32, true, 1, 2},
		{"wrapper only", []byte{0x0E, 0x91, 0x01, 0x91, 0x1F, 0x0F}, 1, 31, true, 0, -1},
	}
	for _, tc := range cases {
		class, code, err := apdu.DecodeErrorClassCode(tc.payload, limits)
		if err != nil || class != tc.class || code != tc.code {
			t.Fatalf("%s: DecodeErrorClassCode = %d/%d err=%v", tc.name, class, code, err)
		}
		d, err := apdu.DecodeError(tc.payload, limits)
		if err != nil {
			t.Fatalf("%s: DecodeError: %v", tc.name, err)
		}
		if d.Class != tc.class || d.Code != tc.code || d.Wrapped != tc.wrapped || len(d.Parameters) != tc.params {
			t.Fatalf("%s: %+v", tc.name, d)
		}
		n, ok := d.FirstFailedElementNumber()
		if ok != (tc.firstFailed >= 0) || (ok && int(n) != tc.firstFailed) {
			t.Fatalf("%s: FirstFailedElementNumber = %d, %v", tc.name, n, ok)
		}
	}
}

func TestDecodeErrorWrappedMalformed(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	for name, payload := range map[string][]byte{
		"empty":                 {},
		"empty wrapper":         {0x0E, 0x0F},
		"wrapper with one enum": {0x0E, 0x91, 0x02, 0x0F},
		"nested wrapper":        {0x0E, 0x0E, 0x91, 0x02, 0x91, 0x20, 0x0F, 0x0F},
		"unterminated wrapper":  {0x0E, 0x91, 0x02, 0x91, 0x20},
		"constructed [1] first": {0x1E, 0x91, 0x02, 0x91, 0x20, 0x1F},
	} {
		if _, err := apdu.DecodeError(payload, limits); !errors.Is(err, bacnet.ErrMalformed) {
			t.Fatalf("%s: err=%v", name, err)
		}
	}
}

// Parameters must not alias the caller's buffer.
func TestDecodeErrorParametersOwnMemory(t *testing.T) {
	payload := []byte{0x0E, 0x91, 0x05, 0x91, 0x00, 0x0F, 0x3E, 0x62, 0xAA, 0xBB, 0x3F}
	d, err := apdu.DecodeError(payload, bacnet.DefaultDecodeLimits())
	if err != nil || len(d.Parameters) != 1 {
		t.Fatalf("%+v err=%v", d, err)
	}
	for i := range payload {
		payload[i] = 0
	}
	got := d.Parameters[0].Value.Elements[0].Value.OctetString
	if len(got) != 2 || got[0] != 0xAA || got[1] != 0xBB {
		t.Fatalf("parameters alias input: % X", got)
	}
}

func FuzzDecodeError(f *testing.F) {
	for _, seed := range [][]byte{
		{0x91, 0x02, 0x91, 0x20},
		{0x09, 0x02, 0x19, 0x20},
		{0x0E, 0x91, 0x02, 0x91, 0x25, 0x0F, 0x19, 0x01},
		{0x0E, 0x91, 0x05, 0x91, 0x00, 0x0F, 0x19, 0x0A, 0x29, 0x01, 0x3E, 0x21, 0x07, 0x3F},
		{0x0E, 0x0F},
	} {
		f.Add(seed)
	}
	limits := bacnet.DefaultDecodeLimits()
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := apdu.DecodeError(data, limits)
		class, code, err2 := apdu.DecodeErrorClassCode(data, limits)
		if (err == nil) != (err2 == nil) {
			t.Fatalf("DecodeError err=%v, DecodeErrorClassCode err=%v", err, err2)
		}
		if err != nil {
			return
		}
		if d.Class != class || d.Code != code {
			t.Fatalf("class/code mismatch: %+v vs %d/%d", d, class, code)
		}
		if !d.Wrapped && d.Parameters != nil {
			t.Fatalf("bare error with parameters: %+v", d)
		}
		_, _ = d.FirstFailedElementNumber()
	})
}
