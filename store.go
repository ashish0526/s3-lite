package s3lite

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

// ErrNoSuchBucket, ErrNoSuchKey, and ErrIsDeleteMarker mirror S3's own
// error names; the HTTP layer (Chapter 3) maps these onto S3-shaped error
// responses.
var (
	ErrNoSuchBucket   = errors.New("s3lite: no such bucket")
	ErrNoSuchKey      = errors.New("s3lite: no such key")
	ErrIsDeleteMarker = errors.New("s3lite: that version is a delete marker")
)

// Store is the blob layer. An object's bytes live in a content-addressed
// blob keyed by ETag, shared across every version and every key whose
// content happens to hash the same; a key's version history — which blob
// each version points at, in what order — lives in a small per-key JSON
// metadata file. This replaces the "one file per key" model of Chapters
// 1-2 outright (see version.go): Put now always creates a new version
// instead of overwriting bytes in place.
type Store struct {
	root string
}

// NewStore opens (creating if necessary) a blob store rooted at dir.
func NewStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{root: dir}, nil
}

func (s *Store) bucketDir(bucket string) (string, error) {
	enc, err := EncodeBucket(bucket)
	if err != nil {
		return "", err
	}
	return filepath.Join(s.root, enc), nil
}

// CreateBucket makes a bucket ready to hold objects. Creating an
// already-existing bucket is not an error (idempotent, like S3's own
// CreateBucket when you already own it).
func (s *Store) CreateBucket(bucket string) error {
	bdir, err := s.bucketDir(bucket)
	if err != nil {
		return err
	}
	return os.MkdirAll(bdir, 0o755)
}

// etagPath is a sidecar file recording a hash next to some bytes that
// aren't a key's current version — used by multipart upload (Chapter 4)
// for each part's own ETag, independent of the object versioning model
// below.
func etagPath(path string) string { return path + ".etag" }

func (s *Store) bucketExists(bucket string) (bool, error) {
	bdir, err := s.bucketDir(bucket)
	if err != nil {
		return false, err
	}
	info, err := os.Stat(bdir)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.IsDir(), nil
}

// PutResult reports what actually landed on disk from a Put: the new
// version's ETag, size, and ID.
type PutResult struct {
	ETag      string
	Size      int64
	VersionID string
}

// ObjectInfo is what Head/Get can tell you about the version they resolved.
type ObjectInfo struct {
	Size      int64
	ETag      string
	VersionID string
}

// storeBlob writes r's bytes into the bucket's content-addressed blob
// store, returning the resulting ETag and size. Identical content under
// any key or version shares the same blob on disk — a write whose hash
// already exists just discards the freshly-written temp copy instead of
// duplicating bytes that are already there.
func (s *Store) storeBlob(bucket string, r io.Reader) (etag string, size int64, err error) {
	bdir, err := s.bucketDir(bucket)
	if err != nil {
		return "", 0, err
	}
	tmpDir := filepath.Join(bdir, "blobs", "tmp")
	if err := os.MkdirAll(tmpDir, 0o755); err != nil {
		return "", 0, err
	}
	tmp, err := os.CreateTemp(tmpDir, ".upload-*")
	if err != nil {
		return "", 0, err
	}
	tmpPath := tmp.Name()
	hr := newHashingReader(r)
	_, copyErr := io.Copy(tmp, hr)
	syncErr := tmp.Sync()
	closeErr := tmp.Close()
	if copyErr != nil || syncErr != nil || closeErr != nil {
		os.Remove(tmpPath)
		if copyErr != nil {
			return "", 0, copyErr
		}
		if syncErr != nil {
			return "", 0, syncErr
		}
		return "", 0, closeErr
	}

	etag = hr.ETag()
	bpath, err := s.blobPath(bucket, etag)
	if err != nil {
		os.Remove(tmpPath)
		return "", 0, err
	}
	if _, statErr := os.Stat(bpath); statErr == nil {
		os.Remove(tmpPath) // identical content already stored
		return etag, hr.Size(), nil
	}
	if err := os.MkdirAll(filepath.Dir(bpath), 0o755); err != nil {
		os.Remove(tmpPath)
		return "", 0, err
	}
	if err := os.Rename(tmpPath, bpath); err != nil {
		os.Remove(tmpPath)
		return "", 0, err
	}
	if err := syncDir(filepath.Dir(bpath)); err != nil {
		return "", 0, err
	}
	return etag, hr.Size(), nil
}

// Put writes an object's bytes as a new version. A key's history only ever
// grows — nothing already on disk is overwritten or removed.
func (s *Store) Put(bucket, key string, r io.Reader) (PutResult, error) {
	ok, err := s.bucketExists(bucket)
	if err != nil {
		return PutResult{}, err
	}
	if !ok {
		return PutResult{}, ErrNoSuchBucket
	}
	if _, err := EncodeKey(key); err != nil {
		return PutResult{}, err
	}
	etag, size, err := s.storeBlob(bucket, r)
	if err != nil {
		return PutResult{}, err
	}
	v, err := s.addVersion(bucket, key, objectVersion{ETag: etag, Size: size, ModTime: time.Now()})
	if err != nil {
		return PutResult{}, err
	}
	return PutResult{ETag: v.ETag, Size: v.Size, VersionID: v.VersionID}, nil
}

// Get opens a version for reading — the latest live version if versionID
// is "", a specific version otherwise. The caller must Close it.
func (s *Store) Get(bucket, key, versionID string) (io.ReadCloser, ObjectInfo, error) {
	v, err := s.resolveVersion(bucket, key, versionID)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	if v.Deleted {
		return nil, ObjectInfo{}, ErrIsDeleteMarker
	}
	bpath, err := s.blobPath(bucket, v.ETag)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	f, err := os.Open(bpath)
	if err != nil {
		return nil, ObjectInfo{}, err
	}
	return f, ObjectInfo{Size: v.Size, ETag: v.ETag, VersionID: v.VersionID}, nil
}

// Head reports a version's size and ETag without reading its bytes.
func (s *Store) Head(bucket, key, versionID string) (ObjectInfo, error) {
	v, err := s.resolveVersion(bucket, key, versionID)
	if err != nil {
		return ObjectInfo{}, err
	}
	if v.Deleted {
		return ObjectInfo{}, ErrIsDeleteMarker
	}
	return ObjectInfo{Size: v.Size, ETag: v.ETag, VersionID: v.VersionID}, nil
}

// Delete appends a delete marker as the new latest version — it does not
// remove any bytes or any earlier version. This is real S3 behavior on a
// versioned bucket: a plain DELETE hides the key (Get/Head with no
// version now report ErrNoSuchKey) without erasing its history, and even
// a key that never existed gets a delete marker, since "hide this key"
// is a well-defined operation whether or not it currently has content.
func (s *Store) Delete(bucket, key string) (versionID string, err error) {
	v, err := s.addVersion(bucket, key, objectVersion{Deleted: true, ModTime: time.Now()})
	if err != nil {
		return "", err
	}
	return v.VersionID, nil
}
