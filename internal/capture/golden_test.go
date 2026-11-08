// SPDX-License-Identifier: MIT

package capture

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/otfabric/go-bacnet"
	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/bvlc"
)

func loadGolden(t *testing.T) []*Golden {
	t.Helper()
	gs, err := LoadGolden()
	if err != nil {
		t.Fatal(err)
	}
	if len(gs) == 0 {
		t.Fatal("no golden cases")
	}
	return gs
}

// first returns the first of Wireshark's aggregated field values.
func first(s string) string {
	v, _, _ := strings.Cut(s, ",")
	return v
}

func wsUint(s string) (uint64, bool) {
	s = first(s)
	if s == "" {
		return 0, false
	}
	base := 10
	if strings.HasPrefix(s, "0x") {
		s, base = s[2:], 16
	}
	v, err := strconv.ParseUint(s, base, 32)
	return v, err == nil
}

// mismatches compares go-bacnet's decode of one frame with Wireshark's.
func mismatches(m Message, w Wireshark) []string {
	var out []string
	check := func(name, ws string, got uint64, present bool) {
		want, ok := wsUint(ws)
		switch {
		case ok != present:
			out = append(out, fmt.Sprintf("%s: Wireshark %q, go-bacnet present=%v", name, ws, present))
		case ok && want != got:
			out = append(out, fmt.Sprintf("%s: Wireshark %d, go-bacnet %d", name, want, got))
		}
	}
	if m.Err != nil && m.ErrLayer == LayerBVLC {
		return out
	}
	check("bvlc function", w.BVLCFunction, uint64(m.BVLC.Function), true)
	if m.NPDU == nil {
		return out
	}
	n := m.NPDU
	check("npdu control", w.Control, uint64(n.Control), true)
	// Wireshark also names the networks a network message lists "dnet":
	// compare only the NPDU's own DNET and SNET.
	if n.Control&0x20 != 0 {
		dnet := uint64(n.Destination.Network())
		if n.Destination.Scope() == bacnet.AddressGlobalBroadcast {
			dnet = 0xFFFF
		}
		check("dnet", w.DNET, dnet, true)
	}
	if n.Control&0x08 != 0 {
		check("snet", w.SNET, uint64(n.Source.Network()), true)
	}
	if n.NetworkMessage {
		check("network message", w.NetMessage, uint64(n.NetMsgType), true)
		return out
	}
	if m.APDU == nil {
		return out
	}
	p := m.APDU
	check("apdu type", w.APDUType, uint64(p.Type)>>4, true)
	switch p.Type {
	case apdu.TypeConfirmedRequest:
		r := p.ConfirmedRequest
		check("service", w.Confirmed, uint64(r.ServiceChoice), true)
		check("invoke id", w.InvokeID, uint64(r.InvokeID), true)
		if r.SegmentedMessage {
			check("sequence", w.Sequence, uint64(r.SequenceNumber), true)
			check("window", w.Window, uint64(r.ProposedWindowSize), true)
		}
	case apdu.TypeUnconfirmedRequest:
		check("service", w.Unconfirmed, uint64(p.UnconfirmedRequest.ServiceChoice), true)
	case apdu.TypeSimpleACK:
		check("service", w.Confirmed, uint64(p.SimpleACK.ServiceChoice), true)
		check("invoke id", w.InvokeID, uint64(p.SimpleACK.InvokeID), true)
	case apdu.TypeComplexACK:
		r := p.ComplexACK
		check("service", w.Confirmed, uint64(r.ServiceChoice), true)
		check("invoke id", w.InvokeID, uint64(r.InvokeID), true)
		if r.SegmentedMessage {
			check("sequence", w.Sequence, uint64(r.SequenceNumber), true)
			check("window", w.Window, uint64(r.ProposedWindowSize), true)
		}
	case apdu.TypeSegmentACK:
		check("invoke id", w.InvokeID, uint64(p.SegmentACK.InvokeID), true)
		check("sequence", w.Sequence, uint64(p.SegmentACK.SequenceNumber), true)
		check("window", w.Window, uint64(p.SegmentACK.ActualWindowSize), true)
	case apdu.TypeError:
		check("service", w.Confirmed, uint64(p.Error.ServiceChoice), true)
		check("invoke id", w.InvokeID, uint64(p.Error.InvokeID), true)
	case apdu.TypeReject:
		check("invoke id", w.InvokeID, uint64(p.Reject.InvokeID), true)
	case apdu.TypeAbort:
		check("invoke id", w.InvokeID, uint64(p.Abort.InvokeID), true)
	}
	return out
}

func differenceFrames(ds []Difference) map[int]bool {
	out := map[int]bool{}
	for _, d := range ds {
		for _, f := range d.Frames {
			out[f] = true
		}
	}
	return out
}

