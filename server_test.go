package s3lite

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	store, err := NewStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return httptest.NewServer(NewServer(store))
}

func doReq(t *testing.T, method, url, body string) *http.Response {
	t.Helper()
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, err := http.NewRequest(method, url, r)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestServerObjectRoundTrip(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	if resp := doReq(t, http.MethodPut, ts.URL+"/mybucket", ""); resp.StatusCode != 200 {
		t.Fatalf("create bucket: status %d", resp.StatusCode)
	}

	put := doReq(t, http.MethodPut, ts.URL+"/mybucket/dir/file.txt", "hello")
	if put.StatusCode != 200 {
		t.Fatalf("put: status %d", put.StatusCode)
	}
	etag := put.Header.Get("ETag")
	if etag == "" || etag[0] != '"' {
		t.Fatalf("ETag header = %q, want a quoted value", etag)
	}

	head := doReq(t, http.MethodHead, ts.URL+"/mybucket/dir/file.txt", "")
	if head.StatusCode != 200 || head.Header.Get("Content-Length") != "5" {
		t.Fatalf("head: status %d, content-length %q", head.StatusCode, head.Header.Get("Content-Length"))
	}

	get := doReq(t, http.MethodGet, ts.URL+"/mybucket/dir/file.txt", "")
	body, _ := io.ReadAll(get.Body)
	get.Body.Close()
	if get.StatusCode != 200 || string(body) != "hello" {
		t.Fatalf("get: status %d, body %q", get.StatusCode, body)
	}
	if get.Header.Get("ETag") != etag {
		t.Fatalf("get ETag %q != put ETag %q", get.Header.Get("ETag"), etag)
	}

	del := doReq(t, http.MethodDelete, ts.URL+"/mybucket/dir/file.txt", "")
	if del.StatusCode != 204 {
		t.Fatalf("delete: status %d", del.StatusCode)
	}

	get2 := doReq(t, http.MethodGet, ts.URL+"/mybucket/dir/file.txt", "")
	if get2.StatusCode != 404 {
		t.Fatalf("get after delete: status %d", get2.StatusCode)
	}
}

func TestServerErrorsAreS3Shaped(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doReq(t, http.MethodPut, ts.URL+"/nosuchbucket/key", "x")
	if resp.StatusCode != 404 {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if resp.Header.Get("Content-Type") != "application/xml" {
		t.Fatalf("Content-Type = %q, want application/xml", resp.Header.Get("Content-Type"))
	}
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<Code>NoSuchBucket</Code>") {
		t.Fatalf("body = %s, want a NoSuchBucket Code element", body)
	}
}

func TestServerMissingBucketNameIsBadRequest(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doReq(t, http.MethodGet, ts.URL+"/", "")
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestServerUnsupportedMethodIsMethodNotAllowed(t *testing.T) {
	ts := newTestServer(t)
	defer ts.Close()

	resp := doReq(t, http.MethodPatch, ts.URL+"/bucket/key", "")
	if resp.StatusCode != 405 {
		t.Fatalf("status = %d, want 405", resp.StatusCode)
	}
}
