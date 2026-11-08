// SPDX-License-Identifier: MIT

package capture

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"reflect"
	"testing"
)

// TestCorpus re-derives every manifest entry from the capture: the
// SHA256, go-bacnet's decode, re-encode and correlation counts and the
// device identities must equal the manifest. It reads every committed
// capture; with BACNET_CAPTURE_EXTENDED=1 (make captures-extended) also
// the ones over 50 MB, from the cache.
func TestCorpus(t *testing.T) {
	man, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	datagrams, files := 0, 0
	for _, mf := range man.Files {
		path := CorpusPath(mf)
		if path == "" {
			continue
		}
		files++
		t.Run(mf.File, func(t *testing.T) {
			raw, err := os.ReadFile(path)
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
	t.Logf("%d files, %d BACnet/IP datagrams", files, datagrams)
	if files < 200 {
		t.Fatalf("only %d captures read", files)
	}
}