// Every excerpted datagram decodes as Wireshark decodes it, layer by layer,
// and go-bacnet refuses exactly the datagrams Wireshark marks malformed,
// except for the differences each case lists and classifies.
func TestGoldenMatchesWireshark(t *testing.T) {
	for _, g := range loadGolden(t) {
		t.Run(g.ID, func(t *testing.T) {
			msgs, err := g.Messages()
			if err != nil {
				t.Fatal(err)
			}
			listed := differenceFrames(g.Differences)
			for i, m := range msgs {
				w := g.Frames[i].Wireshark
				diff := mismatches(m, w)
				if (m.Err != nil) != w.Malformed {
					diff = append(diff, fmt.Sprintf("go-bacnet error %v, Wireshark malformed %v", m.Err, w.Malformed))
				}
				switch {
				case len(diff) > 0 && !listed[m.Frame]:
					t.Errorf("frame %d: %s", m.Frame, strings.Join(diff, "; "))
				case len(diff) == 0 && listed[m.Frame]:
					t.Errorf("frame %d is listed as a difference but agrees with Wireshark", m.Frame)
				}
			}
			for _, d := range g.Differences {
				if d.Classification == "" || d.Detail == "" {
					t.Errorf("difference %v is not classified", d.Frames)
				}
			}
		})
	}
}

// Datagrams that exposed a go-bacnet defect decode completely now.
func TestGoldenRegressions(t *testing.T) {
	n := 0
	for _, g := range loadGolden(t) {
		msgs, err := g.Messages()
		if err != nil {
			t.Fatal(err)
		}
		byFrame := map[int]Message{}
		for _, m := range msgs {
			byFrame[m.Frame] = m
		}
		for _, d := range g.Regressions {
			for _, f := range d.Frames {
				m, ok := byFrame[f]
				if !ok {
					t.Fatalf("%s: regression frame %d not excerpted", g.ID, f)
				}
				if m.Err != nil || m.Service == nil {
					t.Errorf("%s frame %d (%s): %v", g.ID, f, d.Detail, m.Err)
				}
				n++
			}
		}
	}
	if n == 0 {
		t.Fatal("no regression frames")
	}
}

// The devices a case names are identified from the excerpt alone, as the
// generator identified them from the whole capture.
func TestGoldenDevices(t *testing.T) {
	for _, g := range loadGolden(t) {
		msgs, err := g.Messages()
		if err != nil {
			t.Fatal(err)
		}
		ids := Identities(msgs)
		for _, d := range g.Devices {
			got := ids[d.Station]
			if got == nil {
				t.Errorf("%s: %s not identified", g.ID, d.Station)
				continue
			}
			// JSON holds text as UTF-8: a device's string in another
			// encoding (PolarSoft's Latin-1 "®") is compared as JSON
			// stored it.
			for _, s := range []*string{&got.VendorName, &got.ModelName, &got.Firmware, &got.Application} {
				*s = strings.ToValidUTF8(*s, "\uFFFD")
			}
			if !reflect.DeepEqual(*got, d.Identity) {
				t.Errorf("%s: %s\n got %+v\nwant %+v", g.ID, d.Station, *got, d.Identity)
			}
		}
		if (g.Class == "A") != slices.ContainsFunc(g.Devices, func(d Device) bool { return d.ModelName != "" }) {
			t.Errorf("%s: class %s does not match its device evidence", g.ID, g.Class)
		}
	}
}

// Every answer in an excerpt has its request: transactions are excerpted
// whole, so invoke-ID correlation holds within each case.
func TestGoldenTransactions(t *testing.T) {
	total := 0
	for _, g := range loadGolden(t) {
		msgs, err := g.Messages()
		if err != nil {
			t.Fatal(err)
		}
		txs, orphans := Transactions(msgs)
		// An answer is an orphan only when go-bacnet refused its request,
		// and that request must be a listed difference.
		refused := map[string]bool{}
		listed := differenceFrames(g.Differences)
		for _, m := range msgs {
			if m.Err != nil && listed[m.Frame] {
				refused[m.Src.String()+">"+m.Dst.String()] = true
			}
		}
		for _, o := range orphans {
			if !refused[o.Dst.String()+">"+o.Src.String()] {
				t.Errorf("%s frame %d: answer without its request", g.ID, o.Frame)
			}
		}
		for _, tx := range txs {
			req, resp := tx.Final()
			if resp.APDU != nil && resp.APDU.Type != apdu.TypeAbort && resp.APDU.Type != apdu.TypeReject {
				if got := answerService(resp); got != tx.Service {
					t.Errorf("%s frame %d: answer to service %d is for service %d", g.ID, resp.Frame, tx.Service, got)
				}
			}
			_ = req
		}
		total += len(txs)
	}
	if total == 0 {
		t.Fatal("no transactions")
	}
}

