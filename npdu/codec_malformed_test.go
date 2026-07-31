// SPDX-License-Identifier: MIT

package npdu_test

import (
	"errors"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/npdu"
)

func TestNPDUTruncationBranches(t *testing.T) {
	limits := bacnet.DefaultDecodeLimits()
	cases := []struct {
		name string
		raw  []byte
	}{
		{"dnet truncated", []byte{0x01, 0x20, 0x00}},
		{"dlen truncated", []byte{0x01, 0x20, 0x00, 0x02}},
		{"dadr truncated", []byte{0x01, 0x20, 0x00, 0x02, 0x06, 0x01}},
		{"hop truncated", []byte{0x01, 0x20, 0x00, 0x02, 0x00}},
		{"snet truncated", []byte{0x01, 0x08, 0x00}},
		{"slen truncated", []byte{0x01, 0x08, 0x00, 0x01}},
		{"slen zero", []byte{0x01, 0x08, 0x00, 0x01, 0x00}},
		{"sadr truncated", []byte{0x01, 0x08, 0x00, 0x01, 0x06, 0x01}},
		{"netmsg type truncated", []byte{0x01, 0x80}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := npdu.Parse(tc.raw, limits); !errors.Is(err, bacnet.ErrMalformed) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestNPDUSourceLocalStation(t *testing.T) {
	mac := bacnet.MustMAC([]byte{1, 2, 3, 4, 5, 6})
	raw, err := npdu.Append(nil, npdu.NPDU{
		Version: npdu.Version1,
		Source:  bacnet.LocalStation(mac),
		APDU:    []byte{0x10, 0x08},
	})
	if err != nil {
		t.Fatal(err)
	}
	n, _, err := npdu.Parse(raw, bacnet.DefaultDecodeLimits())
	if err != nil || n.Source.Scope() != bacnet.AddressLocalStation || n.Source.MAC() != mac {
		t.Fatalf("%v %#v", err, n.Source)
	}
}
