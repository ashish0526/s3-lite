package s3lite

import (
	"net/http"
	"strings"
)

const metaHeaderPrefix = "X-Amz-Meta-"

// extractMetadata pulls every X-Amz-Meta-* request header into the map
// PutWithOptions stores as a version's user metadata — the header name
// after the prefix, lowercased, is the metadata key.
func extractMetadata(h http.Header) map[string]string {
	var meta map[string]string
	for k, v := range h {
		if !strings.HasPrefix(k, metaHeaderPrefix) || len(v) == 0 {
			continue
		}
		if meta == nil {
			meta = make(map[string]string)
		}
		meta[strings.ToLower(strings.TrimPrefix(k, metaHeaderPrefix))] = v[0]
	}
	return meta
}

func writeMetadataHeaders(w http.ResponseWriter, meta map[string]string) {
	for k, v := range meta {
		w.Header().Set(metaHeaderPrefix+k, v)
	}
}

// checkReadConditions evaluates S3's read-side conditional headers against
// a resolved version and reports the status a match/mismatch should
// produce, or handled=false to proceed normally. Real S3 allows a
// comma-separated list of ETags in If-Match/If-None-Match; this build only
// compares a single value, a documented "-lite" cut (DESIGN.md).
func checkReadConditions(r *http.Request, info ObjectInfo) (status int, handled bool) {
	if inm := r.Header.Get("If-None-Match"); inm != "" {
		if inm == "*" || unquoteETag(inm) == info.ETag {
			return http.StatusNotModified, true
		}
	}
	if im := r.Header.Get("If-Match"); im != "" && im != "*" {
		if unquoteETag(im) != info.ETag {
			return http.StatusPreconditionFailed, true
		}
	}
	if ius := r.Header.Get("If-Unmodified-Since"); ius != "" {
		if t, err := http.ParseTime(ius); err == nil && info.ModTime.After(t) {
			return http.StatusPreconditionFailed, true
		}
	}
	return 0, false
}
