package s3lite

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestPutMetadataRoundTrips(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	_, err := s.PutWithOptions("b", "k", strings.NewReader("data"), PutOptions{
		Metadata: map[string]string{"author": "ashish", "kind": "note"},
	})
	if err != nil {
		t.Fatal(err)
	}
	info, err := s.Head("b", "k", "")
	if err != nil {
		t.Fatal(err)
	}
	if info.Metadata["author"] != "ashish" || info.Metadata["kind"] != "note" {
		t.Fatalf("Metadata = %+v", info.Metadata)
	}
}

func TestConditionalPutIfMatchSucceedsOnCurrentETag(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	put, err := s.Put("b", "k", strings.NewReader("v1"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.PutWithOptions("b", "k", strings.NewReader("v2"), PutOptions{IfMatch: put.ETag})
	if err != nil {
		t.Fatalf("conditional PUT against the current ETag should succeed, got %v", err)
	}
}

func TestConditionalPutIfMatchFailsOnStaleETag(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	if _, err := s.Put("b", "k", strings.NewReader("v1")); err != nil {
		t.Fatal(err)
	}
	// Someone else updates the key in between.
	if _, err := s.Put("b", "k", strings.NewReader("v2")); err != nil {
		t.Fatal(err)
	}
	_, err := s.PutWithOptions("b", "k", strings.NewReader("v3"), PutOptions{IfMatch: "stale-etag"})
	if err != ErrPreconditionFailed {
		t.Fatalf("conditional PUT against a stale ETag = %v, want ErrPreconditionFailed", err)
	}
	// The rejected write must not have landed.
	info, err := s.Head("b", "k", "")
	if err != nil {
		t.Fatal(err)
	}
	if info.Size != 2 { // "v2"
		t.Fatalf("Head after rejected conditional PUT: size %d, want the unchanged v2 size", info.Size)
	}
}

func TestConditionalPutIfNoneMatchStarOnlyCreates(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	_, err := s.PutWithOptions("b", "k", strings.NewReader("first"), PutOptions{IfNoneMatch: "*"})
	if err != nil {
		t.Fatalf("create-only PUT on an absent key should succeed, got %v", err)
	}
	_, err = s.PutWithOptions("b", "k", strings.NewReader("second"), PutOptions{IfNoneMatch: "*"})
	if err != ErrPreconditionFailed {
		t.Fatalf("create-only PUT on an existing key = %v, want ErrPreconditionFailed", err)
	}
}

func TestConditionalPutIfNoneMatchStarSucceedsAfterDelete(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	if _, err := s.Put("b", "k", strings.NewReader("v1")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Delete("b", "k"); err != nil {
		t.Fatal(err)
	}
	// The key is hidden behind a delete marker, so "only create" should
	// treat it as absent, same as real S3.
	if _, err := s.PutWithOptions("b", "k", strings.NewReader("v2"), PutOptions{IfNoneMatch: "*"}); err != nil {
		t.Fatalf("create-only PUT after a delete marker should succeed, got %v", err)
	}
}

func TestConditionalPutIsCompareAndSwapUnderConcurrency(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	put, err := s.Put("b", "k", strings.NewReader("v1"))
	if err != nil {
		t.Fatal(err)
	}

	const attempts = 20
	var wins int64
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.PutWithOptions("b", "k", strings.NewReader("v2"), PutOptions{IfMatch: put.ETag})
			if err == nil {
				atomic.AddInt64(&wins, 1)
			} else if err != ErrPreconditionFailed {
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if wins != 1 {
		t.Fatalf("exactly one conditional PUT against the same stale ETag should win, got %d", wins)
	}
}
