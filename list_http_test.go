package s3lite

import (
	"encoding/xml"
	"net/http"
	"testing"
)

func TestServerListObjects(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doReq(t, http.MethodPut, ts.URL+"/b", "")
	doReq(t, http.MethodPut, ts.URL+"/b/photos/a.jpg", "x")
	doReq(t, http.MethodPut, ts.URL+"/b/photos/b.jpg", "y")
	doReq(t, http.MethodPut, ts.URL+"/b/root.txt", "z")

	resp := doReq(t, http.MethodGet, ts.URL+"/b?list-type=2&delimiter=/", "")
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	var result listBucketResult
	if err := xml.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Contents) != 1 || result.Contents[0].Key != "root.txt" {
		t.Fatalf("Contents = %+v", result.Contents)
	}
	if len(result.CommonPrefixes) != 1 || result.CommonPrefixes[0].Prefix != "photos/" {
		t.Fatalf("CommonPrefixes = %+v", result.CommonPrefixes)
	}
}

func TestServerListObjectsRequiresListType(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()
	doReq(t, http.MethodPut, ts.URL+"/b", "")
	resp := doReq(t, http.MethodGet, ts.URL+"/b", "")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
