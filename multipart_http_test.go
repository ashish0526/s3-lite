package s3lite

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestServerMultipartRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	if resp := doReq(t, http.MethodPut, ts.URL+"/b", ""); resp.StatusCode != 200 {
		t.Fatalf("create bucket: status %d", resp.StatusCode)
	}

	initResp := doReq(t, http.MethodPost, ts.URL+"/b/big.bin?uploads", "")
	if initResp.StatusCode != 200 {
		t.Fatalf("initiate: status %d", initResp.StatusCode)
	}
	var initiated initiateMultipartUploadResult
	if err := xml.NewDecoder(initResp.Body).Decode(&initiated); err != nil {
		t.Fatal(err)
	}
	if initiated.UploadID == "" {
		t.Fatal("empty upload id")
	}

	part1 := doReq(t, http.MethodPut, fmt.Sprintf("%s/b/big.bin?partNumber=1&uploadId=%s", ts.URL, initiated.UploadID), "hello ")
	if part1.StatusCode != 200 {
		t.Fatalf("upload part 1: status %d", part1.StatusCode)
	}
	etag1 := part1.Header.Get("ETag")

	part2 := doReq(t, http.MethodPut, fmt.Sprintf("%s/b/big.bin?partNumber=2&uploadId=%s", ts.URL, initiated.UploadID), "world")
	if part2.StatusCode != 200 {
		t.Fatalf("upload part 2: status %d", part2.StatusCode)
	}
	etag2 := part2.Header.Get("ETag")

	completeBody := fmt.Sprintf(`<CompleteMultipartUpload><Part><PartNumber>1</PartNumber><ETag>%s</ETag></Part><Part><PartNumber>2</PartNumber><ETag>%s</ETag></Part></CompleteMultipartUpload>`, etag1, etag2)
	completeResp := doReq(t, http.MethodPost, fmt.Sprintf("%s/b/big.bin?uploadId=%s", ts.URL, initiated.UploadID), completeBody)
	if completeResp.StatusCode != 200 {
		body, _ := io.ReadAll(completeResp.Body)
		t.Fatalf("complete: status %d, body %s", completeResp.StatusCode, body)
	}
	var completed completeMultipartUploadResult
	if err := xml.NewDecoder(completeResp.Body).Decode(&completed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(completed.ETag, "-2") {
		t.Fatalf("composite ETag %q missing part-count suffix", completed.ETag)
	}

	get := doReq(t, http.MethodGet, ts.URL+"/b/big.bin", "")
	body, _ := io.ReadAll(get.Body)
	if string(body) != "hello world" {
		t.Fatalf("got %q", body)
	}
}

func TestServerMultipartAbort(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doReq(t, http.MethodPut, ts.URL+"/b", "")
	initResp := doReq(t, http.MethodPost, ts.URL+"/b/k?uploads", "")
	var initiated initiateMultipartUploadResult
	xml.NewDecoder(initResp.Body).Decode(&initiated)

	doReq(t, http.MethodPut, fmt.Sprintf("%s/b/k?partNumber=1&uploadId=%s", ts.URL, initiated.UploadID), "x")

	abortResp := doReq(t, http.MethodDelete, fmt.Sprintf("%s/b/k?uploadId=%s", ts.URL, initiated.UploadID), "")
	if abortResp.StatusCode != 204 {
		t.Fatalf("abort: status %d", abortResp.StatusCode)
	}

	uploadAfterAbort := doReq(t, http.MethodPut, fmt.Sprintf("%s/b/k?partNumber=2&uploadId=%s", ts.URL, initiated.UploadID), "y")
	if uploadAfterAbort.StatusCode != 404 {
		t.Fatalf("upload part after abort: status %d, want 404", uploadAfterAbort.StatusCode)
	}
}
