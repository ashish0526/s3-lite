package s3lite

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrNoSuchUpload  = errors.New("s3lite: no such upload")
	ErrPartMismatch  = errors.New("s3lite: part list does not match the upload")
	ErrEmptyPartList = errors.New("s3lite: complete requires at least one part")
)

// A multipart upload lives entirely under one scratch directory:
//
//	<bucket>/uploads/<uploadID>/target.key   -- the key this upload targets
//	<bucket>/uploads/<uploadID>/parts/NNNNN       -- part bytes, zero-padded part number
//	<bucket>/uploads/<uploadID>/parts/NNNNN.etag  -- that part's content hash
//
// Reusing etagPath's ".etag sidecar next to the bytes" pattern from Chapter
// 2 here is deliberate: it's the same "hash it while you write it, read the
// hash back later" trick, just applied to a part instead of a whole object.

func (s *Store) uploadDir(bucket, uploadID string) (string, error) {
	bdir, err := s.bucketDir(bucket)
	if err != nil {
		return "", err
	}
	return filepath.Join(bdir, "uploads", uploadID), nil
}

func partPath(dir string, partNumber int) string {
	return filepath.Join(dir, "parts", strconv.FormatInt(int64(partNumber), 10)+".part")
}

func newUploadID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// CreateMultipartUpload starts an upload targeting (bucket, key) and
// returns an opaque upload ID. The bucket must already exist; the target
// key is recorded so later calls can be checked against it.
func (s *Store) CreateMultipartUpload(bucket, key string) (uploadID string, err error) {
	ok, err := s.bucketExists(bucket)
	if err != nil {
		return "", err
	}
	if !ok {
		return "", ErrNoSuchBucket
	}
	if _, err := EncodeKey(key); err != nil {
		return "", err
	}
	uploadID, err = newUploadID()
	if err != nil {
		return "", err
	}
	dir, err := s.uploadDir(bucket, uploadID)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(dir, "parts"), 0o755); err != nil {
		return "", err
	}
	if _, err := writeFileAtomic(filepath.Join(dir, "target.key"), strings.NewReader(key)); err != nil {
		return "", err
	}
	return uploadID, nil
}

func (s *Store) checkUploadTarget(bucket, key, uploadID string) (dir string, err error) {
	dir, err = s.uploadDir(bucket, uploadID)
	if err != nil {
		return "", err
	}
	got, err := os.ReadFile(filepath.Join(dir, "target.key"))
	if os.IsNotExist(err) {
		return "", ErrNoSuchUpload
	}
	if err != nil {
		return "", err
	}
	if string(got) != key {
		return "", ErrNoSuchUpload
	}
	return dir, nil
}

// UploadPart stores one part's bytes and returns its content-hash ETag,
// the same way Put does for a whole object — parts can arrive in any
// order, each independently hashed and durably written.
func (s *Store) UploadPart(bucket, key, uploadID string, partNumber int, r io.Reader) (string, error) {
	dir, err := s.checkUploadTarget(bucket, key, uploadID)
	if err != nil {
		return "", err
	}
	path := partPath(dir, partNumber)
	hr := newHashingReader(r)
	if _, err := writeFileAtomic(path, hr); err != nil {
		return "", err
	}
	etag := hr.ETag()
	if _, err := writeFileAtomic(etagPath(path), strings.NewReader(etag)); err != nil {
		return "", err
	}
	return etag, nil
}

// CompletedPart is one entry of the part list a client sends to
// CompleteMultipartUpload — the same ETag UploadPart returned for that
// part, echoed back as an integrity check.
type CompletedPart struct {
	PartNumber int
	ETag       string
}

// CompleteMultipartUpload verifies the caller's part list against what was
// actually uploaded (right ETags, strictly ascending part numbers, matching
// S3's own requirement), concatenates the parts in order into the final
// object, and returns a *composite* ETag: real S3's multipart ETag is not
// a hash of the assembled bytes, it's hex(sha256(concat of each part's raw
// digest)) + "-" + partCount — a well-known real-S3 quirk (an SDK can tell
// a multipart-uploaded object apart from a single-PUT one just by whether
// its ETag contains a "-"), reproduced here on purpose rather than smoothed
// over.
func (s *Store) CompleteMultipartUpload(bucket, key, uploadID string, parts []CompletedPart) (PutResult, error) {
	dir, err := s.checkUploadTarget(bucket, key, uploadID)
	if err != nil {
		return PutResult{}, err
	}
	if len(parts) == 0 {
		return PutResult{}, ErrEmptyPartList
	}
	sorted := append([]CompletedPart(nil), parts...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].PartNumber < sorted[j].PartNumber })
	for i, p := range sorted {
		if i > 0 && p.PartNumber <= sorted[i-1].PartNumber {
			return PutResult{}, ErrPartMismatch
		}
		storedETag, err := os.ReadFile(etagPath(partPath(dir, p.PartNumber)))
		if os.IsNotExist(err) {
			return PutResult{}, ErrPartMismatch
		}
		if err != nil {
			return PutResult{}, err
		}
		if string(storedETag) != p.ETag {
			return PutResult{}, ErrPartMismatch
		}
	}

	// The composite ETag only needs each part's digest bytes, never the
	// part's actual content — so it's computed in one pass up front,
	// independent of the (separate) byte-for-byte concatenation below.
	compositeHash := sha256.New()
	readers := make([]io.Reader, len(sorted))
	files := make([]*os.File, len(sorted))
	defer func() {
		for _, f := range files {
			if f != nil {
				f.Close()
			}
		}
	}()
	for i, p := range sorted {
		f, err := os.Open(partPath(dir, p.PartNumber))
		if err != nil {
			return PutResult{}, err
		}
		files[i] = f
		readers[i] = f
		digest, err := hex.DecodeString(p.ETag)
		if err != nil {
			return PutResult{}, ErrPartMismatch
		}
		compositeHash.Write(digest)
	}
	etag := hex.EncodeToString(compositeHash.Sum(nil)) + "-" + strconv.Itoa(len(sorted))

	// The assembled bytes still go through the same content-addressed blob
	// store Put uses (Chapter 6) — just keyed by the composite ETag instead
	// of one computed from the bytes themselves, since that's what real
	// multipart-uploaded objects are addressed by.
	bpath, err := s.blobPath(bucket, etag)
	if err != nil {
		return PutResult{}, err
	}
	if err := os.MkdirAll(filepath.Dir(bpath), 0o755); err != nil {
		return PutResult{}, err
	}
	total, err := writeFileAtomic(bpath, io.MultiReader(readers...))
	if err != nil {
		return PutResult{}, err
	}

	v, err := s.addVersion(bucket, key, objectVersion{ETag: etag, Size: total, ModTime: time.Now()}, nil)
	if err != nil {
		return PutResult{}, err
	}
	os.RemoveAll(dir) // scratch space only; no durability contract of its own

	return PutResult{ETag: v.ETag, Size: v.Size, VersionID: v.VersionID}, nil
}

// AbortMultipartUpload discards an in-progress upload and its parts.
func (s *Store) AbortMultipartUpload(bucket, key, uploadID string) error {
	dir, err := s.checkUploadTarget(bucket, key, uploadID)
	if err != nil {
		return err
	}
	return os.RemoveAll(dir)
}
