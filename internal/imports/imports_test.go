// SPDX-License-Identifier: MIT

package imports_test

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Enforces PACKAGE_DESIGN dependency direction.
func TestImportBoundaries(t *testing.T) {
	root := filepath.Join("..", "..")
	forbidden := map[string][]string{
		".": {"github.com/otfabric/go-bacnet/bvlc", "github.com/otfabric/go-bacnet/npdu",
			"github.com/otfabric/go-bacnet/apdu", "github.com/otfabric/go-bacnet/service",
			"github.com/otfabric/go-bacnet/client", "github.com/otfabric/go-bacnet/bip"},
		"bvlc": {"github.com/otfabric/go-bacnet/npdu", "github.com/otfabric/go-bacnet/apdu",
			"github.com/otfabric/go-bacnet/service", "github.com/otfabric/go-bacnet/client"},
		"npdu": {"github.com/otfabric/go-bacnet/bvlc", "github.com/otfabric/go-bacnet/apdu",
			"github.com/otfabric/go-bacnet/service", "github.com/otfabric/go-bacnet/client"},
		"apdu": {"github.com/otfabric/go-bacnet/bvlc", "github.com/otfabric/go-bacnet/npdu",
			"github.com/otfabric/go-bacnet/service", "github.com/otfabric/go-bacnet/client"},
		"service": {"github.com/otfabric/go-bacnet/bvlc", "github.com/otfabric/go-bacnet/npdu",
			"github.com/otfabric/go-bacnet/client", "github.com/otfabric/go-bacnet/bip"},
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			base := info.Name()
			if base == "testdata" || base == ".git" || base == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		pkg := filepath.Dir(rel)
		if pkg == "." {
			pkg = "."
		}
		rules, ok := forbidden[pkg]
		if !ok {
			return nil
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		defer func() { _ = f.Close() }()
		sc := bufio.NewScanner(f)
		inImport := false
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if strings.HasPrefix(line, "import (") {
				inImport = true
				continue
			}
			if inImport && line == ")" {
				inImport = false
				continue
			}
			check := line
			if strings.HasPrefix(line, "import ") {
				check = strings.TrimPrefix(line, "import ")
			} else if !inImport {
				continue
			}
			for _, bad := range rules {
				if strings.Contains(check, `"`+bad+`"`) {
					t.Errorf("%s imports forbidden %s", rel, bad)
				}
			}
		}
		return sc.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}
