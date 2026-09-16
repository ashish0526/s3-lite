package s3lite

import (
	"encoding/xml"
	"net/http"
	"strconv"
	"strings"
)

type initiateMultipartUploadResult struct {
	XMLName  xml.Name `xml:"InitiateMultipartUploadResult"`
	Bucket   string   `xml:"Bucket"`
	Key      string   `xml:"Key"`
	UploadID string   `xml:"UploadId"`
}

type completeMultipartUploadRequest struct {
	XMLName xml.Name `xml:"CompleteMultipartUpload"`
	Parts   []struct {
		PartNumber int    `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
	} `xml:"Part"`
}

type completeMultipartUploadResult struct {
	XMLName xml.Name `xml:"CompleteMultipartUploadResult"`
	Bucket  string   `xml:"Bucket"`
	Key     string   `xml:"Key"`
	ETag    string   `xml:"ETag"`
}

// unquoteETag undoes quoteETag: a client echoes back the ETag exactly as it
// received it in a header (double-quoted), but the Store compares against
// the unquoted hex string it handed out.
func unquoteETag(s string) string { return strings.Trim(s, `"`) }

func (s *Server) createMultipartUpload(w http.ResponseWriter, bucket, key string) {
	uploadID, err := s.store.CreateMultipartUpload(bucket, key)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	xml.NewEncoder(w).Encode(initiateMultipartUploadResult{Bucket: bucket, Key: key, UploadID: uploadID})
}

func (s *Server) uploadPart(w http.ResponseWriter, r *http.Request, bucket, key, uploadID, partNumberStr string) {
	partNumber, err := strconv.Atoi(partNumberStr)
	if err != nil || partNumber < 1 {
		writeError(w, http.StatusBadRequest, "InvalidArgument", "partNumber must be a positive integer.")
		return
	}
	etag, err := s.store.UploadPart(bucket, key, uploadID, partNumber, r.Body)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("ETag", quoteETag(etag))
	w.WriteHeader(http.StatusOK)
}

func (s *Server) completeMultipartUpload(w http.ResponseWriter, r *http.Request, bucket, key, uploadID string) {
	var req completeMultipartUploadRequest
	if err := xml.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "MalformedXML", "Could not parse the complete-upload request body.")
		return
	}
	parts := make([]CompletedPart, len(req.Parts))
	for i, p := range req.Parts {
		parts[i] = CompletedPart{PartNumber: p.PartNumber, ETag: unquoteETag(p.ETag)}
	}
	result, err := s.store.CompleteMultipartUpload(bucket, key, uploadID, parts)
	if err != nil {
		writeStoreError(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/xml")
	xml.NewEncoder(w).Encode(completeMultipartUploadResult{Bucket: bucket, Key: key, ETag: quoteETag(result.ETag)})
}

func (s *Server) abortMultipartUpload(w http.ResponseWriter, bucket, key, uploadID string) {
	if err := s.store.AbortMultipartUpload(bucket, key, uploadID); err != nil {
		writeStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
