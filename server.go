package s3lite

import (
	"io"
	"net/http"
	"strconv"
	"strings"
)

// Server exposes a Store over an S3-shaped HTTP API: path-style routing
// (bucket is the first path segment, everything after it is the key),
// verbs mapped the way S3 maps them (PUT writes, GET/HEAD read, DELETE
// removes), and errors shaped like apiError instead of bare HTTP text.
type Server struct {
	store *Store
}

func NewServer(store *Store) *Server { return &Server{store: store} }

// parsePath splits a request path into (bucket, key). A path with no
// second segment (just "/bucket" or "/bucket/") names the bucket alone.
func parsePath(path string) (bucket, key string) {
	trimmed := strings.TrimPrefix(path, "/")
	i := strings.IndexByte(trimmed, '/')
	if i < 0 {
		return trimmed, ""
	}
	return trimmed[:i], trimmed[i+1:]
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bucket, key := parsePath(r.URL.Path)
	if bucket == "" {
		writeError(w, http.StatusBadRequest, "InvalidArgument", "A bucket name is required.")
		return
	}
	if key == "" {
		s.handleBucket(w, r, bucket)
		return
	}
	s.handleObject(w, r, bucket, key)
}

func (s *Server) handleBucket(w http.ResponseWriter, r *http.Request, bucket string) {
	switch r.Method {
	case http.MethodPut:
		if err := s.store.CreateBucket(bucket); err != nil {
			writeStoreError(w, err)
			return
		}
		w.WriteHeader(http.StatusOK)
	case http.MethodGet:
		if r.URL.Query().Has("list-type") {
			s.listObjects(w, r, bucket)
			return
		}
		writeError(w, http.StatusBadRequest, "InvalidArgument", "GET on a bucket requires list-type=2.")
	default:
		writeError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "Unsupported method for a bucket path.")
	}
}

func (s *Server) handleObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	q := r.URL.Query()
	switch {
	case r.Method == http.MethodPost && q.Has("uploads"):
		s.createMultipartUpload(w, bucket, key)
		return
	case r.Method == http.MethodPut && q.Has("uploadId") && q.Has("partNumber"):
		s.uploadPart(w, r, bucket, key, q.Get("uploadId"), q.Get("partNumber"))
		return
	case r.Method == http.MethodPost && q.Has("uploadId"):
		s.completeMultipartUpload(w, r, bucket, key, q.Get("uploadId"))
		return
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		s.abortMultipartUpload(w, bucket, key, q.Get("uploadId"))
		return
	}

	switch r.Method {
	case http.MethodPut:
		s.putObject(w, r, bucket, key)
	case http.MethodGet:
		s.getObject(w, r, bucket, key)
	case http.MethodHead:
		s.headObject(w, r, bucket, key)
	case http.MethodDelete:
		s.deleteObject(w, r, bucket, key)
	default:
		writeError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "Unsupported method for an object path.")
	}
}

func (s *Server) putObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	result, err := s.store.Put(bucket, key, r.Body)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("ETag", quoteETag(result.ETag))
	w.Header().Set("x-amz-version-id", result.VersionID)
	w.WriteHeader(http.StatusOK)
}

func (s *Server) getObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	rc, info, err := s.store.Get(bucket, key, r.URL.Query().Get("versionId"))
	if err != nil {
		if err == ErrIsDeleteMarker {
			w.Header().Set("x-amz-delete-marker", "true")
		}
		writeStoreError(w, err)
		return
	}
	defer rc.Close()
	w.Header().Set("ETag", quoteETag(info.ETag))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	w.Header().Set("x-amz-version-id", info.VersionID)
	w.WriteHeader(http.StatusOK)
	io.Copy(w, rc)
}

func (s *Server) headObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	info, err := s.store.Head(bucket, key, r.URL.Query().Get("versionId"))
	if err != nil {
		if err == ErrIsDeleteMarker {
			w.Header().Set("x-amz-delete-marker", "true")
		}
		writeStoreError(w, err)
		return
	}
	w.Header().Set("ETag", quoteETag(info.ETag))
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	w.Header().Set("x-amz-version-id", info.VersionID)
	w.WriteHeader(http.StatusOK)
}

// deleteObject mirrors real S3's two DELETE behaviors on a versioned key:
// no ?versionId adds a delete marker as the new latest version (the bytes
// stay, just hidden); an explicit ?versionId permanently erases that one
// version's record.
func (s *Server) deleteObject(w http.ResponseWriter, r *http.Request, bucket, key string) {
	if versionID := r.URL.Query().Get("versionId"); versionID != "" {
		if err := s.store.DeleteVersion(bucket, key, versionID); err != nil {
			writeStoreError(w, err)
			return
		}
		w.Header().Set("x-amz-version-id", versionID)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	markerID, err := s.store.Delete(bucket, key)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("x-amz-delete-marker", "true")
	w.Header().Set("x-amz-version-id", markerID)
	w.WriteHeader(http.StatusNoContent)
}

// quoteETag matches S3's own wire format: the ETag header value is always
// double-quoted.
func quoteETag(etag string) string { return `"` + etag + `"` }
