package s3lite

import (
	"encoding/xml"
	"net/http"
)

// apiError is the body S3 itself returns on failure: a small XML document
// naming a stable error Code (what SDKs branch on) alongside a
// human-readable Message. Shaping errors this way, from the first HTTP
// step, is what makes this API "S3-shaped" rather than just an HTTP
// wrapper around the Store with ad-hoc error text.
type apiError struct {
	XMLName xml.Name `xml:"Error"`
	Code    string   `xml:"Code"`
	Message string   `xml:"Message"`
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/xml")
	w.WriteHeader(status)
	xml.NewEncoder(w).Encode(apiError{Code: code, Message: message})
}

// writeStoreError maps a Store-layer error onto the S3 error code and HTTP
// status a real client expects. Anything unrecognized is a 500 — a bug in
// this server, not a client mistake.
func writeStoreError(w http.ResponseWriter, err error) {
	switch err {
	case ErrNoSuchBucket:
		writeError(w, http.StatusNotFound, "NoSuchBucket", "The specified bucket does not exist.")
	case ErrNoSuchKey:
		writeError(w, http.StatusNotFound, "NoSuchKey", "The specified key does not exist.")
	case ErrInvalidKey:
		writeError(w, http.StatusBadRequest, "InvalidArgument", "The specified key is not valid.")
	case ErrIsDeleteMarker:
		writeError(w, http.StatusMethodNotAllowed, "MethodNotAllowed", "The specified method is not allowed against a delete marker.")
	case ErrNoSuchUpload:
		writeError(w, http.StatusNotFound, "NoSuchUpload", "The specified upload does not exist.")
	case ErrPartMismatch, ErrEmptyPartList:
		writeError(w, http.StatusBadRequest, "InvalidPart", "The part list does not match the upload.")
	default:
		writeError(w, http.StatusInternalServerError, "InternalError", err.Error())
	}
}
