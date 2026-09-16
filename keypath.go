package s3lite

import (
	"encoding/hex"
	"errors"
	"net/url"
	"path/filepath"
	"strings"
)

// ErrInvalidKey is returned when a bucket or object key cannot be mapped to
// a filesystem path at all (empty, or containing a NUL byte).
var ErrInvalidKey = errors.New("s3lite: invalid key")

// encodeSegment maps one path segment of a key to a filesystem-safe name.
//
// url.PathEscape leaves "." and ".." untouched (they are in the unreserved
// set), which would be catastrophic here: the encoded segment becomes a
// literal directory name, and a literal ".." component escapes the bucket's
// data directory. Those two segments get a distinct escape ("=2e", "=2e2e")
// before falling through to PathEscape for everything else.
func encodeSegment(seg string) string {
	if seg == "." || seg == ".." {
		return "=" + hex.EncodeToString([]byte(seg))
	}
	return url.PathEscape(seg)
}

// EncodeKey maps an S3-style object key (which may itself contain "/" to
// look path-like, e.g. "photos/2024/a.jpg") to a relative filesystem path.
// Repeated slashes collapse (S3's own key namespace is flat; the "/"
// separators are cosmetic, not a real directory hierarchy, so this build
// treats "a//b" and "a/b" as the same key on purpose).
//
// Two different keys can theoretically map to the same encoded path if one
// key's segment is literally the escape sequence this function produces for
// "." or ".." (e.g. a key segment spelled "=2e") — a documented, deliberate
// gap for a "-lite" build; see DESIGN.md.
func EncodeKey(key string) (string, error) {
	if key == "" || strings.ContainsRune(key, 0) {
		return "", ErrInvalidKey
	}
	parts := strings.Split(key, "/")
	encoded := make([]string, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		encoded = append(encoded, encodeSegment(p))
	}
	if len(encoded) == 0 {
		return "", ErrInvalidKey
	}
	return filepath.Join(encoded...), nil
}

// EncodeBucket maps a bucket name to a single filesystem-safe path segment.
func EncodeBucket(bucket string) (string, error) {
	if bucket == "" || strings.ContainsRune(bucket, 0) || strings.Contains(bucket, "/") {
		return "", ErrInvalidKey
	}
	return encodeSegment(bucket), nil
}
