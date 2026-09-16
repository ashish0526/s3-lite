package s3lite

import (
	"strings"
	"testing"
)

func putAll(t *testing.T, s *Store, bucket string, keys []string) {
	t.Helper()
	for _, k := range keys {
		if _, err := s.Put(bucket, k, strings.NewReader(k)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestListPrefixOnly(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	putAll(t, s, "b", []string{"a", "b", "photos/1.jpg", "photos/2.jpg", "videos/1.mp4"})

	res, err := s.List("b", "photos/", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Objects) != 2 || res.Objects[0].Key != "photos/1.jpg" || res.Objects[1].Key != "photos/2.jpg" {
		t.Fatalf("got %+v", res.Objects)
	}
	if len(res.CommonPrefixes) != 0 {
		t.Fatalf("expected no common prefixes without a delimiter, got %v", res.CommonPrefixes)
	}
}

func TestListDelimiterGroupsCommonPrefixes(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	putAll(t, s, "b", []string{"a.txt", "photos/2024/a.jpg", "photos/2024/b.jpg", "photos/2025/c.jpg", "videos/a.mp4"})

	res, err := s.List("b", "", "/", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Objects) != 1 || res.Objects[0].Key != "a.txt" {
		t.Fatalf("expected only the root-level key as an object, got %+v", res.Objects)
	}
	want := map[string]bool{"photos/": true, "videos/": true}
	if len(res.CommonPrefixes) != 2 {
		t.Fatalf("got common prefixes %v", res.CommonPrefixes)
	}
	for _, p := range res.CommonPrefixes {
		if !want[p] {
			t.Fatalf("unexpected common prefix %q", p)
		}
	}
}

func TestListPagination(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	putAll(t, s, "b", []string{"k1", "k2", "k3", "k4", "k5"})

	page1, err := s.List("b", "", "", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1.Objects) != 2 || !page1.IsTruncated {
		t.Fatalf("page1 = %+v", page1)
	}
	if page1.Objects[0].Key != "k1" || page1.Objects[1].Key != "k2" {
		t.Fatalf("page1 keys = %+v", page1.Objects)
	}

	page2, err := s.List("b", "", "", page1.NextContinuationToken, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Objects) != 2 || page2.Objects[0].Key != "k3" || page2.Objects[1].Key != "k4" {
		t.Fatalf("page2 = %+v", page2)
	}
	if !page2.IsTruncated {
		t.Fatal("expected page2 to still be truncated")
	}

	page3, err := s.List("b", "", "", page2.NextContinuationToken, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page3.Objects) != 1 || page3.Objects[0].Key != "k5" || page3.IsTruncated {
		t.Fatalf("page3 = %+v", page3)
	}
}

func TestListReportsSizeAndETag(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	put, err := s.Put("b", "k", strings.NewReader("payload"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.List("b", "", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Objects) != 1 || res.Objects[0].Size != 7 || res.Objects[0].ETag != put.ETag {
		t.Fatalf("got %+v", res.Objects)
	}
}

func TestListMissingBucket(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.List("nope", "", "", "", 0); err != ErrNoSuchBucket {
		t.Fatalf("List on missing bucket = %v, want ErrNoSuchBucket", err)
	}
}

func TestListEmptyBucket(t *testing.T) {
	s := newTestStore(t)
	s.CreateBucket("b")
	res, err := s.List("b", "", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Objects) != 0 || res.IsTruncated {
		t.Fatalf("got %+v", res)
	}
}
