// SPDX-License-Identifier: MIT

package fixtures

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeInterop lays out a bacnet-interop root with the given files under
// fixtures/ and points BACNET_INTEROP_ROOT at it.
func fakeInterop(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		p := filepath.Join(root, "fixtures", filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("BACNET_INTEROP_ROOT", root)
	return root
}

func TestLoadAllReadsManifestEntries(t *testing.T) {
	fakeInterop(t, map[string]string{
		"manifest.json":   `{"fixtures":[{"id":"a","path":"fixtures/x/a.json"}]}`,
		"x/a.json":        `{"id":"a","input_hex":"0A"}`,
		"x/unlisted.json": `{"id":"ignored"}`,
	})
	got, err := LoadAll()
	if err != nil || len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("got %+v err=%v", got, err)
	}
}

func TestLoadAllErrors(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"no manifest", map[string]string{"x/a.json": `{}`}, "manifest.json"},
		{"bad manifest", map[string]string{"manifest.json": `{`}, "unexpected end"},
		{"missing entry", map[string]string{"manifest.json": `{"fixtures":[{"id":"a","path":"fixtures/a.json"}]}`}, "fixtures/a.json"},
		{"bad entry", map[string]string{
			"manifest.json": `{"fixtures":[{"id":"a","path":"fixtures/a.json"}]}`,
			"a.json":        `[`,
		}, "fixtures/a.json"},
		{"id mismatch", map[string]string{
			"manifest.json": `{"fixtures":[{"id":"a","path":"fixtures/a.json"}]}`,
			"a.json":        `{"id":"b"}`,
		}, "id mismatch"},
		{"dot-dot escape", map[string]string{"manifest.json": `{"fixtures":[{"id":"a","path":"fixtures/../secret.json"}]}`}, "escapes fixtures/"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fakeInterop(t, tc.files)
			if _, err := LoadAll(); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err=%v, want %q", err, tc.want)
			}
		})
	}
}

func TestLoadAllNoFixturesDir(t *testing.T) {
	t.Setenv("BACNET_INTEROP_ROOT", t.TempDir())
	if _, err := LoadAll(); err == nil {
		t.Fatal("want error for a root without fixtures/")
	}
}

// A symlink inside fixtures/ that points outside it must not be followed.
func TestLoadAllRefusesSymlinkEscape(t *testing.T) {
	root := fakeInterop(t, map[string]string{
		"manifest.json": `{"fixtures":[{"id":"a","path":"fixtures/a.json"}]}`,
	})
	outside := filepath.Join(root, "secret.json")
	if err := os.WriteFile(outside, []byte(`{"id":"a"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "fixtures", "a.json")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if _, err := LoadAll(); err == nil {
		t.Fatal("a symlink out of fixtures/ was followed")
	}
}
