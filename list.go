package s3lite

import (
	"encoding/hex"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// decodeSegment reverses encodeSegment: a literal "." or ".." was escaped
// to "=2e"/"=2e2e" (hex of the ASCII bytes) before EncodeKey ever wrote a
// path, so a segment starting with "=" is hex-decoded instead of
// PathUnescaped.
func decodeSegment(seg string) (string, error) {
	if strings.HasPrefix(seg, "=") {
		raw, err := hex.DecodeString(seg[1:])
		if err != nil {
			return "", err
		}
		return string(raw), nil
	}
	return url.PathUnescape(seg)
}

// DecodeKey reverses EncodeKey, recovering the original key from the
// relative path a Store wrote it under. This only needs to work for paths
// this package itself produced (walking the objects directory for
// listing), so it does not need to handle arbitrary filesystem paths.
func DecodeKey(encoded string) (string, error) {
	parts := strings.Split(filepath.ToSlash(encoded), "/")
	decoded := make([]string, len(parts))
	for i, p := range parts {
		d, err := decodeSegment(p)
		if err != nil {
			return "", err
		}
		decoded[i] = d
	}
	return strings.Join(decoded, "/"), nil
}

// ObjectSummary is one entry in a listing.
type ObjectSummary struct {
	Key  string
	Size int64
	ETag string
}

// ListResult is one page of a bucket listing: real objects under Objects,
// and — when a delimiter groups keys sharing everything up to and
// including that delimiter — the group names under CommonPrefixes, e.g.
// listing with delimiter "/" turns "photos/2024/a.jpg" into the common
// prefix "photos/" instead of a full key, the same way S3 fakes a
// directory hierarchy over a flat keyspace.
type ListResult struct {
	Objects               []ObjectSummary
	CommonPrefixes        []string
	IsTruncated           bool
	NextContinuationToken string
}

// List enumerates a bucket's keys in sorted order, filtered by prefix,
// grouped by delimiter, and paginated: continuationToken (if non-empty) is
// the last key returned by a previous page, so this page starts strictly
// after it. maxKeys <= 0 defaults to 1000, S3's own default page size.
func (s *Store) List(bucket, prefix, delimiter, continuationToken string, maxKeys int) (ListResult, error) {
	ok, err := s.bucketExists(bucket)
	if err != nil {
		return ListResult{}, err
	}
	if !ok {
		return ListResult{}, ErrNoSuchBucket
	}
	if maxKeys <= 0 {
		maxKeys = 1000
	}

	bdir, err := s.bucketDir(bucket)
	if err != nil {
		return ListResult{}, err
	}
	objectsDir := filepath.Join(bdir, "objects")

	var all []ObjectSummary
	walkErr := filepath.WalkDir(objectsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".meta.json") {
			return nil
		}
		rel, err := filepath.Rel(objectsDir, path)
		if err != nil {
			return err
		}
		key, err := DecodeKey(strings.TrimSuffix(rel, ".meta.json"))
		if err != nil {
			return err
		}
		meta, err := readMeta(path)
		if err != nil {
			return err
		}
		v, ok := meta.latest()
		if !ok || v.Deleted {
			return nil // hidden behind a delete marker, or an empty history
		}
		all = append(all, ObjectSummary{Key: key, Size: v.Size, ETag: v.ETag})
		return nil
	})
	if walkErr != nil {
		return ListResult{}, walkErr
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Key < all[j].Key })

	type entry struct {
		key      string
		isPrefix bool
		obj      ObjectSummary
	}
	var entries []entry
	seenPrefix := map[string]bool{}
	for _, o := range all {
		if !strings.HasPrefix(o.Key, prefix) {
			continue
		}
		rest := o.Key[len(prefix):]
		if delimiter != "" {
			if idx := strings.Index(rest, delimiter); idx >= 0 {
				cp := prefix + rest[:idx+len(delimiter)]
				if seenPrefix[cp] {
					continue
				}
				seenPrefix[cp] = true
				entries = append(entries, entry{key: cp, isPrefix: true})
				continue
			}
		}
		entries = append(entries, entry{key: o.Key, obj: o})
	}

	start := 0
	if continuationToken != "" {
		for start < len(entries) && entries[start].key <= continuationToken {
			start++
		}
	}
	end := start
	for end < len(entries) && end-start < maxKeys {
		end++
	}

	var result ListResult
	for _, e := range entries[start:end] {
		if e.isPrefix {
			result.CommonPrefixes = append(result.CommonPrefixes, e.key)
		} else {
			result.Objects = append(result.Objects, e.obj)
		}
	}
	if end < len(entries) {
		result.IsTruncated = true
		result.NextContinuationToken = entries[end-1].key
	}
	return result, nil
}
