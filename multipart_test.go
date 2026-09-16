package s3lite

import (
	"io"
	"strings"
	"testing"
)

func TestMultipartUploadRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	uploadID, err := s.CreateMultipartUpload("b", "big.bin")
	if err != nil {
		t.Fatal(err)
	}

	// Upload part 2 before part 1 — S3 allows any order.
	etag2, err := s.UploadPart("b", "big.bin", uploadID, 2, strings.NewReader("world"))
	if err != nil {
		t.Fatal(err)
	}
	etag1, err := s.UploadPart("b", "big.bin", uploadID, 1, strings.NewReader("hello "))
	if err != nil {
		t.Fatal(err)
	}

	result, err := s.CompleteMultipartUpload("b", "big.bin", uploadID, []CompletedPart{
		{PartNumber: 2, ETag: etag2},
		{PartNumber: 1, ETag: etag1},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Size != int64(len("hello world")) {
		t.Fatalf("size = %d, want %d", result.Size, len("hello world"))
	}
	if !strings.Contains(result.ETag, "-2") {
		t.Fatalf("composite ETag %q should be suffixed with the part count", result.ETag)
	}

	rc, _, err := s.Get("b", "big.bin", "")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "hello world" {
		t.Fatalf("got %q, want parts concatenated in part-number order", got)
	}

	info, err := s.Head("b", "big.bin", "")
	if err != nil {
		t.Fatal(err)
	}
	if info.ETag != result.ETag {
		t.Fatalf("Head ETag %q != Complete's ETag %q", info.ETag, result.ETag)
	}

	// The upload is gone after completion.
	if _, err := s.UploadPart("b", "big.bin", uploadID, 3, strings.NewReader("x")); err != ErrNoSuchUpload {
		t.Fatalf("UploadPart after completion = %v, want ErrNoSuchUpload", err)
	}
}

func TestMultipartCompleteRejectsBadETag(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	uploadID, err := s.CreateMultipartUpload("b", "k")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadPart("b", "k", uploadID, 1, strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	_, err = s.CompleteMultipartUpload("b", "k", uploadID, []CompletedPart{{PartNumber: 1, ETag: "wrong"}})
	if err != ErrPartMismatch {
		t.Fatalf("Complete with wrong ETag = %v, want ErrPartMismatch", err)
	}
}

func TestMultipartCompleteRejectsUnsortedDuplicatePartNumbers(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	uploadID, err := s.CreateMultipartUpload("b", "k")
	if err != nil {
		t.Fatal(err)
	}
	etag, err := s.UploadPart("b", "k", uploadID, 1, strings.NewReader("data"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.CompleteMultipartUpload("b", "k", uploadID, []CompletedPart{
		{PartNumber: 1, ETag: etag},
		{PartNumber: 1, ETag: etag},
	})
	if err != ErrPartMismatch {
		t.Fatalf("Complete with duplicate part numbers = %v, want ErrPartMismatch", err)
	}
}

func TestMultipartAbortRemovesParts(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	uploadID, err := s.CreateMultipartUpload("b", "k")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadPart("b", "k", uploadID, 1, strings.NewReader("data")); err != nil {
		t.Fatal(err)
	}
	if err := s.AbortMultipartUpload("b", "k", uploadID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadPart("b", "k", uploadID, 2, strings.NewReader("x")); err != ErrNoSuchUpload {
		t.Fatalf("UploadPart after abort = %v, want ErrNoSuchUpload", err)
	}
}

func TestMultipartWrongKeyIsRejected(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	uploadID, err := s.CreateMultipartUpload("b", "k1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UploadPart("b", "k2", uploadID, 1, strings.NewReader("x")); err != ErrNoSuchUpload {
		t.Fatalf("UploadPart under the wrong key = %v, want ErrNoSuchUpload", err)
	}
}
