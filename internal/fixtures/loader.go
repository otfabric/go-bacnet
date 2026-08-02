// SPDX-License-Identifier: MIT

// Package fixtures loads bacnet-interop corpus metadata for unit tests.
package fixtures

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Meta is the subset of fixture provenance needed by go-bacnet tests.
type Meta struct {
	ID            string         `json:"id"`
	Description   string         `json:"description"`
	InputHex      string         `json:"input_hex"`
	Tags          []string       `json:"tags"`
	Expect        Expect         `json:"expect"`
	License       License        `json:"license"`
	Operation     string         `json:"operation"`
	Expected      map[string]any `json:"expected"`
	ExpectedError *ExpectedError `json:"expected_error"`
	SemanticFile  string         `json:"semantic_file"`
}

// Expect mirrors bacnet-interop expect flags.
type Expect struct {
	SemanticDecodeEqual        bool `json:"semantic_decode_equal"`
	DeterministicReencodeEqual bool `json:"deterministic_reencode_equal"`
	OriginalBytesEqual         bool `json:"original_bytes_equal"`
}

// ExpectedError is the negative-path contract for malformed fixtures.
type ExpectedError struct {
	Category string `json:"category"`
	Layer    string `json:"layer"`
}

// License mirrors bacnet-interop license classification.
type License struct {
	Status string `json:"status"`
}

type manifest struct {
	Fixtures []struct {
		ID   string `json:"id"`
		Path string `json:"path"`
	} `json:"fixtures"`
}

// Root resolves the bacnet-interop repository root.
// Order: BACNET_INTEROP_ROOT, then walk upward from this file / cwd for a
// sibling bacnet-interop checkout.
func Root() (string, error) {
	if v := os.Getenv("BACNET_INTEROP_ROOT"); v != "" {
		return filepath.Clean(v), nil
	}
	var starts []string
	if _, file, _, ok := runtime.Caller(0); ok {
		starts = append(starts, filepath.Dir(file))
	}
	if wd, err := os.Getwd(); err == nil {
		starts = append(starts, wd)
	}
	seen := map[string]bool{}
	for _, start := range starts {
		dir := start
		for i := 0; i < 8; i++ {
			cand := filepath.Join(dir, "..", "bacnet-interop")
			abs, err := filepath.Abs(cand)
			if err == nil && !seen[abs] {
				seen[abs] = true
				if st, err := os.Stat(filepath.Join(abs, "fixtures", "manifest.json")); err == nil && !st.IsDir() {
					return abs, nil
				}
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return "", fmt.Errorf("fixtures: bacnet-interop root not found; set BACNET_INTEROP_ROOT")
}

// LoadAll loads every manifest entry's metadata.
func LoadAll() ([]Meta, error) {
	root, err := Root()
	if err != nil {
		return nil, err
	}
	fixturesRoot, err := filepath.Abs(filepath.Join(root, "fixtures"))
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(filepath.Join(root, "fixtures", "manifest.json"))
	if err != nil {
		return nil, err
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	out := make([]Meta, 0, len(m.Fixtures))
	for _, e := range m.Fixtures {
		path := filepath.Join(root, filepath.FromSlash(e.Path))
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("fixtures: %s: %w", e.Path, err)
		}
		if !strings.HasPrefix(abs, fixturesRoot+string(os.PathSeparator)) && abs != fixturesRoot {
			return nil, fmt.Errorf("fixtures: %s escapes fixtures/", e.Path)
		}
		b, err := os.ReadFile(abs)
		if err != nil {
			return nil, fmt.Errorf("fixtures: %s: %w", e.Path, err)
		}
		var meta Meta
		if err := json.Unmarshal(b, &meta); err != nil {
			return nil, fmt.Errorf("fixtures: %s: %w", e.Path, err)
		}
		if meta.ID != e.ID {
			return nil, fmt.Errorf("fixtures: %s id mismatch manifest=%s meta=%s", e.Path, e.ID, meta.ID)
		}
		if meta.Expect.SemanticDecodeEqual && meta.SemanticFile == "" && meta.Operation == "" {
			return nil, fmt.Errorf("fixtures: %s semantic_decode_equal without operation or semantic_file", meta.ID)
		}
		out = append(out, meta)
	}
	return out, nil
}

// Bytes returns decoded input_hex.
func (m Meta) Bytes() ([]byte, error) {
	// Empty input_hex is a valid empty service payload (e.g. GetAlarmSummary).
	// Distinguish "missing key" from "" by requiring the field when other expect
	// flags demand bytes; callers with explicit "" decode to zero-length.
	if m.InputHex == "" {
		return []byte{}, nil
	}
	return hex.DecodeString(strings.ToLower(m.InputHex))
}

// HasTag reports whether m carries tag.
func (m Meta) HasTag(tag string) bool {
	for _, t := range m.Tags {
		if t == tag {
			return true
		}
	}
	return false
}

// Malformed reports malformed-constructed license status.
func (m Meta) Malformed() bool {
	return m.License.Status == "malformed-constructed"
}
