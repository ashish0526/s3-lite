package s3lite

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// writeFileAtomic writes r's bytes to path such that a reader never observes
// a partial file: the data lands in a temp file in the same directory
// first, is fsynced, then swapped into place with os.Rename (atomic on the
// same filesystem), and finally the parent directory itself is fsynced so
// the rename survives a crash. Same durability lesson as db45's Log.Write
// and postgres-in-steps' WAL: a write isn't durable until fsync returns,
// and on Unix fsyncing a file does not flush the directory entry that
// records its existence or its new name — that needs its own fsync.
func writeFileAtomic(path string, r io.Reader) (n int64, err error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return 0, err
	}
	tmpPath := tmp.Name()
	defer func() {
		if err != nil {
			os.Remove(tmpPath)
		}
	}()

	n, err = io.Copy(tmp, r)
	if err != nil {
		tmp.Close()
		return n, err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return n, err
	}
	if err = tmp.Close(); err != nil {
		return n, err
	}
	if err = os.Rename(tmpPath, path); err != nil {
		return n, err
	}
	if err = syncDir(dir); err != nil {
		return n, err
	}
	return n, nil
}

// syncDir fsyncs a directory so that entries created or renamed within it
// (a new file, a rename target) are durable, not just present in the page
// cache. Windows does not support opening a directory for Sync; callers on
// that platform silently skip it, matching db45's fsync.go convention.
func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	if err := d.Sync(); err != nil {
		return fmt.Errorf("fsync dir %s: %w", dir, err)
	}
	return nil
}

// removeFileAtomic deletes path if present; ENOENT is not an error, since a
// delete of something already gone is the state the caller wanted anyway.
func removeFileAtomic(path string) error {
	dir := filepath.Dir(path)
	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if os.IsNotExist(err) {
		// Nothing was ever written under dir, so there is nothing dirty
		// in its directory entry to flush — and dir itself may not exist.
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			return nil
		}
	}
	return syncDir(dir)
}
