package s3lite

import (
	"io"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestPutGetHeadDeleteRoundTrip(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	put, err := s.Put("b", "a/b.txt", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	if put.Size != 7 || put.ETag == "" || put.VersionID == "" {
		t.Fatalf("PutResult = %+v, want Size=7, a non-empty ETag and VersionID", put)
	}

	info, err := s.Head("b", "a/b.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 7 {
		t.Fatalf("size = %d, want 7", info.Size)
	}
	if info.ETag != put.ETag {
		t.Fatalf("Head ETag %q != Put ETag %q", info.ETag, put.ETag)
	}

	rc, getInfo, err := s.Get("b", "a/b.txt", "")
	if err != nil {
		t.Fatal(err)
	}
	if getInfo.VersionID != put.VersionID {
		t.Fatalf("Get VersionID %q != Put VersionID %q", getInfo.VersionID, put.VersionID)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Fatalf("got %q", got)
	}

	if _, err := s.Delete("b", "a/b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("b", "a/b.txt", ""); err != ErrNoSuchKey {
		t.Fatalf("Get after Delete = %v, want ErrNoSuchKey", err)
	}
}

func TestPutRequiresExistingBucket(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.Put("nope", "k", strings.NewReader("x")); err != ErrNoSuchBucket {
		t.Fatalf("Put into missing bucket = %v, want ErrNoSuchBucket", err)
	}
}

func TestGetMissingKey(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Get("b", "missing", ""); err != ErrNoSuchKey {
		t.Fatalf("Get missing key = %v, want ErrNoSuchKey", err)
	}
	if _, err := s.Head("b", "missing", ""); err != ErrNoSuchKey {
		t.Fatalf("Head missing key = %v, want ErrNoSuchKey", err)
	}
}

func TestDeleteMissingKeyIsNotError(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Delete("b", "missing"); err != nil {
		t.Fatalf("Delete of missing key should not error, got %v", err)
	}
}

func TestCreateBucketIdempotent(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	if err := s.CreateBucket("b"); err != nil {
		t.Fatalf("recreating an existing bucket should not error, got %v", err)
	}
}

func TestPutETagIsContentAddressed(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	a, err := s.Put("b", "x", strings.NewReader("same bytes"))
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.Put("b", "y", strings.NewReader("same bytes"))
	if err != nil {
		t.Fatal(err)
	}
	if a.ETag != b.ETag {
		t.Fatalf("identical content under different keys got different ETags: %q vs %q", a.ETag, b.ETag)
	}
}

func TestPutOverwrite(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	first, err := s.Put("b", "k", strings.NewReader("first"))
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Put("b", "k", strings.NewReader("second, longer value"))
	if err != nil {
		t.Fatal(err)
	}
	if first.ETag == second.ETag {
		t.Fatalf("ETag did not change across an overwrite with different content")
	}
	if info, err := s.Head("b", "k", ""); err != nil || info.ETag != second.ETag {
		t.Fatalf("Head after overwrite = %+v, %v; want ETag %q", info, err, second.ETag)
	}
	rc, _, err := s.Get("b", "k", "")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "second, longer value" {
		t.Fatalf("got %q", got)
	}
}
