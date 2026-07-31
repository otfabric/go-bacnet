// SPDX-License-Identifier: MIT

package apdu_test

import (
	"testing"

	"github.com/otfabric/go-bacnet/apdu"
)

func TestEncodeDecodeMaxAPDUSize(t *testing.T) {
	cases := []struct {
		size uint16
		code apdu.MaxAPDUCode
	}{
		{50, apdu.MaxAPDU50},
		{127, apdu.MaxAPDU50},
		{128, apdu.MaxAPDU128},
		{206, apdu.MaxAPDU206},
		{480, apdu.MaxAPDU480},
		{1024, apdu.MaxAPDU1024},
		{1476, apdu.MaxAPDU1476},
		{2000, apdu.MaxAPDU1476},
	}
	for _, tc := range cases {
		code, err := apdu.EncodeMaxAPDUSize(tc.size)
		if err != nil || code != tc.code {
			t.Fatalf("size=%d code=%v err=%v want %v", tc.size, code, err, tc.code)
		}
		got, ok := apdu.DecodeMaxAPDUSize(tc.code)
		if !ok {
			t.Fatalf("decode code %v", tc.code)
		}
		if got == 0 {
			t.Fatalf("decoded size 0 for %v", tc.code)
		}
	}
	if _, err := apdu.EncodeMaxAPDUSize(49); err == nil {
		t.Fatal("expected error for size < 50")
	}
	if _, ok := apdu.DecodeMaxAPDUSize(apdu.MaxAPDUCode(9)); ok {
		t.Fatal("expected false for unknown code")
	}
}

func TestEncodeMaxSegments(t *testing.T) {
	cases := []struct {
		count int
		want  apdu.MaxSegmentsCode
	}{
		{0, apdu.MaxSegmentsUnspecified},
		{1, apdu.MaxSegmentsUnspecified},
		{2, apdu.MaxSegments2},
		{3, apdu.MaxSegments2},
		{4, apdu.MaxSegments4},
		{8, apdu.MaxSegments8},
		{16, apdu.MaxSegments16},
		{32, apdu.MaxSegments32},
		{63, apdu.MaxSegments32},
		{64, apdu.MaxSegments64},
		{65, apdu.MaxSegmentsGT64},
		{128, apdu.MaxSegmentsGT64},
	}
	for _, tc := range cases {
		if got := apdu.EncodeMaxSegments(tc.count); got != tc.want {
			t.Fatalf("count=%d got %v want %v", tc.count, got, tc.want)
		}
	}
}
