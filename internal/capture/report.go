// SPDX-License-Identifier: MIT

package capture

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Report checks one part of the machine-readable capture coverage report
// (testdata/captures/report/<part>.json) against v, as the suite that
// computed v sees it now. With BACNET_CAPTURE_REPORT=1 it rewrites the
// file instead (make captures-report). A stale report is an error, so the
// committed report always states what the suites check.
func Report(part string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	root, err := os.OpenRoot(Dir())
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()
	name := filepath.Join("report", filepath.Base(part)+".json")
	if os.Getenv("BACNET_CAPTURE_REPORT") == "1" {
		if err := root.MkdirAll("report", 0o750); err != nil {
			return err
		}
		return root.WriteFile(name, b, 0o600)
	}
	old, err := root.ReadFile(name)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, b) {
		return fmt.Errorf("testdata/captures/%s is stale: run make captures-report", name)
	}
	return nil
}
