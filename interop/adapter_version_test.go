//go:build interop

// SPDX-License-Identifier: MIT

package interop

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// requireAdapterVersion skips the test when image is a published
// bacnet-interop adapter release older than min ("0.10.0"). Scenarios that
// need an adapter capability added after the pinned release use it, so the
// pinned lane stays green until the pin moves, while images built from a
// bacnet-interop working tree (whose ADAPTER_VERSION is a commit, not a
// release) always run them.
//
// An image given as "none" skips as well. Both skip also in required mode: an
// older pin is not a missing dependency.
func requireAdapterVersion(t *testing.T, image, min string) {
	t.Helper()
	// "none" is what `make interop-pinned` and CI pass for an image the
	// pinned bacnet-interop release does not have yet.
	if image == "none" {
		t.Skipf("no image for this peer in the pinned bacnet-interop release; this scenario needs %s or newer", min)
	}
	out, err := exec.Command("docker", "image", "inspect", "--format",
		"{{range .Config.Env}}{{println .}}{{end}}", image).Output()
	if err != nil {
		return // not pulled or no Docker: startPeer reports that
	}
	for _, line := range strings.Split(string(out), "\n") {
		v, ok := strings.CutPrefix(strings.TrimSpace(line), "ADAPTER_VERSION=")
		if !ok {
			continue
		}
		have, okHave := parseRelease(v)
		want, okWant := parseRelease(min)
		if okHave && okWant && releaseLess(have, want) {
			t.Skipf("adapter image %s is release %s; this scenario needs bacnet-interop %s or newer", image, v, min)
		}
		return
	}
}

// parseRelease parses "v1.2.3" or "1.2.3". Anything else (a commit, "dev")
// is not a release.
func parseRelease(s string) ([3]int, bool) {
	var out [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

func releaseLess(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

func TestParseRelease(t *testing.T) {
	for in, want := range map[string]bool{"v0.9.0": true, "0.10.0": true, "dev": false, "7547ad3": false, "v1.2": false} {
		if _, ok := parseRelease(in); ok != want {
			t.Errorf("parseRelease(%q) ok=%v, want %v", in, ok, want)
		}
	}
	a, _ := parseRelease("v0.9.0")
	b, _ := parseRelease("0.10.0")
	if !releaseLess(a, b) || releaseLess(b, a) || releaseLess(a, a) {
		t.Fatal("0.9.0 must sort before 0.10.0")
	}
}