func answerService(m Message) uint8 {
	switch p := m.APDU; p.Type {
	case apdu.TypeSimpleACK:
		return p.SimpleACK.ServiceChoice
	case apdu.TypeComplexACK:
		return p.ComplexACK.ServiceChoice
	case apdu.TypeError:
		return p.Error.ServiceChoice
	default:
		return 0
	}
}

// Whatever go-bacnet decodes it encodes again to the recorded bytes, layer
// by layer, except for the differences each case classifies.
func TestGoldenReencode(t *testing.T) {
	encoded := 0
	for _, g := range loadGolden(t) {
		t.Run(g.ID, func(t *testing.T) {
			msgs, err := g.Messages()
			if err != nil {
				t.Fatal(err)
			}
			listed := differenceFrames(g.Differences)
			for _, m := range msgs {
				if m.Err != nil {
					continue
				}
				layer, ok, err := Reencode(m)
				if ok && err == nil {
					encoded++
				}
				if layer != "" && !listed[m.Frame] {
					t.Errorf("frame %d: %s: %v", m.Frame, layer, err)
				}
			}
		})
	}
	if encoded == 0 {
		t.Fatal("nothing re-encoded")
	}
}

func TestGoldenSourcesInManifest(t *testing.T) {
	man, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]ManifestFile{}
	for _, f := range man.Files {
		byName[f.Name] = f
	}
	for _, g := range loadGolden(t) {
		f, ok := byName[g.Source.Name]
		if !ok || f.SHA256 != g.Source.SHA256 || !slices.Contains(f.Golden, g.ID) {
			t.Errorf("%s: source %s not in the manifest as excerpted", g.ID, g.Source.Name)
		}
		if g.Class == "" || g.ClassBasis == "" || len(g.Frames) == 0 {
			t.Errorf("%s: class, basis or frames missing", g.ID)
		}
	}
}

func TestSeeds(t *testing.T) {
	seeds := Seeds()
	if len(seeds) == 0 {
		t.Fatal("no seeds")
	}
	for _, s := range seeds {
		if len(s) < 4 || bvlc.Function(s[1]) > bvlc.FunctionOriginalBroadcastNPDU+0x10 {
			t.Fatalf("seed % x is not a BVLL message", s)
		}
	}
	if _, err := (Frame{Hex: "zz"}).Datagram(); err == nil {
		t.Error("bad hex accepted")
	}
	if _, err := (Frame{Hex: "81", Src: "x"}).Datagram(); err == nil {
		t.Error("bad source accepted")
	}
	if _, err := (Frame{Hex: "81", Src: "1.2.3.4:5", Dst: "x"}).Datagram(); !errors.Is(err, err) || err == nil {
		t.Error("bad destination accepted")
	}
}

// caseReport is one golden case in report/decode.json.
type caseReport struct {
	ID          string         `json:"id"`
	Class       string         `json:"class"`
	Devices     []string       `json:"devices,omitempty"`
	Frames      int            `json:"frames"`
	Decoded     int            `json:"decoded"`
	Refused     int            `json:"refused"`
	Wireshark   int            `json:"wireshark_agrees"`
	Reencoded   int            `json:"reencoded_byte_exact"`
	Services    map[string]int `json:"services"`
	Differences int            `json:"classified_differences"`
	Regressions int            `json:"regression_frames"`
}

// TestGoldenReport states per golden case what the decode suites check
// (testdata/captures/report/decode.json).
func TestGoldenReport(t *testing.T) {
	var out []caseReport
	for _, g := range loadGolden(t) {
		msgs, err := g.Messages()
		if err != nil {
			t.Fatal(err)
		}
		r := caseReport{ID: g.ID, Class: g.Class, Frames: len(g.Frames), Services: map[string]int{}, Differences: len(g.Differences)}
		for _, d := range g.Devices {
			r.Devices = append(r.Devices, strings.ToValidUTF8(strings.Join(strings.Fields(strings.Trim(d.ModelName, "\x00 ")+" "+d.Firmware), " "), "�"))
		}
		for _, d := range g.Regressions {
			r.Regressions += len(d.Frames)
		}
		for i, m := range msgs {
			if m.Err != nil {
				r.Refused++
			} else {
				r.Decoded++
				if m.Service != nil {
					r.Services[strings.TrimPrefix(fmt.Sprintf("%T", m.Service), "service.")]++
				}
				if layer, ok, err := Reencode(m); ok && err == nil && layer == "" {
					r.Reencoded++
				}
			}
			if len(mismatches(m, g.Frames[i].Wireshark)) == 0 && (m.Err != nil) == g.Frames[i].Wireshark.Malformed {
				r.Wireshark++
			}
		}
		out = append(out, r)
	}
	if err := Report("decode", out); err != nil {
		t.Error(err)
	}
}
