package s3lite

import (
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestServerVersionedGetAndDelete(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doReq(t, http.MethodPut, ts.URL+"/b", "")
	put1 := doReq(t, http.MethodPut, ts.URL+"/b/k", "v1")
	v1 := put1.Header.Get("x-amz-version-id")
	if v1 == "" {
		t.Fatal("expected x-amz-version-id on PUT")
	}
	doReq(t, http.MethodPut, ts.URL+"/b/k", "v2")

	// Latest is v2.
	latest := doReq(t, http.MethodGet, ts.URL+"/b/k", "")
	body, _ := io.ReadAll(latest.Body)
	if string(body) != "v2" {
		t.Fatalf("latest body = %q, want v2", body)
	}

	// v1 is still reachable by version.
	old := doReq(t, http.MethodGet, fmt.Sprintf("%s/b/k?versionId=%s", ts.URL, v1), "")
	oldBody, _ := io.ReadAll(old.Body)
	if string(oldBody) != "v1" {
		t.Fatalf("versioned GET body = %q, want v1", oldBody)
	}

	// A plain DELETE hides the key behind a marker, not erasing history.
	del := doReq(t, http.MethodDelete, ts.URL+"/b/k", "")
	if del.StatusCode != 204 || del.Header.Get("x-amz-delete-marker") != "true" {
		t.Fatalf("delete: status %d, delete-marker header %q", del.StatusCode, del.Header.Get("x-amz-delete-marker"))
	}
	afterDelete := doReq(t, http.MethodGet, ts.URL+"/b/k", "")
	if afterDelete.StatusCode != 404 {
		t.Fatalf("get after delete: status %d, want 404", afterDelete.StatusCode)
	}

	// v1's bytes are still there under its version ID even after the
	// marker is the latest version.
	stillThere := doReq(t, http.MethodGet, fmt.Sprintf("%s/b/k?versionId=%s", ts.URL, v1), "")
	if stillThere.StatusCode != 200 {
		t.Fatalf("versioned GET after delete: status %d, want 200", stillThere.StatusCode)
	}
}

func TestServerDeleteWithVersionIDIsPermanent(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	doReq(t, http.MethodPut, ts.URL+"/b", "")
	put := doReq(t, http.MethodPut, ts.URL+"/b/k", "only version")
	v := put.Header.Get("x-amz-version-id")

	del := doReq(t, http.MethodDelete, fmt.Sprintf("%s/b/k?versionId=%s", ts.URL, v), "")
	if del.StatusCode != 204 {
		t.Fatalf("delete by version: status %d", del.StatusCode)
	}

	resp := doReq(t, http.MethodGet, fmt.Sprintf("%s/b/k?versionId=%s", ts.URL, v), "")
	if resp.StatusCode != 404 {
		t.Fatalf("get after permanent delete: status %d, want 404", resp.StatusCode)
	}
}
