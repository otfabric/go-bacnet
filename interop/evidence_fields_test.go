//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
)

// Decode evidence: an adapter runs the peer stack's own decoder on the
// request and reports the fields it extracted in the operation line. The
// harness keeps only the operation name, so these helpers read the lines
// themselves from the evidence file the harness mirrors them into.

// evidenceLines returns the operation lines recorded for peer so far.
func evidenceLines(peer *peerHandle) []string {
	if peer == nil || peer.evidencePath == "" {
		return nil
	}
	b, err := os.ReadFile(peer.evidencePath)
	if err != nil {
		return nil
	}
	lines := strings.Split(string(b), "\n")
	if n := len(lines); n > 0 && lines[n-1] == "" {
		lines = lines[:n-1]
	}
	return lines
}

// evidenceMark is the position after the last operation line recorded so
// far; awaitOperationLine looks only at lines after it.
func evidenceMark(peer *peerHandle) int { return len(evidenceLines(peer)) }

// awaitOperationLine waits for an operation line named op recorded after
// mark and returns it decoded.
func awaitOperationLine(t *testing.T, peer *peerHandle, op string, mark int, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		lines := evidenceLines(peer)
		for i := mark; i < len(lines); i++ {
			var ev map[string]any
			if json.Unmarshal([]byte(lines[i]), &ev) != nil {
				continue
			}
			if ev["event"] == "operation" && ev["operation"] == op {
				return ev
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for operation %q after line %d; evidence=%q", op, mark, lines)
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// operationFields returns the "fields" object of an operation line.
func operationFields(t *testing.T, ev map[string]any) map[string]any {
	t.Helper()
	f, ok := ev["fields"].(map[string]any)
	if !ok {
		t.Fatalf("operation line has no fields object: %v", ev)
	}
	return f
}

// wantField compares one decoded field with want after a JSON round trip,
// so numbers, nulls and nested objects compare by value.
func wantField(t *testing.T, fields map[string]any, key string, want any) {
	t.Helper()
	got, ok := fields[key]
	if !ok {
		t.Errorf("peer did not report %q; fields=%v", key, fields)
		return
	}
	g, _ := json.Marshal(got)
	w, _ := json.Marshal(want)
	var gn, wn any
	_ = json.Unmarshal(g, &gn)
	_ = json.Unmarshal(w, &wn)
	g, _ = json.Marshal(gn)
	w, _ = json.Marshal(wn)
	if string(g) != string(w) {
		t.Errorf("peer decoded %s = %s, want %s", key, g, w)
	}
}
