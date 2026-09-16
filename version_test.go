package s3lite

import (
	"io"
	"strings"
	"testing"
)

func TestPutKeepsEveryVersionAddressable(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	v1, err := s.Put("b", "k", strings.NewReader("first"))
	if err != nil {
		t.Fatal(err)
	}
	v2, err := s.Put("b", "k", strings.NewReader("second"))
	if err != nil {
		t.Fatal(err)
	}
	if v1.VersionID == v2.VersionID {
		t.Fatal("two Puts produced the same VersionID")
	}

	// The old version is still readable by ID even though a newer one
	// exists — Put never overwrites history.
	rc, info, err := s.Get("b", "k", v1.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "first" || info.VersionID != v1.VersionID {
		t.Fatalf("Get(v1) = %q, %+v, want \"first\"", got, info)
	}

	latest, _, err := s.Get("b", "k", "")
	if err != nil {
		t.Fatal(err)
	}
	defer latest.Close()
	gotLatest, _ := io.ReadAll(latest)
	if string(gotLatest) != "second" {
		t.Fatalf("Get(latest) = %q, want \"second\"", gotLatest)
	}
}

func TestDeleteHidesLatestButKeepsHistory(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	put, err := s.Put("b", "k", strings.NewReader("data"))
	if err != nil {
		t.Fatal(err)
	}
	markerID, err := s.Delete("b", "k")
	if err != nil {
		t.Fatal(err)
	}

	if _, _, err := s.Get("b", "k", ""); err != ErrNoSuchKey {
		t.Fatalf("Get latest after delete = %v, want ErrNoSuchKey", err)
	}
	if _, err := s.Head("b", "k", ""); err != ErrNoSuchKey {
		t.Fatalf("Head latest after delete = %v, want ErrNoSuchKey", err)
	}

	// The old version is still there, and the delete marker itself is a
	// real, addressable version.
	rc, _, err := s.Get("b", "k", put.VersionID)
	if err != nil {
		t.Fatal(err)
	}
	rc.Close()
	if _, _, err := s.Get("b", "k", markerID); err != ErrIsDeleteMarker {
		t.Fatalf("Get(delete marker) = %v, want ErrIsDeleteMarker", err)
	}
}

func TestDeleteOnNeverWrittenKeyStillCreatesMarker(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	markerID, err := s.Delete("b", "never-existed")
	if err != nil {
		t.Fatal(err)
	}
	if markerID == "" {
		t.Fatal("expected a non-empty delete marker VersionID")
	}
	if _, _, err := s.Get("b", "never-existed", ""); err != ErrNoSuchKey {
		t.Fatalf("Get after delete-of-nonexistent = %v, want ErrNoSuchKey", err)
	}
}

func TestPutAfterDeleteRevivesTheKey(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	if _, err := s.Put("b", "k", strings.NewReader("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Delete("b", "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Put("b", "k", strings.NewReader("v2")); err != nil {
		t.Fatal(err)
	}
	rc, _, err := s.Get("b", "k", "")
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	got, _ := io.ReadAll(rc)
	if string(got) != "v2" {
		t.Fatalf("got %q, want v2 (a Put after a delete marker should become the new latest)", got)
	}
}
