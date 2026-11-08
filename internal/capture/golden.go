// SPDX-License-Identifier: MIT

package capture

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"time"
)

// Golden is one golden case: datagrams excerpted from one recorded capture,
// with the decode of an independent dissector (Wireshark) for every one, the
// devices the capture identifies, and the known differences between
// go-bacnet and the recording. It sits next to the capture it excerpts.
type Golden struct {
	ID          string   `json:"id"`
	Summary     string   `json:"summary"`
	Source      Source   `json:"source"`
	Class       string   `json:"class"` // evidence class A, B, C or D (README.md)
	ClassBasis  string   `json:"class_basis"`
	Devices     []Device `json:"devices,omitempty"`
	Limitations []string `json:"limitations,omitempty"`
	// Differences are datagrams go-bacnet decodes differently from
	// Wireshark, or a recorded behaviour go-bacnet does not reproduce, each
	// with its classification.
	Differences []Difference `json:"differences,omitempty"`
	// Regressions are datagrams that exposed a go-bacnet defect, since
	// fixed: they must decode.
	Regressions []Difference `json:"regressions,omitempty"`
	Frames      []Frame      `json:"frames"`
}

// Source identifies the recorded capture a golden case excerpts.
type Source struct {
	Name     string `json:"name"` // the original file name
	File     string `json:"file"` // the committed file, under testdata/captures
	URL      string `json:"url"`
	SHA256   string `json:"sha256"`
	Size     int64  `json:"size"`
	Fetched  string `json:"fetched"` // acquisition date (YYYY-MM-DD)
	Terms    string `json:"terms"`
	Frames   int    `json:"frames"`    // frames in the original file
	Datagram int    `json:"datagrams"` // BACnet/IP datagrams in it
}

// Device is a device the capture identifies, with the frames that do.
type Device struct {
	Identity
	Role string `json:"role"` // what it does in the capture
}

// Difference is one classified mismatch.
type Difference struct {
	Frames         []int  `json:"frames"`
	Classification string `json:"classification"`
	Detail         string `json:"detail"`
}

// Frame is one excerpted BACnet/IP datagram.
type Frame struct {
	Frame     int       `json:"frame"`
	Time      time.Time `json:"time"`
	Src       string    `json:"src"`
	Dst       string    `json:"dst"`
	Hex       string    `json:"hex"` // UDP payload: BVLC, NPDU, APDU
	Wireshark Wireshark `json:"wireshark"`
}

// Wireshark is Wireshark's decode of a frame (tshark -T fields, PRP off):
// the raw field strings, empty when Wireshark shows no such field.
type Wireshark struct {
	BVLCFunction string `json:"bvlc_function"`
	Control      string `json:"npdu_control,omitempty"`
	DNET         string `json:"dnet,omitempty"`
	SNET         string `json:"snet,omitempty"`
	NetMessage   string `json:"network_message,omitempty"`
	APDUType     string `json:"apdu_type,omitempty"`
	Confirmed    string `json:"confirmed_service,omitempty"`
	Unconfirmed  string `json:"unconfirmed_service,omitempty"`
	InvokeID     string `json:"invoke_id,omitempty"`
	Sequence     string `json:"sequence_number,omitempty"`
	Window       string `json:"window_size,omitempty"`
	Malformed    bool   `json:"malformed,omitempty"`
}

// Datagram returns the frame as a capture datagram.
func (f Frame) Datagram() (Datagram, error) {
	b, err := hex.DecodeString(f.Hex)
	if err != nil {
		return Datagram{}, fmt.Errorf("frame %d: %w", f.Frame, err)
	}
	src, err := netip.ParseAddrPort(f.Src)
	if err != nil {
		return Datagram{}, fmt.Errorf("frame %d: %w", f.Frame, err)
	}
	dst, err := netip.ParseAddrPort(f.Dst)
	if err != nil {
		return Datagram{}, fmt.Errorf("frame %d: %w", f.Frame, err)
	}
	return Datagram{Frame: f.Frame, Time: f.Time, Src: src, Dst: dst, Data: b}, nil
}

// Messages decodes every frame of the case in order.
func (g *Golden) Messages() ([]Message, error) {
	d := NewDecoder()
	out := make([]Message, 0, len(g.Frames))
	for _, f := range g.Frames {
		dg, err := f.Datagram()
		if err != nil {
			return nil, err
		}
		out = append(out, d.Decode(dg))
	}
	return out, nil
}

// Dir returns testdata/captures of this module.
func Dir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "testdata", "captures")
}

// LoadGolden reads every golden case (<source>/<capture>.json, next to the
// capture it excerpts), ordered by ID.
func LoadGolden() ([]*Golden, error) {
	root, err := os.OpenRoot(Dir())
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()
	paths, err := fs.Glob(root.FS(), "*/*.json")
	if err != nil {
		return nil, err
	}
	var out []*Golden
	for _, path := range paths {
		if filepath.Dir(path) == "report" {
			continue
		}
		b, err := root.ReadFile(path)
		if err != nil {
			return nil, err
		}
		g := new(Golden)
		if err := json.Unmarshal(b, g); err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// Seeds returns the UDP payloads of every golden frame, for fuzz targets.
// A missing corpus yields no seeds rather than failing the fuzz target.
func Seeds() [][]byte {
	gs, err := LoadGolden()
	if err != nil {
		return nil
	}
	var out [][]byte
	for _, g := range gs {
		for _, f := range g.Frames {
			if b, err := hex.DecodeString(f.Hex); err == nil {
				out = append(out, b)
			}
		}
	}
	return out
}

// LayerSeeds returns, for fuzz targets, the recorded octets at one layer of
// every golden datagram that decodes that far: the whole datagram
// (LayerBVLC), the NPDU (LayerNPDU), the APDU (LayerAPDU) or the service
// payload (LayerService) of the messages keep selects (nil keeps all).
func LayerSeeds(layer Layer, keep func(Message) bool) [][]byte {
	gs, err := LoadGolden()
	if err != nil {
		return nil
	}
	var out [][]byte
	for _, g := range gs {
		msgs, err := g.Messages()
		if err != nil {
			continue
		}
		for _, m := range msgs {
			if keep != nil && !keep(m) {
				continue
			}
			switch {
			case layer == LayerBVLC:
				out = append(out, m.Data)
			case layer == LayerNPDU && m.NPDU != nil:
				out = append(out, m.BVLC.Payload)
			case layer == LayerAPDU && m.APDU != nil:
				out = append(out, m.NPDU.APDU)
			case layer == LayerService && m.Payload != nil:
				out = append(out, m.Payload)
			}
		}
	}
	return out
}

// ServiceIs selects messages whose decoded service has the type of v.
func ServiceIs(v any) func(Message) bool {
	want := fmt.Sprintf("%T", v)
	return func(m Message) bool { return m.Service != nil && fmt.Sprintf("%T", m.Service) == want }
}
