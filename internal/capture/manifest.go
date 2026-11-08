// SPDX-License-Identifier: MIT

package capture

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Manifest lists every capture examined, whether or not a golden case
// excerpts it: provenance, evidence class and what go-bacnet and Wireshark
// make of it. The extended corpus test re-derives the counts from the
// cached originals.
type Manifest struct {
	Generated string          `json:"generated"` // date the counts were derived
	Wireshark string          `json:"wireshark"` // tshark version used
	Sources   []ManifestGroup `json:"sources"`
	Files     []ManifestFile  `json:"files"`
}

// ManifestGroup is one place captures were taken from.
type ManifestGroup struct {
	ID       string `json:"id"`
	URL      string `json:"url"`
	Revision string `json:"revision"`
	Fetched  string `json:"fetched"`
	Terms    string `json:"terms"`
	Notes    string `json:"notes,omitempty"`
}

// ManifestFile is one capture file.
type ManifestFile struct {
	Source     string         `json:"source"` // ManifestGroup.ID
	Name       string         `json:"name"`   // the original file name
	File       string         `json:"file"`   // where it is: under testdata/captures if committed, else under the cache
	Committed  bool           `json:"committed"`
	URL        string         `json:"url"`
	SHA256     string         `json:"sha256"`
	Size       int64          `json:"size"`
	Class      string         `json:"class"`
	ClassBasis string         `json:"class_basis"`
	Readable   bool           `json:"readable"` // pcap or pcapng the reader parses
	Stats      Stats          `json:"stats"`
	NonIP      map[string]int `json:"non_ip,omitempty"`
	Devices    []string       `json:"devices,omitempty"` // "vendor model firmware" from the devices' own answers
	Golden     []string       `json:"golden,omitempty"`  // golden cases excerpting it
}

// Stats are the counts go-bacnet derives from one capture, with Wireshark's
// for comparison.
type Stats struct {
	Frames       int            `json:"frames"`
	Datagrams    int            `json:"datagrams"`
	Fragments    int            `json:"ip_fragments_merged,omitempty"`
	Decoded      int            `json:"decoded"`
	Rejected     map[string]int `json:"rejected,omitempty"` // by layer
	Transactions int            `json:"transactions"`
	Answered     int            `json:"answered"`
	Orphans      int            `json:"orphan_answers"`
	Segmented    int            `json:"segmented_transactions"`
	// Reencoded counts the decoded datagrams go-bacnet encodes again to
	// the recorded octets at every layer (those whose service has an
	// encoder); ReencodeDiffs the others, by first differing layer and
	// service type.
	Reencoded     int            `json:"reencoded_byte_exact"`
	ReencodeDiffs map[string]int `json:"reencode_differences,omitempty"`
	SegmentACKs   int            `json:"segment_acks"`
	// Wireshark: BVLC frames and those it marks malformed.
	WiresharkBVLC      int `json:"wireshark_bvlc"`
	WiresharkMalformed int `json:"wireshark_malformed"`
}

// CacheDir is where the original captures are cached: BACNET_CAPTURE_CACHE,
// else testdata/captures/cache (git-ignored; make captures-fetch fills it).
func CacheDir() string {
	if d := os.Getenv("BACNET_CAPTURE_CACHE"); d != "" {
		return d
	}
	return filepath.Join(Dir(), "cache")
}

// CorpusPath returns where a manifest file is: under testdata/captures if
// committed; else in the cache with BACNET_CAPTURE_EXTENDED=1; else "".
func CorpusPath(mf ManifestFile) string {
	switch {
	case !mf.Readable:
		return ""
	case mf.Committed:
		return filepath.Join(Dir(), filepath.FromSlash(mf.File))
	case os.Getenv("BACNET_CAPTURE_EXTENDED") == "1":
		return filepath.Join(CacheDir(), filepath.FromSlash(mf.File))
	}
	return ""
}

// LoadManifest reads testdata/captures/manifest.json.
func LoadManifest() (*Manifest, error) {
	b, err := os.ReadFile(filepath.Join(Dir(), "manifest.json"))
	if err != nil {
		return nil, err
	}
	m := new(Manifest)
	return m, json.Unmarshal(b, m)
}

// Analyze derives the go-bacnet side of Stats from a capture.
func Analyze(f *File) (Stats, []Message) {
	st := Stats{Frames: f.Frames, Datagrams: len(f.IP), Fragments: f.Fragments}
	d := NewDecoder()
	msgs := make([]Message, 0, len(f.IP))
	for _, dg := range f.IP {
		m := d.Decode(dg)
		msgs = append(msgs, m)
		if m.Err != nil {
			if st.Rejected == nil {
				st.Rejected = map[string]int{}
			}
			st.Rejected[string(m.ErrLayer)]++
			continue
		}
		st.Decoded++
		switch layer, ok, err := Reencode(m); {
		case layer != "":
			if st.ReencodeDiffs == nil {
				st.ReencodeDiffs = map[string]int{}
			}
			st.ReencodeDiffs[fmt.Sprintf("%s %T", layer, m.Service)]++
		case ok && err == nil:
			st.Reencoded++
		}
	}
	txs, orphans := Transactions(msgs)
	st.Transactions, st.Orphans = len(txs), len(orphans)
	for _, tx := range txs {
		if tx.Answered() {
			st.Answered++
		}
		if len(tx.Request) > 1 || len(tx.Response) > 1 {
			st.Segmented++
		}
		st.SegmentACKs += len(tx.SegmentACKs)
	}
	return st, msgs
}
