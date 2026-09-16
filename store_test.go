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
	if _, err := s.Put("b", "a/b.txt", strings.NewReader("payload")); err != nil {
		t.Fatal(err)
	}

	size, err := s.Head("b", "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if size != 7 {
		t.Fatalf("size = %d, want 7", size)
	}

	rc, err := s.Get("b", "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(rc)
	rc.Close()
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "payload" {
		t.Fatalf("got %q", got)
	}

	if err := s.Delete("b", "a/b.txt"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get("b", "a/b.txt"); err != ErrNoSuchKey {
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
	if _, err := s.Get("b", "missing"); err != ErrNoSuchKey {
		t.Fatalf("Get missing key = %v, want ErrNoSuchKey", err)
	}
	if _, err := s.Head("b", "missing"); err != ErrNoSuchKey {
		t.Fatalf("Head missing key = %v, want ErrNoSuchKey", err)
	}
}

func TestDeleteMissingKeyIsNotError(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("b", "missing"); err != nil {
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

func TestPutOverwrite(t *testing.T) {
	s := newTestStore(t)
	if err := s.CreateBucket("b"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("b", "k", strings.NewReader("first")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("b", "k", strings.NewReader("second, longer value")); err != nil {
		t.Fatal(err)
	}
	rc, err := s.Get("b", "k")
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
