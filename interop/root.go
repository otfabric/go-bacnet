// SPDX-License-Identifier: MIT

package interop

import (
	"fmt"
	"os"
	"path/filepath"
)

// resolveInteropRootFrom picks the bacnet-interop checkout to mount into re-exec
// containers. Prefer envRoot (typically BACNET_INTEROP_ROOT), else the sibling
// directory next to the go-bacnet module root.
func resolveInteropRootFrom(modRoot, envRoot string) (string, error) {
	interopRoot := envRoot
	if interopRoot == "" {
		if modRoot == "" {
			return "", fmt.Errorf("module root is empty")
		}
		interopRoot = filepath.Join(modRoot, "..", "bacnet-interop")
	}
	abs, err := filepath.Abs(interopRoot)
	if err != nil {
		return "", fmt.Errorf("resolve bacnet-interop root %q: %w", interopRoot, err)
	}
	if err := requireInteropFixtures(abs); err != nil {
		return "", err
	}
	return abs, nil
}

func requireInteropFixtures(root string) error {
	for _, rel := range []string{
		filepath.Join("fixtures", "device", "device-baseline-v1.json"),
		filepath.Join("fixtures", "manifest.json"),
	} {
		p := filepath.Join(root, rel)
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return fmt.Errorf("bacnet-interop fixtures incomplete at %s: missing %s", root, rel)
		}
	}
	return nil
}
