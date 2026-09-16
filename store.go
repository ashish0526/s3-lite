package s3lite

import (
	"errors"
	"io"
	"os"
	"path/filepath"
)

// ErrNoSuchBucket and ErrNoSuchKey mirror S3's own error names; the HTTP
// layer (Chapter 3) maps these onto S3-shaped error responses.
var (
	ErrNoSuchBucket = errors.New("s3lite: no such bucket")
	ErrNoSuchKey    = errors.New("s3lite: no such key")
)

// Store is the blob layer: buckets and keys mapped onto files under a root
// directory, written and deleted atomically. It knows nothing about HTTP,
// hashing, multipart, versions, or metadata yet — those are every later
// chapter, layered on top of Put/Get/Head/Delete.
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

// objectPath is the on-disk location of one key's current bytes.
func (s *Store) objectPath(bucket, key string) (string, error) {
	bdir, err := s.bucketDir(bucket)
	if err != nil {
		return "", err
	}
	enc, err := EncodeKey(key)
	if err != nil {
		return "", err
	}
	return filepath.Join(bdir, "objects", enc), nil
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

// Put writes an object's bytes, replacing any existing bytes at that key
// atomically (writeFileAtomic). Returns the number of bytes written.
func (s *Store) Put(bucket, key string, r io.Reader) (int64, error) {
	ok, err := s.bucketExists(bucket)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrNoSuchBucket
	}
	path, err := s.objectPath(bucket, key)
	if err != nil {
		return 0, err
	}
	return writeFileAtomic(path, r)
}

// Get opens an object for reading. The caller must Close it.
func (s *Store) Get(bucket, key string) (io.ReadCloser, error) {
	path, err := s.objectPath(bucket, key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil, ErrNoSuchKey
	}
	if err != nil {
		return nil, err
	}
	return f, nil
}

// Head reports an object's size without reading its bytes.
func (s *Store) Head(bucket, key string) (size int64, err error) {
	path, err := s.objectPath(bucket, key)
	if err != nil {
		return 0, err
	}
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return 0, ErrNoSuchKey
	}
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

// Delete removes an object. Deleting a key that does not exist is not an
// error — the caller's desired end state (the key is gone) already holds.
func (s *Store) Delete(bucket, key string) error {
	path, err := s.objectPath(bucket, key)
	if err != nil {
		return err
	}
	return removeFileAtomic(path)
}
