package s3lite

import "testing"

func TestEncodeKeyBasic(t *testing.T) {
	got, err := EncodeKey("photos/2024/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	want := "photos/2024/a.jpg"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEncodeKeyCollapsesRepeatedSlashes(t *testing.T) {
	a, err := EncodeKey("a//b")
	if err != nil {
		t.Fatal(err)
	}
	b, err := EncodeKey("a/b")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatalf("expected a//b and a/b to collapse to the same path, got %q vs %q", a, b)
	}
}

func TestEncodeKeyRejectsInvalid(t *testing.T) {
	for _, k := range []string{"", "a\x00b"} {
		if _, err := EncodeKey(k); err != ErrInvalidKey {
			t.Fatalf("EncodeKey(%q) = %v, want ErrInvalidKey", k, err)
		}
	}
}

func TestEncodeKeyNeutralizesTraversal(t *testing.T) {
	got, err := EncodeKey("../../etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	if got == "../../etc/passwd" || got[:2] == ".." {
		t.Fatalf("traversal segment leaked through unescaped: %q", got)
	}
}

func TestEncodeBucket(t *testing.T) {
	if _, err := EncodeBucket(""); err != ErrInvalidKey {
		t.Fatalf("expected ErrInvalidKey for empty bucket, got %v", err)
	}
	if _, err := EncodeBucket("has/slash"); err != ErrInvalidKey {
		t.Fatalf("expected ErrInvalidKey for bucket with slash, got %v", err)
	}
	got, err := EncodeBucket("my-bucket")
	if err != nil {
		t.Fatal(err)
	}
	if got != "my-bucket" {
		t.Fatalf("got %q", got)
	}
}
