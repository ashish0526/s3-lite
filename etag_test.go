package s3lite

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"strings"
	"testing"
)

func TestHashingReaderMatchesDirectHash(t *testing.T) {
	data := "the quick brown fox jumps over the lazy dog"
	hr := newHashingReader(strings.NewReader(data))
	n, err := io.Copy(io.Discard, hr)
	if err != nil {
		t.Fatal(err)
	}
	if n != int64(len(data)) {
		t.Fatalf("copied %d bytes, want %d", n, len(data))
	}
	if hr.Size() != int64(len(data)) {
		t.Fatalf("Size() = %d, want %d", hr.Size(), len(data))
	}
	want := sha256.Sum256([]byte(data))
	if hr.ETag() != hex.EncodeToString(want[:]) {
		t.Fatalf("ETag mismatch: got %s", hr.ETag())
	}
}

func TestHashingReaderEmpty(t *testing.T) {
	hr := newHashingReader(strings.NewReader(""))
	io.Copy(io.Discard, hr)
	want := sha256.Sum256(nil)
	if hr.ETag() != hex.EncodeToString(want[:]) {
		t.Fatalf("empty-input ETag mismatch: got %s", hr.ETag())
	}
}
