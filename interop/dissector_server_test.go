//go:build dissector

// SPDX-License-Identifier: MIT

package interop

import (
	"encoding/hex"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestDissectorAcceptsServerReplies runs the server end-to-end tests of
// ./e2e with BACNET_SERVER_FRAMES_DIR set, so every datagram the go-bacnet
// server sends (acks, errors, rejects, segments, I-Am, I-Have, COV and
// event notifications, BVLC results and Forwarded-NPDUs) is recorded, and
// passes each frame to Wireshark's tshark. A frame tshark calls malformed,
// flags with an expert warning or error, or does not dissect as BACnet
// fails the test.
//
// Like the client-request oracle it checks frames as Wireshark understands
// them, not that a device accepts them.
func TestDissectorAcceptsServerReplies(t *testing.T) {
	tshark, err := exec.LookPath("tshark")
	if err != nil {
		if os.Getenv("BACNET_DISSECTOR_REQUIRED") != "" {
			t.Fatal("tshark not found on PATH")
		}
		t.Skip("tshark not found on PATH")
	}
	dir := t.TempDir()
	cmd := exec.Command("go", "test", "-count=1", "-run", "^TestServer", "./e2e/")
	cmd.Dir = ".."
	cmd.Env = append(os.Environ(), "BACNET_SERVER_FRAMES_DIR="+dir)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("server e2e tests: %v\n%s", err, out)
	}
	files, err := filepath.Glob(filepath.Join(dir, "*.hex"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	var frames [][]byte
	var owner []string // test name of each frame
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := strings.TrimSuffix(filepath.Base(f), ".hex")
		for _, line := range strings.Fields(string(data)) {
			b, err := hex.DecodeString(line)
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			frames = append(frames, b)
			owner = append(owner, name)
		}
	}
	if len(files) < 10 || len(frames) < 100 {
		t.Fatalf("only %d frames from %d tests recorded", len(frames), len(files))
	}
	// The recorded ports are ephemeral loopback ports; the payload is what
	// is checked, so every frame is written from 47808 to 47808, the port
	// Wireshark dissects as BVLC.
	server := netip.MustParseAddrPort("10.0.0.1:47808")
	peer := netip.MustParseAddrPort("10.0.0.2:47808")
	pcap := filepath.Join(t.TempDir(), "server-replies.pcap")
	writePcap(t, pcap, server, peer, frames)
	if keep := os.Getenv("BACNET_DISSECTOR_SERVER_PCAP"); keep != "" {
		writePcap(t, keep, server, peer, frames)
		t.Logf("pcap kept at %s", keep)
	}
	out, err := exec.Command(tshark, "-r", pcap, "-T", "fields", "-E", "separator=|",
		"-e", "frame.number", "-e", "_ws.col.protocol", "-e", "_ws.col.info",
		"-e", "_ws.malformed", "-e", "_ws.expert.severity", "-e", "_ws.expert.message").Output()
	if err != nil {
		t.Fatalf("tshark: %v\n%s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) != len(frames) {
		t.Fatalf("tshark reported %d frames for %d written", len(lines), len(frames))
	}
	kinds := map[string]int{}
	for n, line := range lines {
		f := strings.SplitN(line, "|", 6)
		if len(f) != 6 {
			t.Errorf("%s: unexpected tshark line %q", owner[n], line)
			continue
		}
		proto, info, malformed, severity, expert := f[1], f[2], f[3], f[4], f[5]
		kinds[proto+" "+firstWords(info)]++
		if !strings.HasPrefix(proto, "BACnet") && proto != "BVLC" {
			t.Errorf("%s frame %d: dissected as %q, not BACnet\n  % x", owner[n], n+1, proto, frames[n])
		}
		if malformed != "" || strings.Contains(strings.ToLower(info), "malformed") {
			t.Errorf("%s frame %d: malformed: %s | %s\n  % x", owner[n], n+1, info, expert, frames[n])
		}
		// Expert severities: warn 0x600000, error 0x800000.
		for _, sev := range strings.Split(severity, ",") {
			if sev == "6291456" || sev == "8388608" {
				t.Errorf("%s frame %d: expert warning/error: %s | %s\n  % x", owner[n], n+1, info, expert, frames[n])
				break
			}
		}
	}
	names := make([]string, 0, len(kinds))
	for k := range kinds {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		t.Logf("%5d  %s", kinds[k], k)
	}
	t.Logf("%d frames from %d tests dissected", len(frames), len(files))
}

// firstWords is the service part of a tshark Info column: the text before
// the first digit or bracket, which carries invoke IDs and instances.
func firstWords(info string) string {
	if i := strings.IndexAny(info, "0123456789[("); i > 0 {
		info = info[:i]
	}
	return strings.TrimSpace(info)
}
