// SPDX-License-Identifier: MIT

package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// TestExtendedCorpus re-derives every manifest entry from the cached
// originals (make captures-extended): the SHA256, go-bacnet's decode and
// correlation counts and the device identities must equal the manifest.
// Without BACNET_CAPTURE_EXTENDED=1 it is skipped; with it every original
// must be in the cache.
func TestExtendedCorpus(t *testing.T) {
	if os.Getenv("BACNET_CAPTURE_EXTENDED") != "1" {
		t.Skip("set BACNET_CAPTURE_EXTENDED=1 after make captures-fetch")
	}
	man, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	datagrams := 0
	for _, mf := range man.Files {
		t.Run(mf.Name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join(CacheDir(), mf.Source, mf.Name))
			if err != nil {
				t.Fatal(err)
			}
			if sum := sha256.Sum256(raw); hex.EncodeToString(sum[:]) != mf.SHA256 {
				t.Fatalf("SHA256 %x, manifest %s", sum, mf.SHA256)
			}
			f, err := Read(raw)
			if (err == nil) != mf.Readable {
				t.Fatalf("readable %v, manifest %v (%v)", err == nil, mf.Readable, err)
			}
			if err != nil {
				return
			}
			st, msgs := Analyze(f)
			st.WiresharkBVLC, st.WiresharkMalformed = mf.Stats.WiresharkBVLC, mf.Stats.WiresharkMalformed
			if !reflect.DeepEqual(st, mf.Stats) {
				t.Errorf("stats\n got %+v\nwant %+v", st, mf.Stats)
			}
			if got := deviceStrings(Identities(msgs)); !reflect.DeepEqual(got, mf.Devices) {
				t.Errorf("devices\n got %q\nwant %q", got, mf.Devices)
			}
			datagrams += st.Datagrams
		})
	}
	t.Logf("%d files, %d BACnet/IP datagrams", len(man.Files), datagrams)
}
