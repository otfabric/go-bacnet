// SPDX-License-Identifier: MIT

package capture

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/otfabric/go-bacnet/apdu"
	"github.com/otfabric/go-bacnet/service"
)

// TestGenerate rebuilds manifest.json, SHA256SUMS and golden/*.json from
// the originals in the capture cache (make captures-fetch) with tshark.
// It runs only with BACNET_CAPTURE_GENERATE=1; the files it writes are
// committed and checked by the other tests.
func TestGenerate(t *testing.T) {
	if os.Getenv("BACNET_CAPTURE_GENERATE") != "1" {
		t.Skip("set BACNET_CAPTURE_GENERATE=1 (needs the capture cache and tshark)")
	}
	tshark, err := exec.LookPath("tshark")
	if err != nil {
		t.Fatal(err)
	}
	ver, _ := exec.Command(tshark, "-v").Output()
	cache := CacheDir()
	man := &Manifest{Generated: fetchedDate, Wireshark: strings.TrimSpace(strings.SplitN(string(ver), "\n", 2)[0]), Sources: sources}
	golden := map[string][]string{}
	for _, c := range cases {
		golden[c.File] = append(golden[c.File], c.ID)
	}
	ents, err := os.ReadDir(filepath.Join(cache, "kargs"))
	if err != nil {
		t.Fatal(err)
	}
	var sums []string
	parsed := map[string]*parsedFile{}
	for _, e := range ents {
		name := e.Name()
		path := filepath.Join(cache, "kargs", name)
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(raw)
		mf := ManifestFile{Source: "kargs", Name: name, URL: kargsURL(name), SHA256: hex.EncodeToString(sum[:]), Size: int64(len(raw)), Golden: golden[name]}
		sums = append(sums, fmt.Sprintf("%s  kargs/%s", mf.SHA256, name))
		f, err := Read(raw)
		if err != nil {
			mf.Class, mf.ClassBasis = "-", "not a pcap or pcapng file: "+err.Error()
			man.Files = append(man.Files, mf)
			continue
		}
		mf.Readable = true
		st, msgs := Analyze(f)
		ws, err := runTshark(tshark, path, ports(f))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		st.WiresharkBVLC = len(ws)
		for _, w := range ws {
			if w.Malformed {
				st.WiresharkMalformed++
			}
		}
		mf.Stats = st
		if len(f.NonIP) > 0 {
			mf.NonIP = f.NonIP
		}
		ids := Identities(msgs)
		mf.Devices = deviceStrings(ids)
		mf.Class, mf.ClassBasis = classify(name, f, msgs, ids)
		man.Files = append(man.Files, mf)
		parsed[name] = &parsedFile{file: f, msgs: msgs, ws: ws, ids: ids, mf: mf}
	}
	sort.Strings(sums)
	writeFile(t, filepath.Join(Dir(), "SHA256SUMS"), []byte(strings.Join(sums, "\n")+"\n"))
	writeJSON(t, filepath.Join(Dir(), "manifest.json"), man)

	if err := os.MkdirAll(filepath.Join(Dir(), "golden"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		p := parsed[c.File]
		if p == nil {
			t.Fatalf("%s: %s not in the cache", c.ID, c.File)
		}
		writeJSON(t, filepath.Join(Dir(), "golden", c.ID+".json"), c.build(t, p))
	}
}

type parsedFile struct {
	file *File
	msgs []Message
	ws   map[int]Wireshark
	ids  map[string]*Identity
	mf   ManifestFile
}

// caseSpec selects the datagrams of one golden case.
type caseSpec struct {
	ID, File, Class, ClassBasis, Summary string
	// Devices are the stations whose identity the case records, with
	// their role.
	Devices map[string]string
	// PerKind transactions are kept per responder, service, answer type
	// and segmentation; as many unconfirmed requests per sender and
	// service, network messages per type and BVLL messages per function.
	PerKind int
	// Frames are kept in addition (and their transactions).
	Frames      []int
	Limitations []string
	Differences []Difference
	Regressions []Difference
	// Regression selects datagrams that exposed a defect since fixed
	// (RegressionDetail says which); they are kept and listed.
	Regression       func(Message) bool
	RegressionDetail string
}

func (c caseSpec) build(t *testing.T, p *parsedFile) *Golden {
	t.Helper()
	keep := map[int]bool{}
	txs, _ := Transactions(p.msgs)
	byFrame := map[int]int{}
	for i, tx := range txs {
		for _, ms := range [][]Message{tx.Request, tx.Response, tx.SegmentACKs} {
			for _, m := range ms {
				byFrame[m.Frame] = i
			}
		}
	}
	addTx := func(tx Transaction) {
		for _, ms := range [][]Message{tx.Request, tx.Response, tx.SegmentACKs} {
			for _, m := range ms {
				keep[m.Frame] = true
			}
		}
	}
	seen := map[string]int{}
	quota := func(k string) bool {
		if seen[k] >= c.PerKind {
			return false
		}
		seen[k]++
		return true
	}
	for _, tx := range txs {
		req, resp := tx.Final()
		kind := "none"
		if resp.APDU != nil {
			kind = fmt.Sprintf("%#x", uint8(resp.APDU.Type))
		}
		if quota(fmt.Sprintf("tx|%s|%d|%s|%v", Endpoint(req, false), tx.Service, kind, len(tx.Request) > 1 || len(tx.Response) > 1)) {
			addTx(tx)
		}
	}
	for _, m := range p.msgs {
		var k string
		switch {
		case m.Err != nil:
			k = fmt.Sprintf("err|%s|%v", m.ErrLayer, m.Err)
		case m.NPDU == nil:
			k = fmt.Sprintf("bvll|%s|%d", m.Src, m.BVLC.Function)
		case m.NPDU.NetworkMessage:
			k = fmt.Sprintf("net|%s|%d", m.Src, m.NPDU.NetMsgType)
		case m.APDU.Type == apdu.TypeUnconfirmedRequest:
			k = fmt.Sprintf("unc|%s|%d", Endpoint(m, true), m.APDU.UnconfirmedRequest.ServiceChoice)
		default:
			continue
		}
		if quota(k) {
			keep[m.Frame] = true
		}
	}
	devs := make([]Device, 0, len(c.Devices))
	for st, role := range c.Devices {
		id := p.ids[st]
		if id == nil {
			t.Fatalf("%s: no identity for %s", c.ID, st)
		}
		devs = append(devs, Device{Identity: *id, Role: role})
		for _, f := range id.Frames {
			keep[f] = true
			if i, ok := byFrame[f]; ok {
				addTx(txs[i])
			}
		}
	}
	sort.Slice(devs, func(i, j int) bool { return devs[i].Station < devs[j].Station })
	for _, f := range c.Frames {
		keep[f] = true
		if i, ok := byFrame[f]; ok {
			addTx(txs[i])
		}
	}
	for _, d := range c.Differences {
		for _, f := range d.Frames {
			keep[f] = true
		}
	}
	class, basis := c.Class, c.ClassBasis
	if class == "" {
		class, basis = p.mf.Class, p.mf.ClassBasis
	}
	if c.Regression != nil {
		var fs []int
		for _, m := range p.msgs {
			if c.Regression(m) {
				fs = append(fs, m.Frame)
			}
		}
		if len(fs) == 0 {
			t.Fatalf("%s: no regression frames", c.ID)
		}
		c.Regressions = append(c.Regressions, Difference{Frames: fs, Classification: "codec bug (fixed)", Detail: c.RegressionDetail})
	}
	for _, d := range c.Regressions {
		for _, f := range d.Frames {
			keep[f] = true
			if i, ok := byFrame[f]; ok {
				addTx(txs[i])
			}
		}
	}
	g := &Golden{
		ID: c.ID, Summary: c.Summary, Class: class, ClassBasis: basis, Regressions: c.Regressions,
		Source: Source{
			Name: c.File, URL: p.mf.URL, SHA256: p.mf.SHA256, Size: p.mf.Size, Fetched: fetchedDate,
			Terms: kargsTerms, Frames: p.file.Frames, Datagram: len(p.file.IP),
		},
		Devices: devs, Limitations: c.Limitations, Differences: c.Differences,
	}
	for _, dg := range p.file.IP {
		if !keep[dg.Frame] {
			continue
		}
		w, ok := p.ws[dg.Frame]
		if !ok {
			t.Fatalf("%s: frame %d not decoded by tshark", c.ID, dg.Frame)
		}
		g.Frames = append(g.Frames, Frame{Frame: dg.Frame, Time: dg.Time, Src: dg.Src.String(), Dst: dg.Dst.String(), Hex: hex.EncodeToString(dg.Data), Wireshark: w})
	}
	return g
}

// ports are the UDP ports of the BACnet/IP datagrams besides 47808, for
// tshark's decode-as.
func ports(f *File) []uint16 {
	set := map[uint16]bool{}
	for _, dg := range f.IP {
		for _, p := range []uint16{dg.Src.Port(), dg.Dst.Port()} {
			if p != 47808 {
				set[p] = true
			}
		}
	}
	out := make([]uint16, 0, len(set))
	for p := range set {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

var tsharkFields = []string{
	"frame.number", "bvlc.function", "bacnet.control", "bacnet.dnet", "bacnet.snet", "bacnet.mesgtyp",
	"bacapp.type", "bacapp.confirmed_service", "bacapp.unconfirmed_service", "bacapp.invoke_id",
	"bacapp.sequence_number", "bacapp.window_size", "_ws.malformed", "_ws.expert.severity",
}

// runTshark returns Wireshark's decode of every BVLC frame. The PRP
// dissector is off: it claims the Ethernet padding of short frames and
// marks them malformed.
func runTshark(tshark, path string, extra []uint16) (map[int]Wireshark, error) {
	args := []string{"--disable-protocol", "prp", "-r", path, "-Y", "bvlc", "-T", "fields", "-E", "separator=|", "-E", "occurrence=a", "-E", "aggregator=,"}
	for _, p := range extra {
		args = append(args, "-d", fmt.Sprintf("udp.port==%d,bvlc", p))
	}
	for _, f := range tsharkFields {
		args = append(args, "-e", f)
	}
	var stderr bytes.Buffer
	cmd := exec.Command(tshark, args...)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("tshark: %w: %s", err, stderr.String())
	}
	res := map[int]Wireshark{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		p := strings.Split(sc.Text(), "|")
		if len(p) != len(tsharkFields) {
			return nil, fmt.Errorf("tshark line %q", sc.Text())
		}
		n, err := strconv.Atoi(p[0])
		if err != nil {
			return nil, err
		}
		res[n] = Wireshark{
			BVLCFunction: p[1], Control: p[2], DNET: p[3], SNET: p[4], NetMessage: p[5],
			APDUType: p[6], Confirmed: p[7], Unconfirmed: p[8], InvokeID: p[9],
			Sequence: p[10], Window: p[11],
			Malformed: p[12] != "" || strings.Contains(p[13], "8388608"),
		}
	}
	return res, sc.Err()
}

var (
	stackModel     = regexp.MustCompile(`^(GNU|Simple BACnet|BACnet Stack)`)
	syntheticFiles = map[string]string{
		"bacapp-malform.cap":                 "deliberately malformed test traffic (file name, malformed BVLC lengths)",
		"mstp_mix_basrt_V124_bad.cap":        "corrupted traffic recorded to test a router (file name)",
		"MSTP_Malformed_Packets.pcap":        "deliberately malformed test traffic (file name)",
		"readrange_malformed.cap":            "deliberately malformed test traffic (file name)",
		"ContextTagAbove14Sample_1.pcap":     "hand-made sample for a Wireshark dissector bug",
		"Ethereal-Misinterpreted-Packet.cap": "sample for a dissector bug",
	}
)

// classify assigns the evidence class of a capture from what its devices
// say about themselves (see testdata/captures/README.md).
func classify(name string, f *File, msgs []Message, ids map[string]*Identity) (string, string) {
	if why, ok := syntheticFiles[name]; ok {
		return "D", why
	}
	if len(f.IP) == 0 {
		return "C", "no BACnet/IP datagrams (other data link only)"
	}
	var real, stack []string
	for _, id := range ids {
		if id.ModelName == "" {
			continue
		}
		if id.VendorID == 260 || stackModel.MatchString(id.ModelName) {
			stack = append(stack, id.ModelName)
			continue
		}
		real = append(real, strings.TrimSpace(id.ModelName+" "+id.Firmware))
	}
	sort.Strings(real)
	if len(real) > 0 {
		return "A", "model and firmware from the devices' own Device objects: " + strings.Join(dedupe(real), "; ")
	}
	vendors := map[uint16]bool{}
	for _, m := range msgs {
		if m.Service == nil || m.APDU == nil || m.APDU.Type != apdu.TypeUnconfirmedRequest || m.APDU.UnconfirmedRequest.ServiceChoice != apdu.ServiceIAm {
			continue
		}
		if v := iamVendor(m); v != 0 && v != 260 {
			vendors[v] = true
		}
	}
	if len(vendors) > 0 {
		vs := make([]string, 0, len(vendors))
		for v := range vendors {
			vs = append(vs, strconv.Itoa(int(v)))
		}
		sort.Strings(vs)
		return "B", "vendor IDs in I-Am only (" + strings.Join(vs, ", ") + "); model not in the traffic"
	}
	if len(stack) > 0 {
		return "D", "BACnet Stack demo applications (vendor 260 or model " + strings.Join(dedupe(stack), ", ") + ")"
	}
	return "C", "device origin not in the traffic"
}

func dedupe(s []string) []string {
	sort.Strings(s)
	out := s[:0]
	for i, v := range s {
		if i == 0 || v != s[i-1] {
			out = append(out, v)
		}
	}
	return out
}

func deviceStrings(ids map[string]*Identity) []string {
	var out []string
	for _, id := range ids {
		if id.ModelName == "" && id.Firmware == "" {
			continue
		}
		// JSON holds UTF-8: PolarSoft's Latin-1 "®" in a string marked
		// UTF-8 becomes U+FFFD.
		d := fmt.Sprintf("%s %s %s (vendor %d, instance %d)", strings.Trim(id.VendorName, "\x00 "), strings.Trim(id.ModelName, "\x00 "), id.Firmware, id.VendorID, id.Instance)
		out = append(out, strings.ToValidUTF8(strings.Join(strings.Fields(d), " "), "\uFFFD"))
	}
	return dedupe(out)
}

func kargsURL(name string) string {
	return "https://kargs.net/captures/" + url.PathEscape(name)
}

func writeJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, append(b, '\n'))
}

func writeFile(t *testing.T, path string, b []byte) {
	t.Helper()
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func iamVendor(m Message) uint16 {
	if v, ok := m.Service.(service.IAm); ok {
		return v.VendorID
	}
	return 0
}
