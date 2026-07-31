// SPDX-License-Identifier: MIT

package interop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolveInteropRootFromEnv(t *testing.T) {
	root := t.TempDir()
	mustWriteFixtureTree(t, root)

	got, err := resolveInteropRootFrom("/unused/mod", root)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(root)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveInteropRootFromSibling(t *testing.T) {
	base := t.TempDir()
	modRoot := filepath.Join(base, "go-bacnet")
	interop := filepath.Join(base, "bacnet-interop")
	if err := os.MkdirAll(modRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWriteFixtureTree(t, interop)

	got, err := resolveInteropRootFrom(modRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs(interop)
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestResolveInteropRootMissingFixtures(t *testing.T) {
	root := t.TempDir()
	_, err := resolveInteropRootFrom("/unused", root)
	if err == nil {
		t.Fatal("expected error for incomplete fixtures")
	}
}

func mustWriteFixtureTree(t *testing.T, root string) {
	t.Helper()
	device := filepath.Join(root, "fixtures", "device")
	if err := os.MkdirAll(device, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(device, "device-baseline-v1.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "fixtures", "manifest.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}
}
