package s3lite

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteFileAtomicRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a", "b", "obj")
	n, err := writeFileAtomic(path, strings.NewReader("hello world"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 11 {
		t.Fatalf("wrote %d bytes, want 11", n)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Fatalf("got %q", got)
	}
	// no leftover temp files
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("leftover temp file: %s", e.Name())
		}
	}
}

func TestWriteFileAtomicOverwriteIsAllOrNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "obj")
	if _, err := writeFileAtomic(path, strings.NewReader("version one")); err != nil {
		t.Fatal(err)
	}
	if _, err := writeFileAtomic(path, strings.NewReader("v2")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "v2" {
		t.Fatalf("got %q, want a clean overwrite with no trailing bytes from the longer first write", got)
	}
}

func TestRemoveFileAtomicMissingIsNotError(t *testing.T) {
	dir := t.TempDir()
	if err := removeFileAtomic(filepath.Join(dir, "nope")); err != nil {
		t.Fatalf("removing a missing file should not error, got %v", err)
	}
}
