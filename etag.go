package s3lite

import (
	"crypto/sha256"
	"encoding/hex"
	"hash"
	"io"
)

// hashingReader wraps a reader, hashing every byte as it streams through —
// no second pass over the data is needed to compute a content hash, the
// same principle db45's checksum step and postgres-in-steps' page checksums
// both rely on: fold integrity into the write you were doing anyway.
type hashingReader struct {
	r io.Reader
	h hash.Hash
	n int64
}

func newHashingReader(r io.Reader) *hashingReader {
	return &hashingReader{r: r, h: sha256.New()}
}

func (c *hashingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.h.Write(p[:n])
		c.n += int64(n)
	}
	return n, err
}

// ETag returns S3's stand-in for a content hash: hex-encoded, here SHA-256
// (real S3 uses MD5 for single-part uploads, and a different composite
// scheme for multipart — Chapter 4 documents the multipart case). Only
// valid after the reader has been fully consumed.
func (c *hashingReader) ETag() string { return hex.EncodeToString(c.h.Sum(nil)) }

// Size reports the number of bytes read so far.
func (c *hashingReader) Size() int64 { return c.n }
