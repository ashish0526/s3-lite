package s3lite

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

var versionCounter uint64

// newVersionID returns an identifier that sorts lexicographically in
// creation order: a nanosecond timestamp, plus a per-process counter to
// break ties when two writes land in the same nanosecond — routine under a
// fast test loop, not just a theoretical concern, since Put/Delete can run
// back-to-back faster than the clock visibly advances.
func newVersionID() string {
	n := atomic.AddUint64(&versionCounter, 1)
	return fmt.Sprintf("%020d.%010d", time.Now().UnixNano(), n)
}

// objectVersion is one entry in a key's history. ETag is empty and Deleted
// is true for a delete marker — a version with no bytes of its own,
// recording only that the key was hidden at that point in time.
type objectVersion struct {
	VersionID string
	ETag      string
	Size      int64
	Deleted   bool
	ModTime   time.Time
	Metadata  map[string]string
}

// objectMeta is a key's full version history, newest first.
type objectMeta struct {
	Versions []objectVersion
}

func (m objectMeta) latest() (objectVersion, bool) {
	if len(m.Versions) == 0 {
		return objectVersion{}, false
	}
	return m.Versions[0], true
}

func (m objectMeta) find(versionID string) (objectVersion, bool) {
	for _, v := range m.Versions {
		if v.VersionID == versionID {
			return v, true
		}
	}
	return objectVersion{}, false
}

// metaPath is where a key's version history is recorded.
func (s *Store) metaPath(bucket, key string) (string, error) {
	bdir, err := s.bucketDir(bucket)
	if err != nil {
		return "", err
	}
	enc, err := EncodeKey(key)
	if err != nil {
		return "", err
	}
	return filepath.Join(bdir, "objects", enc+".meta.json"), nil
}

// blobPath is where the bytes for a given content hash live, shared by
// every version (of any key) whose content happens to hash the same.
func (s *Store) blobPath(bucket, etag string) (string, error) {
	bdir, err := s.bucketDir(bucket)
	if err != nil {
		return "", err
	}
	if len(etag) < 2 {
		return "", ErrInvalidKey
	}
	return filepath.Join(bdir, "blobs", etag[:2], etag+".blob"), nil
}

func readMeta(path string) (objectMeta, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return objectMeta{}, nil
	}
	if err != nil {
		return objectMeta{}, err
	}
	var m objectMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return objectMeta{}, err
	}
	return m, nil
}

func writeMeta(path string, m objectMeta) error {
	data, err := json.Marshal(m)
	if err != nil {
		return err
	}
	_, err = writeFileAtomic(path, bytes.NewReader(data))
	return err
}

// addVersion appends v (given a fresh VersionID) as the new newest version
// of (bucket, key) and durably records it. If precond is non-nil, it is
// checked against the key's current latest version (and whether one exists
// at all) inside the very same locked read-modify-write as the append —
// that is what makes a conditional PUT (Chapter 7) an actual
// compare-and-swap rather than a check that can go stale between "check"
// and "act": s.mu serializes every addVersion/DeleteVersion call across the
// whole Store, so two concurrent conditional writers to the same key are
// never both evaluated against the same "current" state.
//
// Deliberate simplification: the lock is store-wide, not per-key — one
// writer to key A blocks a concurrent writer to unrelated key B. Real S3
// obviously doesn't serialize unrelated keys against each other; a per-key
// lock (a map of mutexes, or sharding by key hash) would fix that without
// changing the correctness argument at all. Documented in DESIGN.md as a
// throughput cost accepted for a single-process "-lite" store, not a
// correctness gap.
func (s *Store) addVersion(bucket, key string, v objectVersion, precond func(current objectVersion, exists bool) error) (objectVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	mpath, err := s.metaPath(bucket, key)
	if err != nil {
		return objectVersion{}, err
	}
	meta, err := readMeta(mpath)
	if err != nil {
		return objectVersion{}, err
	}
	if precond != nil {
		cur, exists := meta.latest()
		if err := precond(cur, exists); err != nil {
			return objectVersion{}, err
		}
	}
	v.VersionID = newVersionID()
	meta.Versions = append([]objectVersion{v}, meta.Versions...)
	if err := writeMeta(mpath, meta); err != nil {
		return objectVersion{}, err
	}
	return v, nil
}

// DeleteVersion permanently erases one specific version's record from a
// key's history — real S3's behavior for DELETE with an explicit version
// ID, as opposed to Store.Delete's marker-only DELETE. The blob that
// version pointed at is deliberately not garbage-collected here: because
// blobs are content-addressed and shared, another version (of this key or
// any other) might still point at the very same bytes, and this build has
// no reference count to check — see DESIGN.md.
func (s *Store) DeleteVersion(bucket, key, versionID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	mpath, err := s.metaPath(bucket, key)
	if err != nil {
		return err
	}
	meta, err := readMeta(mpath)
	if err != nil {
		return err
	}
	idx := -1
	for i, v := range meta.Versions {
		if v.VersionID == versionID {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrNoSuchKey
	}
	meta.Versions = append(meta.Versions[:idx], meta.Versions[idx+1:]...)
	return writeMeta(mpath, meta)
}

// resolveVersion looks up one version of a key: the latest live (non
// delete-marker) version when versionID is "", or a specific version by ID
// otherwise (which CAN be a delete marker — the caller decides what that
// means for it).
func (s *Store) resolveVersion(bucket, key, versionID string) (objectVersion, error) {
	mpath, err := s.metaPath(bucket, key)
	if err != nil {
		return objectVersion{}, err
	}
	meta, err := readMeta(mpath)
	if err != nil {
		return objectVersion{}, err
	}
	if versionID == "" {
		v, ok := meta.latest()
		if !ok || v.Deleted {
			return objectVersion{}, ErrNoSuchKey
		}
		return v, nil
	}
	v, ok := meta.find(versionID)
	if !ok {
		return objectVersion{}, ErrNoSuchKey
	}
	return v, nil
}
