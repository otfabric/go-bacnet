// SPDX-License-Identifier: MIT

package capture

import (
	"sort"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/service"
)

// Identity is what a device said about itself in a capture: its I-Am and
// the Device object properties it returned in ReadProperty and
// ReadPropertyMultiple answers.
type Identity struct {
	Station          string `json:"station"` // Endpoint of the device (B/IP node | NPDU address)
	Instance         uint32 `json:"instance"`
	VendorID         uint16 `json:"vendor_id"`
	VendorName       string `json:"vendor_name,omitempty"`
	ModelName        string `json:"model_name,omitempty"`
	Firmware         string `json:"firmware_revision,omitempty"`
	Application      string `json:"application_software_version,omitempty"`
	ProtocolRevision uint64 `json:"protocol_revision,omitempty"`
	MaxAPDU          uint16 `json:"max_apdu,omitempty"`
	Segmentation     uint8  `json:"segmentation"`
	// Frames are the frames the identity was read from.
	Frames []int `json:"frames"`
}

// Identities collects the identity of every device seen answering in msgs,
// keyed by station.
func Identities(msgs []Message) map[string]*Identity {
	out := map[string]*Identity{}
	get := func(m Message) *Identity {
		st := Endpoint(m, true)
		id := out[st]
		if id == nil {
			id = &Identity{Station: st}
			out[st] = id
		}
		return id
	}
	for _, m := range msgs {
		switch v := m.Service.(type) {
		case service.IAm:
			id := get(m)
			if id.Instance == 0 && id.VendorID == 0 {
				id.Instance, id.VendorID = v.Device.Instance, v.VendorID
				id.MaxAPDU, id.Segmentation = v.MaxAPDULength, v.Segmentation
				id.Frames = appendFrame(id.Frames, m.Frame)
			}
		case service.ReadPropertyACK:
			if v.Object.Type == bacnet.ObjectTypeDevice && v.Property.ArrayIndex == nil {
				id := get(m)
				if id.set(v.Property.Identifier, v.Value) {
					id.Frames = appendFrame(id.Frames, m.Frame)
				}
			}
		case []service.ReadAccessResult:
			for _, r := range v {
				if r.Object.Type != bacnet.ObjectTypeDevice {
					continue
				}
				id := get(m)
				for _, p := range r.Properties {
					if p.Err == nil && p.Property.ArrayIndex == nil && id.set(p.Property.Identifier, p.Value) {
						id.Frames = appendFrame(id.Frames, m.Frame)
					}
				}
			}
		}
	}
	for k, id := range out {
		if len(id.Frames) == 0 {
			delete(out, k)
		}
	}
	return out
}

// set records one Device property the first time it is seen.
func (id *Identity) set(p bacnet.PropertyIdentifier, v bacnet.ApplicationValue) bool {
	str := func(dst *string) bool {
		if *dst != "" || v.Kind != bacnet.ValueCharacterString {
			return false
		}
		*dst = v.Character.Value
		return true
	}
	switch p {
	case bacnet.PropertyVendorName:
		return str(&id.VendorName)
	case bacnet.PropertyModelName:
		return str(&id.ModelName)
	case bacnet.PropertyFirmwareRevision:
		return str(&id.Firmware)
	case bacnet.PropertyApplicationSoftwareVersion:
		return str(&id.Application)
	case bacnet.PropertyProtocolRevision:
		if id.ProtocolRevision == 0 && v.Kind == bacnet.ValueUnsigned {
			id.ProtocolRevision = v.Unsigned
			return true
		}
	default: // not an identity property
	}
	return false
}

func appendFrame(fs []int, f int) []int {
	for _, x := range fs {
		if x == f {
			return fs
		}
	}
	fs = append(fs, f)
	sort.Ints(fs)
	return fs
}
