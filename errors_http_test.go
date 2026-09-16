package s3lite

import (
	"encoding/xml"
	"net/http/httptest"
	"testing"
)

func TestWriteStoreErrorMapsNoSuchKey(t *testing.T) {
	rec := httptest.NewRecorder()
	writeStoreError(rec, ErrNoSuchKey)
	if rec.Code != 404 {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
	var got apiError
	if err := xml.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Code != "NoSuchKey" {
		t.Fatalf("Code = %q, want NoSuchKey", got.Code)
	}
}

func TestWriteStoreErrorUnknownIsInternalError(t *testing.T) {
	rec := httptest.NewRecorder()
	writeStoreError(rec, errUnmapped)
	if rec.Code != 500 {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

var errUnmapped = &customErr{"boom"}

type customErr struct{ s string }

func (e *customErr) Error() string { return e.s }
