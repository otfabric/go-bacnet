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
	path := filepath.Join(Dir(), "report", part+".json")
	if os.Getenv("BACNET_CAPTURE_REPORT") == "1" {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		return os.WriteFile(path, b, 0o644)
	}
	old, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !bytes.Equal(old, b) {
		return fmt.Errorf("%s is stale: run make captures-report", path)
	}
	return nil
}
